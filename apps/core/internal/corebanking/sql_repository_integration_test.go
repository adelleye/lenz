//go:build integration

package corebanking

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

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

type concurrentTransferResult struct {
	transfer *Transfer
	err      error
}

func seededSQLService(t *testing.T, db *sqlx.DB, ctx context.Context) *Service {
	t.Helper()
	svc := NewService(NewRepository(db), NewMockNIPProvider())
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return svc
}

func externalInboundSQLInput(t *testing.T, institutionID string, payload map[string]any) ExternalInboundEventInput {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	provider, _ := payload["provider"].(string)
	return ExternalInboundEventInput{
		InstitutionID: institutionID,
		Provider:      provider,
		Payload:       body,
		Headers:       map[string]string{"X-Institution-ID": institutionID},
	}
}

func createSQLCustomerAccount(t *testing.T, svc *Service, ctx context.Context, firstName, lastName, email, accountNumber, accountName string) *Account {
	t.Helper()
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     firstName,
		LastName:      lastName,
		Email:         email,
		Phone:         "+2348012345684",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: accountNumber,
		Name:          accountName,
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func mustGetSQLTransfer(t *testing.T, svc *Service, ctx context.Context, transferID string) *Transfer {
	t.Helper()
	transfer, err := svc.GetTransfer(ctx, DemoInstitutionID, transferID)
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func runConcurrentTransfers(t *testing.T, count int, fn func(int) (*Transfer, error)) []concurrentTransferResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]concurrentTransferResult, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			transfer, err := fn(i)
			results[i] = concurrentTransferResult{transfer: transfer, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

func assertConcurrentReplay(t *testing.T, results []concurrentTransferResult) *Transfer {
	t.Helper()
	var first *Transfer
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent replay request %d returned error: %v", i, result.err)
		}
		if result.transfer == nil {
			t.Fatalf("concurrent replay request %d returned nil transfer", i)
		}
		if first == nil {
			first = result.transfer
			continue
		}
		if result.transfer.ID != first.ID {
			t.Fatalf("concurrent replay request %d returned transfer %s, want %s", i, result.transfer.ID, first.ID)
		}
	}
	return first
}

func assertRepositoryBalance(t *testing.T, repo *SQLRepository, ctx context.Context, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := repo.GetBalance(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want available=%d ledger=%d", balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
	}
}

func assertSQLReplayIntegrity(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	if err := assertAllSQLJournalsBalanced(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertNoSQLDuplicateProviderEvents(t, ctx, db)
	assertNoSQLDuplicateIdempotencyKeys(t, ctx, db)
	t.Log("journal_mismatches=0 balance_mismatches=0 provider_event_duplicate_count=0 idempotency_duplicate_count=0")
}

func assertNoSQLDuplicateProviderEvents(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	var duplicates int
	if err := db.GetContext(ctx, &duplicates, `
SELECT COUNT(*)
FROM (
	SELECT institution_id, provider, provider_event_id
	FROM provider_events
	GROUP BY institution_id, provider, provider_event_id
	HAVING COUNT(*) > 1
) duplicate_provider_events`); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("provider_event duplicate count = %d, want 0", duplicates)
	}
}

func assertNoSQLDuplicateIdempotencyKeys(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	var duplicates int
	if err := db.GetContext(ctx, &duplicates, `
SELECT COUNT(*)
FROM (
	SELECT institution_id, idempotency_key
	FROM transfers
	GROUP BY institution_id, idempotency_key
	HAVING COUNT(*) > 1
) duplicate_idempotency_keys`); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("idempotency duplicate count = %d, want 0", duplicates)
	}
}

func assertSQLTransferCountByProviderEvent(t *testing.T, ctx context.Context, db *sqlx.DB, providerEventID string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND provider_event_id = $2`, DemoInstitutionID, providerEventID); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for provider_event_id %q = %d, want %d", providerEventID, count, want)
	}
}

func assertSQLTransferCountByIdempotency(t *testing.T, ctx context.Context, db *sqlx.DB, idempotencyKey string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND idempotency_key = $2`, DemoInstitutionID, idempotencyKey); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for idempotency_key %q = %d, want %d", idempotencyKey, count, want)
	}
}

func assertSQLTransferCountByIdempotencyPrefix(t *testing.T, ctx context.Context, db *sqlx.DB, prefix string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND idempotency_key LIKE $2`, DemoInstitutionID, prefix+"%"); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for idempotency_key prefix %q = %d, want %d", prefix, count, want)
	}
}

func assertSQLTransferCountByProviderReference(t *testing.T, ctx context.Context, db *sqlx.DB, providerReference, direction string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND provider_reference = $2 AND direction = $3`, DemoInstitutionID, providerReference, direction); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for provider_reference %q direction %q = %d, want %d", providerReference, direction, count, want)
	}
}

func assertSQLJournalCountByProviderReference(t *testing.T, ctx context.Context, db *sqlx.DB, providerReference, direction string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers t
JOIN journal_entries je ON je.institution_id = t.institution_id AND je.transfer_id = t.id
WHERE t.institution_id = $1 AND t.provider_reference = $2 AND t.direction = $3`, DemoInstitutionID, providerReference, direction); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("journal count for provider_reference %q direction %q = %d, want %d", providerReference, direction, count, want)
	}
}

func assertSQLJournalCountByTransfer(t *testing.T, ctx context.Context, db *sqlx.DB, transferID string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2`, DemoInstitutionID, transferID); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("journal count for transfer %q = %d, want %d", transferID, count, want)
	}
}

func assertSQLTransferHold(t *testing.T, ctx context.Context, db *sqlx.DB, transferID, wantStatus string) {
	t.Helper()
	var hold AccountHold
	if err := db.GetContext(ctx, &hold, `
SELECT id, institution_id, account_id, transfer_id, amount_minor, currency_id, status, reason, reference, created_at, updated_at, released_at
FROM account_holds
WHERE institution_id = $1 AND transfer_id = $2`, DemoInstitutionID, transferID); err != nil {
		t.Fatal(err)
	}
	if hold.Status != wantStatus {
		t.Fatalf("hold for transfer %s status=%s, want %s: %+v", transferID, hold.Status, wantStatus, hold)
	}
}

func resetIntegrationSchema(t *testing.T, db *sqlx.DB) {
	t.Helper()
	_, err := db.Exec(`
TRUNCATE TABLE
	audit_events,
	provider_events,
	account_holds,
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
	assertSQLBalancePair(t, svc, ctx, want, want)
}

func assertSQLBalancePair(t *testing.T, svc *Service, ctx context.Context, wantAvailable, wantLedger int64) {
	t.Helper()
	assertSQLAccountBalancePair(t, svc, ctx, DemoCustomerAccountID, wantAvailable, wantLedger)
}

func assertSQLAccountBalancePair(t *testing.T, svc *Service, ctx context.Context, accountID string, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch for account %s: got available=%d ledger=%d want available=%d ledger=%d", accountID, balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
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

func assertSQLInternalCreditRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, accountID, sourceAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal credit to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal credit transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal credit, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	source := postings[sourceAccountID]
	customer := postings[accountID]
	if source.direction != PostingDebit || source.amount != amountMinor || customer.direction != PostingCredit || customer.amount != amountMinor {
		t.Fatalf("internal credit postings should debit source and credit customer: %+v", rows)
	}
}

func assertSQLInternalDebitRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, accountID, destinationAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal debit to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal debit transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal debit, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	customer := postings[accountID]
	destination := postings[destinationAccountID]
	if customer.direction != PostingDebit || customer.amount != amountMinor || destination.direction != PostingCredit || destination.amount != amountMinor {
		t.Fatalf("internal debit postings should debit customer and credit destination: %+v", rows)
	}
}

func assertSQLInternalTransferRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, sourceAccountID, destinationAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal transfer to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal transfer, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	source := postings[sourceAccountID]
	destination := postings[destinationAccountID]
	if source.direction != PostingDebit || source.amount != amountMinor || destination.direction != PostingCredit || destination.amount != amountMinor {
		t.Fatalf("internal transfer postings should debit source and credit destination: %+v", rows)
	}
}

func assertSQLHistory(t *testing.T, history []Transaction, inboundID, outboundID, failedPendingOutboundID, succeededPendingOutboundID, pendingID, failedID, reversalID string) {
	t.Helper()
	if len(history) != 7 {
		t.Fatalf("expected seven transaction history rows, got %d: %+v", len(history), history)
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
		inboundID:                  {status: TransferStatusSucceeded, signedMinor: 500000, hasJournal: true},
		outboundID:                 {status: TransferStatusSucceeded, signedMinor: -125000, hasJournal: true},
		failedPendingOutboundID:    {status: TransferStatusFailed, signedMinor: 0, hasJournal: false},
		succeededPendingOutboundID: {status: TransferStatusSucceeded, signedMinor: -25000, hasJournal: true},
		pendingID:                  {status: TransferStatusPending, signedMinor: 0, hasJournal: false},
		failedID:                   {status: TransferStatusFailed, signedMinor: 0, hasJournal: false},
		reversalID:                 {status: TransferStatusSucceeded, signedMinor: -500000, hasJournal: true},
	}
	for transferID, want := range expect {
		got, ok := seen[transferID]
		if !ok {
			t.Fatalf("missing history row for transfer %s: %+v", transferID, history)
		}
		if got.Status != want.status || got.SignedAmountMinor != want.signedMinor {
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

func assertSQLHistoryRow(t *testing.T, history []Transaction, transferID, status string, signedMinor int64, hasJournal bool) {
	t.Helper()
	for _, row := range history {
		if row.TransferID != transferID {
			continue
		}
		if row.Status != status || row.SignedAmountMinor != signedMinor {
			t.Fatalf("history row mismatch for transfer %s: got %+v want status=%s signed=%d", transferID, row, status, signedMinor)
		}
		if hasJournal && row.JournalEntryID == nil {
			t.Fatalf("history row should have journal: %+v", row)
		}
		if !hasJournal && row.JournalEntryID != nil {
			t.Fatalf("history row should not have journal: %+v", row)
		}
		return
	}
	t.Fatalf("missing history row for transfer %s: %+v", transferID, history)
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
		a.institution_id,
		a.id AS account_id,
		COALESCE(SUM(
			CASE
				WHEN p.id IS NULL THEN 0
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
	GROUP BY a.institution_id, a.id
),
active_holds AS (
	SELECT
		institution_id,
		account_id,
		COALESCE(SUM(amount_minor), 0) AS held_minor
	FROM account_holds
	WHERE institution_id = $1 AND status = 'active'
	GROUP BY institution_id, account_id
)
SELECT COUNT(*)
FROM posting_balances pb
LEFT JOIN account_balances b
	ON b.institution_id = pb.institution_id
	AND b.account_id = pb.account_id
LEFT JOIN active_holds h
	ON h.institution_id = pb.institution_id
	AND h.account_id = pb.account_id
WHERE b.account_id IS NULL
	OR b.ledger_minor <> pb.computed_minor
	OR b.available_minor <> pb.computed_minor - COALESCE(h.held_minor, 0)`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("SQL account balances do not reconcile to postings and active holds")
	}
	return nil
}
