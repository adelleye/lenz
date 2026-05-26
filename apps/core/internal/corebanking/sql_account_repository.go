package corebanking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type sqlAccountRepository struct {
	db *sqlx.DB
}

const accountSelectSQL = `SELECT id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at FROM accounts`

func (r *sqlAccountRepository) CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error) {
	var account Account
	err := WithTx(ctx, r.db, func(tx TxRunner) error {
		var customerExists bool
		if err := tx.GetContext(ctx, &customerExists, `SELECT EXISTS (SELECT 1 FROM customers WHERE institution_id = $1 AND id = $2)`, input.InstitutionID, input.CustomerID); err != nil {
			return normalizeAccountSQLError(err)
		}
		if !customerExists {
			return ErrNotFound
		}

		now := time.Now().UTC()
		err := tx.GetContext(ctx, &account, `
INSERT INTO accounts (
	id,
	institution_id,
	customer_id,
	account_number,
	name,
	kind,
	product_type,
	allow_negative_balance,
	currency_id,
	normal_balance,
	status,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, 'customer', $6, false, $7, 'credit', 'active', $8, $8)
RETURNING id, institution_id, customer_id, account_number, name, kind, product_type, allow_negative_balance, currency_id, normal_balance, status, created_at, updated_at`,
			uuid.Must(uuid.NewRandom()).String(),
			input.InstitutionID,
			input.CustomerID,
			input.AccountNumber,
			input.Name,
			input.ProductType,
			input.CurrencyID,
			now,
		)
		if err != nil {
			return normalizeAccountSQLError(err)
		}

		if _, err = tx.ExecContext(ctx, `
INSERT INTO account_balances (account_id, institution_id, available_minor, ledger_minor, currency_id, updated_at)
VALUES ($1, $2, 0, 0, $3, $4)`,
			account.ID,
			account.InstitutionID,
			account.CurrencyID,
			now,
		); err != nil {
			return normalizeAccountSQLError(err)
		}
		_, err = insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: account.InstitutionID,
			Action:        AuditActionAccountCreated,
			EntityType:    "account",
			EntityID:      account.ID,
			CustomerID:    optionalAuditValue(account.CustomerID),
			AccountID:     account.ID,
			NewStatus:     account.Status,
			Metadata: map[string]string{
				"product_type": account.ProductType,
				"currency_id":  account.CurrencyID,
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &account, nil
}

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

func (r *sqlAccountRepository) GetAccountByNumber(ctx context.Context, institutionID, accountNumber string) (*Account, error) {
	var account Account
	err := r.db.GetContext(ctx, &account, accountSelectSQL+` WHERE institution_id = $1 AND account_number = $2`, institutionID, accountNumber)
	return &account, normalizeSQLError(err)
}

func (r *sqlAccountRepository) GetDefaultInternalSettlementAccount(ctx context.Context, institutionID, currencyID string) (*Account, error) {
	var accounts []Account
	err := r.db.SelectContext(ctx, &accounts, accountSelectSQL+`
WHERE institution_id = $1
  AND currency_id = $2
  AND kind = 'internal'
  AND product_type = 'internal'
  AND allow_negative_balance = true
  AND normal_balance = 'debit'
  AND status = 'active'
ORDER BY created_at, id
LIMIT 2`, institutionID, currencyID)
	if err != nil {
		return nil, err
	}
	switch len(accounts) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return &accounts[0], nil
	default:
		return nil, ErrInvalidRequest
	}
}

func (r *sqlAccountRepository) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	var accountExists bool
	if err := r.db.GetContext(ctx, &accountExists, `SELECT EXISTS (SELECT 1 FROM accounts WHERE institution_id = $1 AND id = $2)`, institutionID, accountID); err != nil {
		return nil, normalizeSQLError(err)
	}
	if !accountExists {
		return nil, ErrNotFound
	}

	var balance AccountBalance
	err := r.db.GetContext(ctx, &balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2`, institutionID, accountID)
	err = normalizeSQLError(err)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrDataIntegrity
	}
	if err != nil {
		return nil, err
	}
	return &balance, nil
}

func (r *sqlAccountRepository) ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	options = normalizeListTransactionsOptions(options)
	txns := []Transaction{}
	var beforeCreatedAt *time.Time
	if options.BeforeCreatedAt != nil && !options.BeforeCreatedAt.IsZero() {
		beforeCreatedAt = options.BeforeCreatedAt
	}
	var beforeTransferID *string
	if options.BeforeTransferID != "" {
		beforeTransferID = &options.BeforeTransferID
	}
	err := r.db.SelectContext(ctx, &txns, `
SELECT
	t.id::text || ':' || COALESCE(p.id::text, 'pending') AS id,
	t.id AS transfer_id,
	t.journal_entry_id,
	$2 AS account_id,
	t.institution_id,
	CASE
		WHEN t.status = 'succeeded' AND p.id IS NOT NULL AND (
			(a.normal_balance = 'credit' AND p.direction = 'credit') OR
			(a.normal_balance = 'debit' AND p.direction = 'debit')
		) THEN 'credit'
		WHEN t.status = 'succeeded' AND p.id IS NOT NULL THEN 'debit'
		WHEN t.direction = 'inbound' THEN 'credit'
		ELSE 'debit'
	END AS direction,
	t.status,
	t.ledger_status,
	t.provider_status,
	t.reconciliation_status,
	t.amount_minor,
	CASE
		WHEN t.status != 'succeeded' THEN 0
		WHEN a.normal_balance = 'credit' AND p.direction = 'credit' THEN p.amount_minor
		WHEN a.normal_balance = 'debit' AND p.direction = 'debit' THEN p.amount_minor
		ELSE -p.amount_minor
	END AS signed_amount_minor,
	t.currency_id,
	t.narration,
	CASE WHEN cp_a.kind = 'customer' THEN cp.account_id END AS counterparty_account_id,
	t.provider,
	t.provider_reference,
	t.created_at
FROM transfers t
JOIN accounts a ON a.institution_id = t.institution_id AND a.id = $2
LEFT JOIN postings p ON p.institution_id = t.institution_id AND p.journal_entry_id = t.journal_entry_id AND p.account_id = $2
LEFT JOIN postings cp ON cp.institution_id = t.institution_id AND cp.journal_entry_id = t.journal_entry_id AND cp.account_id != $2
LEFT JOIN accounts cp_a ON cp_a.institution_id = cp.institution_id AND cp_a.id = cp.account_id
WHERE t.institution_id = $1
  AND (t.account_id = $2 OR p.account_id = $2)
  AND (
	$3::timestamptz IS NULL OR
	t.created_at < $3 OR
	($4::uuid IS NOT NULL AND t.created_at = $3 AND t.id < $4::uuid)
  )
ORDER BY t.created_at DESC, t.id DESC
LIMIT $5`, institutionID, accountID, beforeCreatedAt, beforeTransferID, options.Limit)
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
		err = normalizeSQLError(err)
		if errors.Is(err, ErrNotFound) {
			return nil, ErrDataIntegrity
		}
		return nil, err
	}
	return &out, nil
}

func lockTransferAccountBalances(ctx context.Context, tx TxRunner, institutionID, accountID, clearingAccountID string) (*lockedAccountBalance, *lockedAccountBalance, error) {
	if accountID == clearingAccountID {
		return nil, nil, ErrInvalidRequest
	}
	firstID, secondID := accountID, clearingAccountID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}

	first, err := lockAccountBalance(ctx, tx, institutionID, firstID)
	if err != nil {
		return nil, nil, err
	}
	second, err := lockAccountBalance(ctx, tx, institutionID, secondID)
	if err != nil {
		return nil, nil, err
	}
	if first.Account.ID == accountID {
		return first, second, nil
	}
	return second, first, nil
}

func normalizeAccountSQLError(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrConflict
	}
	return normalizeSQLError(err)
}
