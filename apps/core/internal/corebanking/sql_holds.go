package corebanking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type sqlHoldRepository struct{}

func (r *sqlHoldRepository) create(ctx context.Context, tx TxRunner, transfer Transfer, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO account_holds (id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'active', 'pending_outbound_transfer', $7, $7)`,
		uuid.Must(uuid.NewRandom()).String(), transfer.InstitutionID, transfer.AccountID, transfer.ID, transfer.AmountMinor, transfer.CurrencyID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE account_balances
SET available_minor = available_minor - $1,
    updated_at = $2
WHERE institution_id = $3 AND account_id = $4`, transfer.AmountMinor, now, transfer.InstitutionID, transfer.AccountID)
	return err
}

func (r *sqlHoldRepository) release(ctx context.Context, tx TxRunner, institutionID, transferID string, now time.Time) error {
	hold, err := r.getActiveForTransfer(ctx, tx, institutionID, transferID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
UPDATE account_holds
SET status = 'released',
    updated_at = $1,
    released_at = $1
WHERE institution_id = $2 AND id = $3`, now, institutionID, hold.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE account_balances
SET available_minor = available_minor + $1,
    updated_at = $2
WHERE institution_id = $3 AND account_id = $4`, hold.AmountMinor, now, institutionID, hold.AccountID)
	return err
}

func (r *sqlHoldRepository) consume(ctx context.Context, tx TxRunner, institutionID, transferID string, now time.Time) error {
	hold, err := r.getActiveForTransfer(ctx, tx, institutionID, transferID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE account_holds
SET status = 'consumed',
    updated_at = $1,
    released_at = $1
WHERE institution_id = $2 AND id = $3`, now, institutionID, hold.ID)
	return err
}

func (r *sqlHoldRepository) getActiveForTransfer(ctx context.Context, tx TxRunner, institutionID, transferID string) (*AccountHold, error) {
	var hold AccountHold
	err := tx.GetContext(ctx, &hold, `
SELECT id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, created_at, updated_at, released_at
FROM account_holds
WHERE institution_id = $1 AND transfer_id = $2 AND status = 'active'
FOR UPDATE`, institutionID, transferID)
	return &hold, normalizeSQLError(err)
}
