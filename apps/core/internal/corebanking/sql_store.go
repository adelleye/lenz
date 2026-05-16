package corebanking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type SQLStore struct {
	db *sqlx.DB
}

func NewSQLStore(db *sqlx.DB) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) EnsureDemoData(ctx context.Context) (*SeedResult, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessCommitted(tx)

	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `
INSERT INTO currencies (id, name, created_at, updated_at)
VALUES ('NGN', 'Nigerian Naira', $1, $1)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = EXCLUDED.updated_at`, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO countries (id, name, flag, currency, is_supported, meta, created_at, updated_at)
VALUES ('NG', 'Nigeria', 'NG', 'NGN', true, '{}'::jsonb, $1, $1)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, currency = EXCLUDED.currency, is_supported = EXCLUDED.is_supported, updated_at = EXCLUDED.updated_at`, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO institutions (id, name, short_name, code, nuban_prefix, country_id, currency_id, status, meta, created_at, updated_at)
VALUES ($1, 'Lenz Demo Microfinance Bank', 'Lenz Demo', '999001', '999', 'NG', 'NGN', 'active', '{}'::jsonb, $2, $2)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, short_name = EXCLUDED.short_name, code = EXCLUDED.code, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		DemoInstitutionID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO branches (id, institution_id, code, name, meta, status, created_at, updated_at)
VALUES ($1, $2, 'HQ', 'Demo HQ', '{}'::jsonb, 'active', $3, $3)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		DemoBranchID, DemoInstitutionID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO customers (id, institution_id, branch_id, first_name, last_name, email, phone, status, meta, created_at, updated_at)
VALUES ($1, $2, $3, 'Ada', 'Demo', 'ada.demo@example.com', '+2348000000000', 'active', '{}'::jsonb, $4, $4)
ON CONFLICT (id) DO UPDATE SET first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		DemoCustomerID, DemoInstitutionID, DemoBranchID, now); err != nil {
		return nil, err
	}
	customerID := DemoCustomerID
	if _, err = tx.ExecContext(ctx, `
INSERT INTO accounts (id, institution_id, customer_id, account_number, name, kind, currency_id, normal_balance, status, created_at, updated_at)
VALUES ($1, $2, $3, '9990000001', 'Ada Demo Wallet', 'customer', 'NGN', 'credit', 'active', $4, $4)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		DemoCustomerAccountID, DemoInstitutionID, customerID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO accounts (id, institution_id, customer_id, account_number, name, kind, currency_id, normal_balance, status, created_at, updated_at)
VALUES ($1, $2, NULL, '9999999999', 'Mock NIP Clearing', 'internal', 'NGN', 'debit', 'active', $3, $3)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		DemoClearingAccountID, DemoInstitutionID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO account_balances (account_id, institution_id, available_minor, ledger_minor, currency_id, updated_at)
VALUES ($1, $2, 0, 0, 'NGN', $3)
ON CONFLICT (account_id) DO NOTHING`, DemoCustomerAccountID, DemoInstitutionID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
INSERT INTO account_balances (account_id, institution_id, available_minor, ledger_minor, currency_id, updated_at)
VALUES ($1, $2, 0, 0, 'NGN', $3)
ON CONFLICT (account_id) DO NOTHING`, DemoClearingAccountID, DemoInstitutionID, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return s.seedResult(ctx)
}

func (s *SQLStore) seedResult(ctx context.Context) (*SeedResult, error) {
	var out SeedResult
	if err := s.db.GetContext(ctx, &out.Institution, `SELECT id, name, short_name, code, currency_id, status, created_at, updated_at FROM institutions WHERE id = $1`, DemoInstitutionID); err != nil {
		return nil, err
	}
	if err := s.db.GetContext(ctx, &out.Branch, `SELECT id, institution_id, code, name, status, created_at, updated_at FROM branches WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoBranchID); err != nil {
		return nil, err
	}
	if err := s.db.GetContext(ctx, &out.Customer, `SELECT id, institution_id, branch_id, first_name, last_name, email, phone, status, created_at, updated_at FROM customers WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoCustomerID); err != nil {
		return nil, err
	}
	if err := s.db.GetContext(ctx, &out.Account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoCustomerAccountID); err != nil {
		return nil, err
	}
	if err := s.db.GetContext(ctx, &out.Clearing, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, DemoClearingAccountID); err != nil {
		return nil, err
	}
	return &out, nil
}

const accountSelectSQL = `SELECT id, institution_id, customer_id, account_number, name, kind, currency_id, normal_balance, status, created_at, updated_at FROM accounts`

func (s *SQLStore) ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	var accounts []Account
	err := s.db.SelectContext(ctx, &accounts, accountSelectSQL+` WHERE institution_id = $1 AND customer_id = $2 ORDER BY created_at`, institutionID, customerID)
	return accounts, err
}

func (s *SQLStore) GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error) {
	var account Account
	err := s.db.GetContext(ctx, &account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, institutionID, accountID)
	return &account, normalizeSQLError(err)
}

func (s *SQLStore) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	var balance AccountBalance
	err := s.db.GetContext(ctx, &balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2`, institutionID, accountID)
	return &balance, normalizeSQLError(err)
}

func (s *SQLStore) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	var transfer Transfer
	err := s.db.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND id = $2`, institutionID, transferID)
	return &transfer, normalizeSQLError(err)
}

func (s *SQLStore) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	var transfers []Transfer
	err := s.db.SelectContext(ctx, &transfers, transferSelectSQL+` WHERE institution_id = $1 ORDER BY created_at DESC LIMIT 100`, institutionID)
	return transfers, err
}

func (s *SQLStore) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	var entry JournalEntry
	if err := s.db.GetContext(ctx, &entry, `SELECT id, institution_id, transfer_id, entry_type, currency_id, narration, total_debit_minor, total_credit_minor, created_at FROM journal_entries WHERE institution_id = $1 AND id = $2`, institutionID, journalEntryID); err != nil {
		return nil, normalizeSQLError(err)
	}
	var postings []Posting
	if err := s.db.SelectContext(ctx, &postings, `SELECT id, institution_id, journal_entry_id, account_id, direction, amount_minor, currency_id, created_at FROM postings WHERE institution_id = $1 AND journal_entry_id = $2 ORDER BY created_at, id`, institutionID, journalEntryID); err != nil {
		return nil, err
	}
	return &JournalWithPostings{JournalEntry: entry, Postings: postings, Balanced: entry.TotalDebitMinor == entry.TotalCreditMinor}, nil
}

func (s *SQLStore) ListTransactions(ctx context.Context, institutionID, accountID string) ([]Transaction, error) {
	var txns []Transaction
	err := s.db.SelectContext(ctx, &txns, `
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
ORDER BY t.created_at DESC`, institutionID, accountID)
	return txns, err
}

func (s *SQLStore) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessCommitted(tx)

	transfer, err := recordTransferTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return transfer, nil
}

func (s *SQLStore) ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackUnlessCommitted(tx)

	input.InstitutionID = strings.TrimSpace(input.InstitutionID)
	input.TransferID = strings.TrimSpace(input.TransferID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.InstitutionID == "" || input.TransferID == "" || input.IdempotencyKey == "" {
		return nil, ErrInvalidRequest
	}

	original, err := getTransferTx(ctx, tx, input.InstitutionID, input.TransferID)
	if err != nil {
		return nil, err
	}
	if original.Status != TransferStatusSucceeded || original.JournalEntryID == nil || original.Direction == TransferDirectionReversal {
		return nil, ErrInvalidRequest
	}

	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = original.Provider
	}
	providerReference := strings.TrimSpace(input.ProviderReference)
	if providerReference == "" {
		originalReference := strings.TrimSpace(original.ProviderReference)
		if originalReference == "" {
			originalReference = original.ID
		}
		providerReference = "reversal:" + originalReference
	}
	narration := strings.TrimSpace(input.Narration)
	if narration == "" {
		narration = "Reversal of " + original.ID
	}

	direction := TransferDirectionOutbound
	if original.Direction == TransferDirectionOutbound {
		direction = TransferDirectionInbound
	}
	transfer, err := recordTransferTx(ctx, tx, RecordTransferInput{
		InstitutionID:        input.InstitutionID,
		AccountID:            original.AccountID,
		ClearingAccountID:    DemoClearingAccountID,
		Direction:            direction,
		Status:               TransferStatusSucceeded,
		AmountMinor:          original.AmountMinor,
		CurrencyID:           original.CurrencyID,
		IdempotencyKey:       input.IdempotencyKey,
		Provider:             provider,
		ProviderReference:    providerReference,
		ProviderEventID:      strings.TrimSpace(input.ProviderEventID),
		ReversalOfTransferID: original.ID,
		FailureReason:        strings.TrimSpace(input.FailureReason),
		Narration:            narration,
	})
	if err != nil {
		return nil, err
	}
	if transfer.ReversalOfTransferID == nil || *transfer.ReversalOfTransferID != original.ID {
		return nil, ErrConflict
	}
	transfer.Direction = TransferDirectionReversal
	if _, err = tx.ExecContext(ctx, `UPDATE transfers SET direction = 'reversal', updated_at = $1 WHERE institution_id = $2 AND id = $3`, time.Now().UTC(), input.InstitutionID, transfer.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return transfer, nil
}

const transferSelectSQL = `SELECT id, institution_id, account_id, direction, status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, failure_reason, narration, created_at, updated_at FROM transfers`

func recordTransferTx(ctx context.Context, tx *sqlx.Tx, input RecordTransferInput) (*Transfer, error) {
	if existing, err := getTransferByIdempotencyTx(ctx, tx, input.InstitutionID, input.IdempotencyKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if input.ProviderEventID != "" {
		if existing, err := getTransferByProviderEventTx(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID); err == nil {
			return existing, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	account, err := lockAccountBalanceTx(ctx, tx, input.InstitutionID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if _, err = lockAccountBalanceTx(ctx, tx, input.InstitutionID, input.ClearingAccountID); err != nil {
		return nil, err
	}

	status := input.Status
	failureReason := input.FailureReason
	if status == TransferStatusSucceeded && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" && account.Balance.AvailableMinor < input.AmountMinor {
		status = TransferStatusFailed
		failureReason = "insufficient_funds"
	}

	transferID := newID()
	now := time.Now().UTC()
	var providerEventID *string
	if input.ProviderEventID != "" {
		providerEventID = &input.ProviderEventID
	}
	var reversalOf *string
	if input.ReversalOfTransferID != "" {
		reversalOf = &input.ReversalOfTransferID
	}
	var failure *string
	if failureReason != "" {
		failure = &failureReason
	}

	if input.ProviderEventID != "" {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO provider_events (id, institution_id, provider, provider_event_id, provider_reference, transfer_id, created_at)
VALUES ($1, $2, $3, $4, $5, NULL, $6)`,
			newID(), input.InstitutionID, input.Provider, input.ProviderEventID, input.ProviderReference, now); err != nil {
			if isUniqueViolation(err) {
				return getTransferByProviderEventTx(ctx, tx, input.InstitutionID, input.Provider, input.ProviderEventID)
			}
			return nil, err
		}
	}

	var journalEntryID *string
	if status == TransferStatusSucceeded {
		journalID, err := postJournalTx(ctx, tx, input, transferID, now)
		if err != nil {
			return nil, err
		}
		journalEntryID = &journalID
	}

	transfer := Transfer{
		ID:                   transferID,
		InstitutionID:        input.InstitutionID,
		AccountID:            input.AccountID,
		Direction:            input.Direction,
		Status:               status,
		AmountMinor:          input.AmountMinor,
		CurrencyID:           input.CurrencyID,
		IdempotencyKey:       input.IdempotencyKey,
		Provider:             input.Provider,
		ProviderReference:    input.ProviderReference,
		ProviderEventID:      providerEventID,
		JournalEntryID:       journalEntryID,
		ReversalOfTransferID: reversalOf,
		FailureReason:        failure,
		Narration:            input.Narration,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if _, err = tx.NamedExecContext(ctx, `
INSERT INTO transfers (id, institution_id, account_id, direction, status, amount_minor, currency_id, idempotency_key, provider, provider_reference, provider_event_id, journal_entry_id, reversal_of_transfer_id, failure_reason, narration, created_at, updated_at)
VALUES (:id, :institution_id, :account_id, :direction, :status, :amount_minor, :currency_id, :idempotency_key, :provider, :provider_reference, :provider_event_id, :journal_entry_id, :reversal_of_transfer_id, :failure_reason, :narration, :created_at, :updated_at)`, transfer); err != nil {
		if isUniqueViolation(err) {
			return getTransferByIdempotencyTx(ctx, tx, input.InstitutionID, input.IdempotencyKey)
		}
		return nil, err
	}
	if input.ProviderEventID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE provider_events SET transfer_id = $1 WHERE institution_id = $2 AND provider = $3 AND provider_event_id = $4`, transfer.ID, input.InstitutionID, input.Provider, input.ProviderEventID); err != nil {
			return nil, err
		}
	}
	return &transfer, nil
}

type lockedAccountBalance struct {
	Account
	Balance AccountBalance
}

func lockAccountBalanceTx(ctx context.Context, tx *sqlx.Tx, institutionID, accountID string) (*lockedAccountBalance, error) {
	var out lockedAccountBalance
	if err := tx.GetContext(ctx, &out.Account, accountSelectSQL+` WHERE institution_id = $1 AND id = $2 FOR UPDATE`, institutionID, accountID); err != nil {
		return nil, normalizeSQLError(err)
	}
	if err := tx.GetContext(ctx, &out.Balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2 FOR UPDATE`, institutionID, accountID); err != nil {
		return nil, normalizeSQLError(err)
	}
	return &out, nil
}

func postJournalTx(ctx context.Context, tx *sqlx.Tx, input RecordTransferInput, transferID string, now time.Time) (string, error) {
	journalID := newID()
	entryDirection := input.Direction
	if input.ReversalOfTransferID != "" {
		entryDirection = TransferDirectionReversal
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO journal_entries (id, institution_id, transfer_id, entry_type, currency_id, narration, total_debit_minor, total_credit_minor, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8)`,
		journalID, input.InstitutionID, transferID, entryDirection, input.CurrencyID, input.Narration, input.AmountMinor, now); err != nil {
		return "", err
	}

	debitAccountID := input.ClearingAccountID
	creditAccountID := input.AccountID
	if input.Direction == TransferDirectionOutbound {
		debitAccountID = input.AccountID
		creditAccountID = input.ClearingAccountID
	}
	postings := []struct {
		accountID string
		direction string
	}{
		{accountID: debitAccountID, direction: PostingDebit},
		{accountID: creditAccountID, direction: PostingCredit},
	}
	for _, posting := range postings {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO postings (id, institution_id, journal_entry_id, account_id, direction, amount_minor, currency_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			newID(), input.InstitutionID, journalID, posting.accountID, posting.direction, input.AmountMinor, input.CurrencyID, now); err != nil {
			return "", err
		}
		if err := applyPostingBalanceTx(ctx, tx, input.InstitutionID, posting.accountID, posting.direction, input.AmountMinor, journalID, now); err != nil {
			return "", err
		}
	}
	return journalID, nil
}

func applyPostingBalanceTx(ctx context.Context, tx *sqlx.Tx, institutionID, accountID, direction string, amountMinor int64, journalID string, now time.Time) error {
	var normalBalance string
	if err := tx.GetContext(ctx, &normalBalance, `SELECT normal_balance FROM accounts WHERE institution_id = $1 AND id = $2`, institutionID, accountID); err != nil {
		return normalizeSQLError(err)
	}
	delta := -amountMinor
	if (normalBalance == NormalBalanceDebit && direction == PostingDebit) || (normalBalance == NormalBalanceCredit && direction == PostingCredit) {
		delta = amountMinor
	}
	_, err := tx.ExecContext(ctx, `
UPDATE account_balances
SET available_minor = available_minor + $1,
    ledger_minor = ledger_minor + $1,
    last_journal_entry_id = $2,
    updated_at = $3
WHERE institution_id = $4 AND account_id = $5`, delta, journalID, now, institutionID, accountID)
	return err
}

func getTransferTx(ctx context.Context, tx *sqlx.Tx, institutionID, transferID string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND id = $2 FOR UPDATE`, institutionID, transferID)
	return &transfer, normalizeSQLError(err)
}

func getTransferByIdempotencyTx(ctx context.Context, tx *sqlx.Tx, institutionID, idempotencyKey string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+` WHERE institution_id = $1 AND idempotency_key = $2`, institutionID, idempotencyKey)
	return &transfer, normalizeSQLError(err)
}

func getTransferByProviderEventTx(ctx context.Context, tx *sqlx.Tx, institutionID, provider, providerEventID string) (*Transfer, error) {
	var transfer Transfer
	err := tx.GetContext(ctx, &transfer, transferSelectSQL+`
 WHERE institution_id = $1 AND id = (
	SELECT transfer_id FROM provider_events
	WHERE institution_id = $1 AND provider = $2 AND provider_event_id = $3 AND transfer_id IS NOT NULL
 )`, institutionID, provider, providerEventID)
	return &transfer, normalizeSQLError(err)
}

func normalizeSQLError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func rollbackUnlessCommitted(tx *sqlx.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		fmt.Printf("rollback failed: %v\n", err)
	}
}
