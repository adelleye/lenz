//go:build integration

package corebanking

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestSQLStoreTransferSpineIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewSQLStore(db))

	seed, err := svc.SeedDemo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if seed.Institution.ID != DemoInstitutionID || seed.Customer.ID != DemoCustomerID || seed.Account.ID != DemoCustomerAccountID {
		t.Fatalf("demo seed mismatch: %+v", seed)
	}

	accounts, err := svc.ListCustomerAccounts(ctx, DemoInstitutionID, DemoCustomerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != DemoCustomerAccountID {
		t.Fatalf("expected one demo customer account, got %+v", accounts)
	}

	inbound := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       500000,
		IdempotencyKey:    "sql-in-001",
		ProviderEventID:   "sql-provider-event-001",
		ProviderReference: "sql-provider-ref-001",
		Narration:         "SQL inbound proof",
	})
	assertStatus(t, inbound, TransferStatusSucceeded)
	assertSQLBalance(t, svc, ctx, 500000)
	assertSQLJournalBalanced(t, svc, ctx, inbound, 500000)

	duplicateIdempotency := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       500000,
		IdempotencyKey:    "sql-in-001",
		ProviderEventID:   "sql-provider-event-001",
		ProviderReference: "sql-provider-ref-001",
		Narration:         "SQL duplicate idempotency proof",
	})
	if duplicateIdempotency.ID != inbound.ID {
		t.Fatalf("duplicate idempotency key posted a new transfer: first=%s duplicate=%s", inbound.ID, duplicateIdempotency.ID)
	}
	assertSQLBalance(t, svc, ctx, 500000)

	duplicateProviderEvent := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       500000,
		IdempotencyKey:    "sql-in-002",
		ProviderEventID:   "sql-provider-event-001",
		ProviderReference: "sql-provider-ref-001",
		Narration:         "SQL duplicate provider event proof",
	})
	if duplicateProviderEvent.ID != inbound.ID {
		t.Fatalf("duplicate provider event posted a new transfer: first=%s duplicate=%s", inbound.ID, duplicateProviderEvent.ID)
	}
	assertSQLBalance(t, svc, ctx, 500000)

	outbound := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       125000,
		IdempotencyKey:    "sql-out-001",
		ProviderReference: "sql-out-provider-ref-001",
		Narration:         "SQL outbound proof",
	})
	assertStatus(t, outbound, TransferStatusSucceeded)
	assertSQLBalance(t, svc, ctx, 375000)
	assertSQLJournalBalanced(t, svc, ctx, outbound, 125000)

	failed := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    999999999,
		IdempotencyKey: "sql-out-failed-001",
		Narration:      "SQL insufficient funds proof",
	})
	assertStatus(t, failed, TransferStatusFailed)
	if failed.JournalEntryID != nil || failed.FailureReason == nil || *failed.FailureReason != "insufficient_funds" {
		t.Fatalf("failed transfer should record insufficient funds without a journal: %+v", failed)
	}
	assertSQLBalance(t, svc, ctx, 375000)

	pending := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     100000,
		IdempotencyKey:  "sql-pending-001",
		ProviderEventID: "sql-provider-event-pending-001",
		Status:          TransferStatusPending,
		Narration:       "SQL pending proof",
	})
	assertStatus(t, pending, TransferStatusPending)
	if pending.JournalEntryID != nil {
		t.Fatalf("pending transfer should not have a journal: %+v", pending)
	}
	assertSQLBalance(t, svc, ctx, 375000)

	reversal := reverseTransfer(t, svc, ctx, inbound.ID, "sql-reversal-001")
	assertStatus(t, reversal, TransferStatusSucceeded)
	if reversal.Direction != TransferDirectionReversal || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != inbound.ID {
		t.Fatalf("reversal did not reference the original transfer: %+v", reversal)
	}
	assertSQLBalance(t, svc, ctx, -125000)
	assertSQLJournalBalanced(t, svc, ctx, reversal, 500000)

	_, err = svc.ReverseTransfer(ctx, DemoInstitutionID, inbound.ID, "sql-in-001")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected unrelated idempotency key collision to fail, got %v", err)
	}

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	assertSQLHistory(t, history, inbound.ID, outbound.ID, pending.ID, failed.ID, reversal.ID)

	if err := assertAllSQLJournalsBalanced(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant balance read to fail, got %v", err)
	}
}

func integrationDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv("LENZ_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set LENZ_INTEGRATION_DATABASE_URL or DATABASE_URL to run SQL integration tests")
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	t.Cleanup(func() {
		resetIntegrationSchema(t, db)
		_ = db.Close()
	})
	resetIntegrationSchema(t, db)
	return db
}

func resetIntegrationSchema(t *testing.T, db *sqlx.DB) {
	t.Helper()
	_, err := db.Exec(`
TRUNCATE TABLE
	audit_events,
	provider_events,
	transfers,
	postings,
	journal_entries,
	account_balances,
	accounts,
	customers,
	branches,
	institutions,
	countries,
	currencies
RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset integration schema: %v", err)
	}
}

func assertSQLBalance(t *testing.T, svc *Service, ctx context.Context, want int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != want || balance.LedgerMinor != want {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want=%d", balance.AvailableMinor, balance.LedgerMinor, want)
	}
}

func assertSQLJournalBalanced(t *testing.T, svc *Service, ctx context.Context, transfer *Transfer, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected transfer to have journal entry: %+v", transfer)
	}
	journal, err := svc.GetJournal(ctx, transfer.InstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Balanced || journal.JournalEntry.TotalDebitMinor != amountMinor || journal.JournalEntry.TotalCreditMinor != amountMinor || len(journal.Postings) != 2 {
		t.Fatalf("journal is not balanced for %d: %+v", amountMinor, journal)
	}
	var debit, credit int64
	for _, posting := range journal.Postings {
		switch posting.Direction {
		case PostingDebit:
			debit += posting.AmountMinor
		case PostingCredit:
			credit += posting.AmountMinor
		}
	}
	if debit != amountMinor || credit != amountMinor {
		t.Fatalf("posting totals mismatch: debit=%d credit=%d want=%d", debit, credit, amountMinor)
	}
}

func assertSQLHistory(t *testing.T, history []Transaction, inboundID, outboundID, pendingID, failedID, reversalID string) {
	t.Helper()
	if len(history) != 5 {
		t.Fatalf("expected five transaction history rows, got %d: %+v", len(history), history)
	}
	seen := map[string]Transaction{}
	for _, txn := range history {
		seen[txn.TransferID] = txn
	}
	expect := map[string]struct {
		status      string
		signedMinor int64
		hasJournal  bool
	}{
		inboundID:  {status: TransferStatusSucceeded, signedMinor: 500000, hasJournal: true},
		outboundID: {status: TransferStatusSucceeded, signedMinor: -125000, hasJournal: true},
		pendingID:  {status: TransferStatusPending, signedMinor: 0, hasJournal: false},
		failedID:   {status: TransferStatusFailed, signedMinor: 0, hasJournal: false},
		reversalID: {status: TransferStatusSucceeded, signedMinor: -500000, hasJournal: true},
	}
	for transferID, want := range expect {
		got, ok := seen[transferID]
		if !ok {
			t.Fatalf("missing history row for transfer %s: %+v", transferID, history)
		}
		if got.Status != want.status || got.SignedMinor != want.signedMinor {
			t.Fatalf("history mismatch for %s: got %+v want status=%s signed=%d", transferID, got, want.status, want.signedMinor)
		}
		if want.hasJournal && got.JournalEntryID == nil {
			t.Fatalf("succeeded history row must be backed by a Lenz journal: %+v", got)
		}
		if !want.hasJournal && got.JournalEntryID != nil {
			t.Fatalf("non-posted history row should not have a journal: %+v", got)
		}
	}
}

func assertAllSQLJournalsBalanced(ctx context.Context, db *sqlx.DB) error {
	var mismatches int
	err := db.GetContext(ctx, &mismatches, `
WITH journal_totals AS (
	SELECT
		je.id,
		je.total_debit_minor,
		je.total_credit_minor,
		COALESCE(SUM(CASE WHEN p.direction = 'debit' THEN p.amount_minor ELSE 0 END), 0) AS posting_debits,
		COALESCE(SUM(CASE WHEN p.direction = 'credit' THEN p.amount_minor ELSE 0 END), 0) AS posting_credits
	FROM journal_entries je
	LEFT JOIN postings p
		ON p.institution_id = je.institution_id
		AND p.journal_entry_id = je.id
	WHERE je.institution_id = $1
	GROUP BY je.id
)
SELECT COUNT(*)
FROM journal_totals
WHERE total_debit_minor <> total_credit_minor
	OR total_debit_minor <> posting_debits
	OR total_credit_minor <> posting_credits`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("found unbalanced SQL journal entries")
	}
	return nil
}

func assertSQLBalancesMatchPostings(ctx context.Context, db *sqlx.DB) error {
	var mismatches int
	err := db.GetContext(ctx, &mismatches, `
WITH posting_balances AS (
	SELECT
		a.id AS account_id,
		COALESCE(SUM(
			CASE
				WHEN (a.normal_balance = 'credit' AND p.direction = 'credit')
					OR (a.normal_balance = 'debit' AND p.direction = 'debit')
				THEN p.amount_minor
				ELSE -p.amount_minor
			END
		), 0) AS computed_minor
	FROM accounts a
	LEFT JOIN postings p
		ON p.institution_id = a.institution_id
		AND p.account_id = a.id
	WHERE a.institution_id = $1
	GROUP BY a.id
)
SELECT COUNT(*)
FROM account_balances b
JOIN posting_balances pb ON pb.account_id = b.account_id
WHERE b.institution_id = $1
	AND (b.available_minor <> pb.computed_minor OR b.ledger_minor <> pb.computed_minor)`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("SQL account balances do not match postings")
	}
	return nil
}
