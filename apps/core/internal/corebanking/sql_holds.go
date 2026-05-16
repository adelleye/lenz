package corebanking

import (
	"context"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

func createHoldTx(ctx context.Context, tx *sqlx.Tx, transfer Transfer, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO account_holds (id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'active', 'pending_outbound_transfer', $7, $7)`,
		newID(), transfer.InstitutionID, transfer.AccountID, transfer.ID, transfer.AmountMinor, transfer.CurrencyID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE account_balances
SET available_minor = available_minor - $1,
    updated_at = $2
WHERE institution_id = $3 AND account_id = $4`, transfer.AmountMinor, now, transfer.InstitutionID, transfer.AccountID)
	return err
}

func releaseHoldTx(ctx context.Context, tx *sqlx.Tx, institutionID, transferID string, now time.Time) error {
	hold, err := getActiveHoldForTransferTx(ctx, tx, institutionID, transferID)
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

func consumeHoldTx(ctx context.Context, tx *sqlx.Tx, institutionID, transferID string, now time.Time) error {
	hold, err := getActiveHoldForTransferTx(ctx, tx, institutionID, transferID)
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

func getActiveHoldForTransferTx(ctx context.Context, tx *sqlx.Tx, institutionID, transferID string) (*AccountHold, error) {
	var hold AccountHold
	err := tx.GetContext(ctx, &hold, `
SELECT id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, created_at, updated_at, released_at
FROM account_holds
WHERE institution_id = $1 AND transfer_id = $2 AND status = 'active'
FOR UPDATE`, institutionID, transferID)
	return &hold, normalizeSQLError(err)
}
