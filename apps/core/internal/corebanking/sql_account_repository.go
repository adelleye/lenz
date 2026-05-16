package corebanking

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
)

type sqlAccountRepository struct {
	db *sqlx.DB
}

const accountSelectSQL = `SELECT id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at FROM accounts`

func (r *sqlAccountRepository) ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	var accounts []Account
	err := r.db.SelectContext(ctx, &accounts, accountSelectSQL+` WHERE institution_id = $1 AND customer_id = $2 ORDER BY created_at`, institutionID, customerID)
	return accounts, err
}

func (r *sqlAccountRepository) GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error) {
	var account Account
	err := r.db.GetContext(ctx, &account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, institutionID, accountID)
	return &account, normalizeSQLError(err)
}

func (r *sqlAccountRepository) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	var balance AccountBalance
	err := r.db.GetContext(ctx, &balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2`, institutionID, accountID)
	return &balance, normalizeSQLError(err)
}

func (r *sqlAccountRepository) ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	options = normalizeListTransactionsOptions(options)
	var txns []Transaction
	var beforeCreatedAt *time.Time
	if options.BeforeCreatedAt != nil && !options.BeforeCreatedAt.IsZero() {
		beforeCreatedAt = options.BeforeCreatedAt
	}
	err := r.db.SelectContext(ctx, &txns, `
SELECT
	t.id::text || ':' || COALESCE(p.id::text, 'pending') AS id,
	t.id AS transfer_id,
	t.journal_entry_id,
	t.account_id,
	t.direction,
	t.status,
	t.amount_minor,
	CASE
		WHEN t.status != 'succeeded' THEN 0
		WHEN a.normal_balance = 'credit' AND p.direction = 'credit' THEN p.amount_minor
		WHEN a.normal_balance = 'debit' AND p.direction = 'debit' THEN p.amount_minor
		ELSE -p.amount_minor
	END AS signed_minor,
	t.currency_id,
	t.narration,
	t.created_at
FROM transfers t
JOIN accounts a ON a.institution_id = t.institution_id AND a.id = t.account_id
LEFT JOIN postings p ON p.institution_id = t.institution_id AND p.journal_entry_id = t.journal_entry_id AND p.account_id = t.account_id
WHERE t.institution_id = $1 AND t.account_id = $2
  AND ($3::timestamptz IS NULL OR t.created_at < $3)
ORDER BY t.created_at DESC, t.id DESC
LIMIT $4`, institutionID, accountID, beforeCreatedAt, options.Limit)
	return txns, err
}

type lockedAccountBalance struct {
	Account
	Balance AccountBalance
}

func lockAccountBalance(ctx context.Context, tx TxRunner, institutionID, accountID string) (*lockedAccountBalance, error) {
	var out lockedAccountBalance
	if err := tx.GetContext(ctx, &out.Account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2 FOR UPDATE`, institutionID, accountID); err != nil {
		return nil, normalizeSQLError(err)
	}
	if err := tx.GetContext(ctx, &out.Balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2 FOR UPDATE`, institutionID, accountID); err != nil {
		return nil, normalizeSQLError(err)
	}
	return &out, nil
}
