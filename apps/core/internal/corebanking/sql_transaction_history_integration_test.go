//go:build integration

package corebanking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestSQLTransactionHistoryGoal07(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	emptyAccount := createSQLCustomerAccount(t, svc, ctx, "SQL", "EmptyHistory", "sql.empty.history@example.com", "7234567890", "SQL Empty History")
	emptyHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, emptyAccount.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if emptyHistory == nil || len(emptyHistory) != 0 {
		t.Fatalf("expected empty non-nil SQL history, got %#v", emptyHistory)
	}
	if _, err := svc.GetTransactions(ctx, "99999999-9999-9999-9999-999999999999", emptyAccount.ID, ListTransactionsOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant SQL history lookup to return not found, got %v", err)
	}

	account := createSQLCustomerAccount(t, svc, ctx, "SQL", "History", "sql.history@example.com", "7234567891", "SQL History")
	credit := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    60000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-history-credit",
		Reference:      "sql-history-credit-ref",
	})
	debit := mustInternalDebit(t, svc, ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    11000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-history-debit",
		Reference:      "sql-history-debit-ref",
	})
	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertHistoryRow(t, history, credit.ID, TransactionDirectionCredit, 60000, credit.JournalEntryID, ProviderLedgerInternal, "sql-history-credit-ref", nil)
	assertHistoryRow(t, history, debit.ID, TransactionDirectionDebit, -11000, debit.JournalEntryID, ProviderLedgerInternal, "sql-history-debit-ref", nil)
	assertSQLHistoryReconciles(t, ctx, db, account.ID, history)

	source := createSQLCustomerAccount(t, svc, ctx, "SQL", "HistorySource", "sql.history.source@example.com", "7234567892", "SQL History Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "SQL", "HistoryDestination", "sql.history.destination@example.com", "7234567893", "SQL History Destination")
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-history-transfer-funding",
	})
	transfer := mustInternalTransfer(t, svc, ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          9000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-history-transfer",
		Reference:            "sql-history-transfer-ref",
	})
	sourceHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, source.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	destinationHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, destination.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertHistoryRow(t, sourceHistory, transfer.ID, TransactionDirectionDebit, -9000, transfer.JournalEntryID, ProviderLedgerInternal, "sql-history-transfer-ref", &destination.ID)
	assertHistoryRow(t, destinationHistory, transfer.ID, TransactionDirectionCredit, 9000, transfer.JournalEntryID, ProviderLedgerInternal, "sql-history-transfer-ref", &source.ID)
	assertSQLHistoryReconciles(t, ctx, db, source.ID, sourceHistory)
	assertSQLHistoryReconciles(t, ctx, db, destination.ID, destinationHistory)
}

func TestSQLTransactionHistoryPaginationGoal07(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	account := createSQLCustomerAccount(t, svc, ctx, "SQL", "HistoryPage", "sql.history.page@example.com", "7234567894", "SQL History Page")
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		transfer := mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    int64(1000 + i),
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-history-page-" + string(rune('a'+i)),
		})
		setSQLTransferCreatedAt(t, ctx, db, transfer.ID, base)
	}

	first, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("expected first SQL page of two rows, got %+v", first)
	}
	second, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{
		Limit:            2,
		BeforeCreatedAt:  &first[len(first)-1].CreatedAt,
		BeforeTransferID: first[len(first)-1].TransferID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("expected second SQL page of two rows, got %+v", second)
	}
	seen := map[string]bool{}
	for _, row := range append(first, second...) {
		if seen[row.TransferID] {
			t.Fatalf("SQL pagination returned duplicate transfer %s", row.TransferID)
		}
		seen[row.TransferID] = true
	}
	assertHistoryNewestFirst(t, first)
	assertHistoryNewestFirst(t, second)
}

func assertSQLHistoryReconciles(t *testing.T, ctx context.Context, db *sqlx.DB, accountID string, history []Transaction) {
	t.Helper()
	for _, row := range history {
		var transferCount int
		if err := db.GetContext(ctx, &transferCount, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND id = $2 AND account_id IS NOT NULL`, row.InstitutionID, row.TransferID); err != nil {
			t.Fatal(err)
		}
		if transferCount != 1 {
			t.Fatalf("history row does not reconcile to one transfer: %+v", row)
		}
		if row.JournalEntryID == nil {
			continue
		}
		var postingCount int
		if err := db.GetContext(ctx, &postingCount, `
SELECT COUNT(*)
FROM journal_entries je
JOIN postings p ON p.institution_id = je.institution_id AND p.journal_entry_id = je.id
WHERE je.institution_id = $1
  AND je.id = $2
  AND je.transfer_id = $3
  AND p.account_id = $4
  AND p.amount_minor = $5`, row.InstitutionID, *row.JournalEntryID, row.TransferID, accountID, row.AmountMinor); err != nil {
			t.Fatal(err)
		}
		if postingCount != 1 {
			t.Fatalf("history row does not reconcile to one journal posting: %+v", row)
		}
	}
}

func setSQLTransferCreatedAt(t *testing.T, ctx context.Context, db *sqlx.DB, transferID string, createdAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE transfers SET created_at = $1 WHERE id = $2`, createdAt, transferID); err != nil {
		t.Fatal(err)
	}
}
