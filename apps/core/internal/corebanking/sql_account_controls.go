package corebanking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (r *sqlAccountRepository) SetAccountStatus(ctx context.Context, input AccountControlInput, status string, allowedCurrentStatuses ...string) (*Account, error) {
	var account Account
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		institutionID, accountID := input.InstitutionID, input.AccountID
		if err := tx.GetContext(ctx, &account, accountSelectSQL+`
WHERE institution_id = $1 AND id = $2 FOR UPDATE`, institutionID, accountID); err != nil {
			return normalizeSQLError(err)
		}
		if !allowedAccountStatusTransition(account.Status, allowedCurrentStatuses) {
			return ErrInvalidRequest
		}
		if account.Status == status {
			return nil
		}
		oldStatus := account.Status
		now := time.Now().UTC()
		if err := normalizeSQLError(tx.GetContext(ctx, &account, `
UPDATE accounts
SET status = $1,
    updated_at = $2
WHERE institution_id = $3 AND id = $4
RETURNING id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at`,
			status, now, institutionID, accountID)); err != nil {
			return err
		}
		_, err := insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: input.InstitutionID,
			Action:        accountStatusAuditAction(oldStatus, status),
			EntityType:    "account",
			EntityID:      account.ID,
			CustomerID:    optionalAuditValue(account.CustomerID),
			AccountID:     account.ID,
			Reference:     input.Reference,
			OldStatus:     oldStatus,
			NewStatus:     status,
			Metadata: map[string]string{
				"reason": input.Reason,
			},
			CreatedAt: now,
		})
		return err
	})
	return &account, normalizeSQLError(err)
}

func (r *sqlAccountRepository) PlaceAccountLien(ctx context.Context, input AccountLienInput) (*AccountHold, error) {
	var hold *AccountHold
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		account, err := lockAccountBalance(ctx, tx, input.InstitutionID, input.AccountID)
		if err != nil {
			return err
		}
		if account.CurrencyID != input.CurrencyID || account.Kind != AccountKindCustomer || account.Status == AccountStatusClosed {
			return ErrInvalidRequest
		}
		existing, err := getActiveLienByReference(ctx, tx, input.InstitutionID, input.AccountID, input.Reference)
		if err == nil {
			hold = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if account.Balance.AvailableMinor < input.AmountMinor {
			return ErrInsufficient
		}
		now := time.Now().UTC()
		hold = &AccountHold{
			ID:            uuid.Must(uuid.NewRandom()).String(),
			InstitutionID: input.InstitutionID,
			AccountID:     input.AccountID,
			AmountMinor:   input.AmountMinor,
			CurrencyID:    input.CurrencyID,
			Status:        HoldStatusActive,
			Reason:        input.Reason,
			Reference:     input.Reference,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if _, err = tx.NamedExecContext(ctx, `
INSERT INTO account_holds (id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, reference, created_at, updated_at, released_at)
VALUES (:id, :institution_id, :account_id, :transfer_id, :amount_minor, :currency_id, :status, :reason, :reference, :created_at, :updated_at, :released_at)`, hold); err != nil {
			return normalizeSQLError(err)
		}
		if err = execOneRow(ctx, tx, "update account balance for lien place", `
UPDATE account_balances
SET available_minor = available_minor - $1,
    updated_at = $2
WHERE institution_id = $3 AND account_id = $4`, input.AmountMinor, now, input.InstitutionID, input.AccountID); err != nil {
			return err
		}
		_, err = insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: input.InstitutionID,
			Action:        AuditActionLienPlaced,
			EntityType:    "account_hold",
			EntityID:      hold.ID,
			AccountID:     hold.AccountID,
			Reference:     input.Reference,
			NewStatus:     HoldStatusActive,
			Metadata: map[string]string{
				"amount_minor": formatAuditInt(input.AmountMinor),
				"currency_id":  input.CurrencyID,
				"reason":       input.Reason,
			},
			CreatedAt: now,
		})
		return err
	})
	return hold, err
}

func (r *sqlAccountRepository) ReleaseAccountLien(ctx context.Context, input ReleaseLienInput) (*AccountHold, error) {
	var hold *AccountHold
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		current, err := getLienForUpdate(ctx, tx, input.InstitutionID, input.AccountID, input.LienID)
		if err != nil {
			return err
		}
		if current.Status != HoldStatusActive {
			hold = current
			return nil
		}
		now := time.Now().UTC()
		if err = execOneRow(ctx, tx, "release account lien", `
UPDATE account_holds
SET status = 'released',
    updated_at = $1,
    released_at = $1
WHERE institution_id = $2 AND account_id = $3 AND id = $4`, now, input.InstitutionID, input.AccountID, input.LienID); err != nil {
			return err
		}
		if err = execOneRow(ctx, tx, "update account balance for lien release", `
UPDATE account_balances
SET available_minor = available_minor + $1,
    updated_at = $2
WHERE institution_id = $3 AND account_id = $4`, current.AmountMinor, now, input.InstitutionID, input.AccountID); err != nil {
			return err
		}
		if hold, err = getLienForUpdate(ctx, tx, input.InstitutionID, input.AccountID, input.LienID); err != nil {
			return err
		}
		_, err = insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: input.InstitutionID,
			Action:        AuditActionLienReleased,
			EntityType:    "account_hold",
			EntityID:      hold.ID,
			AccountID:     hold.AccountID,
			Reference:     input.Reference,
			OldStatus:     HoldStatusActive,
			NewStatus:     HoldStatusReleased,
			Metadata: map[string]string{
				"amount_minor": formatAuditInt(hold.AmountMinor),
				"currency_id":  hold.CurrencyID,
				"reason":       input.Reason,
			},
			CreatedAt: now,
		})
		return err
	})
	return hold, err
}

func accountStatusAuditAction(oldStatus, status string) string {
	switch status {
	case AccountStatusFrozen:
		return AuditActionAccountFrozen
	case AccountStatusPostNoDebit:
		return AuditActionPNDActivated
	default:
		if oldStatus == AccountStatusPostNoDebit {
			return AuditActionPNDDeactivated
		}
		return AuditActionAccountUnfrozen
	}
}

func getActiveLienByReference(ctx context.Context, tx TxRunner, institutionID, accountID, reference string) (*AccountHold, error) {
	var hold AccountHold
	err := tx.GetContext(ctx, &hold, `
SELECT id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, reference, created_at, updated_at, released_at
FROM account_holds
WHERE institution_id = $1
  AND account_id = $2
  AND reference = $3
  AND transfer_id IS NULL
  AND status = 'active'
FOR UPDATE`, institutionID, accountID, reference)
	return &hold, normalizeSQLError(err)
}

func getLienForUpdate(ctx context.Context, tx TxRunner, institutionID, accountID, lienID string) (*AccountHold, error) {
	var hold AccountHold
	err := tx.GetContext(ctx, &hold, `
SELECT id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, reference, created_at, updated_at, released_at
FROM account_holds
WHERE institution_id = $1
  AND account_id = $2
  AND id = $3
  AND transfer_id IS NULL
FOR UPDATE`, institutionID, accountID, lienID)
	return &hold, normalizeSQLError(err)
}
