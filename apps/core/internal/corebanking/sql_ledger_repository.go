package corebanking

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type sqlLedgerRepository struct {
	db *sqlx.DB
}

func (r *sqlLedgerRepository) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	var entry JournalEntry
	if err := r.db.GetContext(ctx, &entry, `SELECT id, institution_id, transfer_id, entry_type, currency_id, narration, total_debit_minor, total_credit_minor, created_at FROM journal_entries WHERE institution_id = $1 AND id = $2`, institutionID, journalEntryID); err != nil {
		return nil, normalizeSQLError(err)
	}
	var postings []Posting
	if err := r.db.SelectContext(ctx, &postings, `SELECT id, institution_id, journal_entry_id, account_id, direction, amount_minor, currency_id, created_at FROM postings WHERE institution_id = $1 AND journal_entry_id = $2 ORDER BY created_at, id`, institutionID, journalEntryID); err != nil {
		return nil, err
	}
	return &JournalWithPostings{JournalEntry: entry, Postings: postings, Balanced: entry.TotalDebitMinor == entry.TotalCreditMinor}, nil
}

type postingBalanceOptions struct {
	HeldAccountID string
}

func (r *sqlLedgerRepository) postJournal(ctx context.Context, tx TxRunner, input RecordTransferInput, transferID string, now time.Time, options postingBalanceOptions) (string, error) {
	journalID := uuid.Must(uuid.NewRandom()).String()
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
	normalBalances, err := r.normalBalances(ctx, tx, input.InstitutionID, debitAccountID, creditAccountID)
	if err != nil {
		return "", err
	}
	for _, posting := range postings {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO postings (id, institution_id, journal_entry_id, account_id, direction, amount_minor, currency_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			uuid.Must(uuid.NewRandom()).String(), input.InstitutionID, journalID, posting.accountID, posting.direction, input.AmountMinor, input.CurrencyID, now); err != nil {
			return "", err
		}
		availableDeltaOverride := false
		availableDelta := int64(0)
		if posting.accountID == options.HeldAccountID {
			availableDeltaOverride = true
		}
		if err := r.applyPostingBalance(ctx, tx, input.InstitutionID, posting.accountID, normalBalances[posting.accountID], posting.direction, input.AmountMinor, journalID, now, availableDeltaOverride, availableDelta); err != nil {
			return "", err
		}
	}
	return journalID, nil
}

func (r *sqlLedgerRepository) applyPostingBalance(ctx context.Context, tx TxRunner, institutionID, accountID, normalBalance, direction string, amountMinor int64, journalID string, now time.Time, availableDeltaOverride bool, availableDelta int64) error {
	delta := -amountMinor
	if (normalBalance == NormalBalanceDebit && direction == PostingDebit) || (normalBalance == NormalBalanceCredit && direction == PostingCredit) {
		delta = amountMinor
	}
	if !availableDeltaOverride {
		availableDelta = delta
	}
	_, err := tx.ExecContext(ctx, `
UPDATE account_balances
SET available_minor = available_minor + $1,
    ledger_minor = ledger_minor + $2,
    last_journal_entry_id = $3,
    updated_at = $4
WHERE institution_id = $5 AND account_id = $6`, availableDelta, delta, journalID, now, institutionID, accountID)
	return err
}

func (r *sqlLedgerRepository) normalBalances(ctx context.Context, tx TxRunner, institutionID, firstAccountID, secondAccountID string) (map[string]string, error) {
	rows := []struct {
		ID            string `db:"id"`
		NormalBalance string `db:"normal_balance"`
	}{}
	if err := tx.SelectContext(ctx, &rows, `
SELECT id, normal_balance
FROM accounts
WHERE institution_id = $1 AND id IN ($2, $3)`, institutionID, firstAccountID, secondAccountID); err != nil {
		return nil, err
	}
	balances := make(map[string]string, len(rows))
	for _, row := range rows {
		balances[row.ID] = row.NormalBalance
	}
	if _, ok := balances[firstAccountID]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := balances[secondAccountID]; !ok {
		return nil, ErrNotFound
	}
	return balances, nil
}
