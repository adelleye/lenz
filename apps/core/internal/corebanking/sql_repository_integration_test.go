//go:build integration

package corebanking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func TestSQLRepositoryTransferSpineIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewSQLRepository(db), NewMockNIPProvider())

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

	pendingOutboundToFail := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       50000,
		IdempotencyKey:    "sql-out-pending-fail-001",
		ProviderReference: "sql-out-pending-fail-ref-001",
		Status:            TransferStatusPending,
		Narration:         "SQL pending outbound fail proof",
	})
	assertStatus(t, pendingOutboundToFail, TransferStatusPending)
	assertSQLBalancePair(t, svc, ctx, 325000, 375000)

	failedPendingOutbound := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusFailed,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "sql-out-pending-fail-settle-001",
		ProviderReference: "sql-out-pending-fail-ref-001",
		ProviderEventID:   "sql-provider-event-out-pending-fail-001",
		FailureReason:     "provider_failed",
		Narration:         "SQL pending outbound failed",
	})
	if failedPendingOutbound.ID != pendingOutboundToFail.ID {
		t.Fatalf("failed settlement should update the pending transfer: pending=%s failed=%s", pendingOutboundToFail.ID, failedPendingOutbound.ID)
	}
	assertStatus(t, failedPendingOutbound, TransferStatusFailed)
	assertSQLBalance(t, svc, ctx, 375000)

	pendingOutboundToSucceed := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       25000,
		IdempotencyKey:    "sql-out-pending-success-001",
		ProviderReference: "sql-out-pending-success-ref-001",
		Status:            TransferStatusPending,
		Narration:         "SQL pending outbound success proof",
	})
	assertStatus(t, pendingOutboundToSucceed, TransferStatusPending)
	assertSQLBalancePair(t, svc, ctx, 350000, 375000)

	succeededPendingOutbound := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       25000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "sql-out-pending-success-settle-001",
		ProviderReference: "sql-out-pending-success-ref-001",
		ProviderEventID:   "sql-provider-event-out-pending-success-001",
		Narration:         "SQL pending outbound succeeded",
	})
	if succeededPendingOutbound.ID != pendingOutboundToSucceed.ID {
		t.Fatalf("successful settlement should update the pending transfer: pending=%s succeeded=%s", pendingOutboundToSucceed.ID, succeededPendingOutbound.ID)
	}
	assertStatus(t, succeededPendingOutbound, TransferStatusSucceeded)
	assertSQLBalance(t, svc, ctx, 350000)
	assertSQLJournalBalanced(t, svc, ctx, succeededPendingOutbound, 25000)

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
	assertSQLBalance(t, svc, ctx, 350000)

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
	assertSQLBalance(t, svc, ctx, 350000)

	reversal := reverseTransfer(t, svc, ctx, inbound.ID, "sql-reversal-001")
	assertStatus(t, reversal, TransferStatusSucceeded)
	if reversal.Direction != TransferDirectionReversal || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != inbound.ID {
		t.Fatalf("reversal did not reference the original transfer: %+v", reversal)
	}
	if reversal.LedgerStatus != LedgerStatusReversalDeficit || reversal.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("deficit reversal should be marked for manual review: %+v", reversal)
	}
	assertSQLBalance(t, svc, ctx, -150000)
	assertSQLJournalBalanced(t, svc, ctx, reversal, 500000)

	_, err = svc.ReverseTransfer(ctx, DemoInstitutionID, inbound.ID, "sql-in-001")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected unrelated idempotency key collision to fail, got %v", err)
	}

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLHistory(t, history, inbound.ID, outbound.ID, pendingOutboundToFail.ID, pendingOutboundToSucceed.ID, pending.ID, failed.ID, reversal.ID)

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

func TestWithTxCommitsAndRollsBackMoneyMovementIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := NewSQLRepository(db)

	if _, err := repo.EnsureDemoData(ctx); err != nil {
		t.Fatal(err)
	}

	commitInput := RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       17000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "withtx-commit",
		Provider:          ProviderMockNIP,
		ProviderReference: "withtx-commit-ref",
		ProviderEventID:   "withtx-commit-event",
		Narration:         "WithTx commit proof",
	}
	if err := WithTx(ctx, db, func(tx TxRunner) error {
		_, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, commitInput)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertRepositoryBalance(t, repo, ctx, 17000, 17000)

	rollbackInput := RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       9000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "withtx-rollback",
		Provider:          ProviderMockNIP,
		ProviderReference: "withtx-rollback-ref",
		ProviderEventID:   "withtx-rollback-event",
		Narration:         "WithTx rollback proof",
	}
	forcedRollback := errors.New("force rollback after posting")
	err := WithTx(ctx, db, func(tx TxRunner) error {
		if _, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, rollbackInput); err != nil {
			return err
		}
		return forcedRollback
	})
	if !errors.Is(err, forcedRollback) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}
	assertRepositoryBalance(t, repo, ctx, 17000, 17000)

	var rollbackRows int
	if err := db.GetContext(ctx, &rollbackRows, `SELECT COUNT(*) FROM transfers WHERE institution_id = $1 AND idempotency_key = $2`, DemoInstitutionID, rollbackInput.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if rollbackRows != 0 {
		t.Fatalf("rollback transfer should not be committed, found %d rows", rollbackRows)
	}
}

func TestSQLRepositoryTransferSpineIntegrationConcurrentReplay(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	t.Run("provider_event_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)

		const eventID = "sql-concurrent-provider-event"
		const amount = int64(3333)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockInbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    fmt.Sprintf("sql-concurrent-provider-event-%02d", i),
				ProviderEventID:   eventID,
				ProviderReference: "sql-concurrent-provider-ref",
				Narration:         "SQL concurrent provider event replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByProviderEvent(t, ctx, db, eventID, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("inbound_idempotency_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)

		const idempotencyKey = "sql-concurrent-inbound-idempotency"
		const amount = int64(2222)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockInbound(ctx, TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     amount,
				IdempotencyKey:  idempotencyKey,
				ProviderEventID: fmt.Sprintf("sql-concurrent-inbound-idempotency-event-%02d", i),
				Narration:       "SQL concurrent inbound idempotency replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("outbound_idempotency_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     50000,
			IdempotencyKey:  "sql-concurrent-outbound-funding",
			ProviderEventID: "sql-concurrent-outbound-funding-event",
			Narration:       "SQL concurrent outbound funding",
		})

		const idempotencyKey = "sql-concurrent-outbound-idempotency"
		const amount = int64(12000)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockOutbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    idempotencyKey,
				ProviderReference: "sql-concurrent-outbound-idempotency-ref",
				Narration:         "SQL concurrent outbound idempotency replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, 50000-amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("pending_settlement_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     50000,
			IdempotencyKey:  "sql-concurrent-settlement-funding",
			ProviderEventID: "sql-concurrent-settlement-funding-event",
			Narration:       "SQL concurrent settlement funding",
		})

		const providerReference = "sql-concurrent-settlement-ref"
		const amount = int64(7000)
		pending := mockOutbound(t, svc, ctx, TransferRequest{
			AccountID:         DemoCustomerAccountID,
			AmountMinor:       amount,
			IdempotencyKey:    "sql-concurrent-settlement-pending",
			ProviderReference: providerReference,
			Status:            TransferStatusPending,
			Narration:         "SQL concurrent settlement pending outbound",
		})
		assertStatus(t, pending, TransferStatusPending)
		assertSQLBalancePair(t, svc, ctx, 50000-amount, 50000)

		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockOutbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    fmt.Sprintf("sql-concurrent-settlement-%02d", i),
				ProviderReference: providerReference,
				ProviderEventID:   fmt.Sprintf("sql-concurrent-settlement-event-%02d", i),
				Status:            TransferStatusSucceeded,
				Narration:         "SQL concurrent pending settlement replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		if transfer.ID != pending.ID {
			t.Fatalf("settlement replay returned different transfer: pending=%s got=%s", pending.ID, transfer.ID)
		}
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, 50000-amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByProviderReference(t, ctx, db, providerReference, TransferDirectionOutbound, 1)
		assertSQLJournalCountByProviderReference(t, ctx, db, providerReference, TransferDirectionOutbound, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})
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

type concurrentTransferResult struct {
	transfer *Transfer
	err      error
}

func seededSQLService(t *testing.T, db *sqlx.DB, ctx context.Context) *Service {
	t.Helper()
	svc := NewService(NewSQLRepository(db), NewMockNIPProvider())
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return svc
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
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want available=%d ledger=%d", balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
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
),
active_holds AS (
	SELECT
		account_id,
		COALESCE(SUM(amount_minor), 0) AS held_minor
	FROM account_holds
	WHERE institution_id = $1 AND status = 'active'
	GROUP BY account_id
)
SELECT COUNT(*)
FROM account_balances b
JOIN posting_balances pb ON pb.account_id = b.account_id
LEFT JOIN active_holds h ON h.account_id = b.account_id
WHERE b.institution_id = $1
	AND (b.ledger_minor <> pb.computed_minor OR b.available_minor <> pb.computed_minor - COALESCE(h.held_minor, 0))`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("SQL account balances do not reconcile to postings and active holds")
	}
	return nil
}
