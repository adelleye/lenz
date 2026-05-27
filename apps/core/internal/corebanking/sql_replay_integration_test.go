//go:build integration

package corebanking

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestWithTxCommitsAndRollsBackMoneyMovementIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := newSQLRepository(db)

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
		_, _, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, commitInput)
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
		if _, _, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, rollbackInput); err != nil {
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
				ProviderEventID: "sql-concurrent-inbound-idempotency-event",
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

	t.Run("reversal_unique_per_source", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)

		original := mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     50000,
			IdempotencyKey:  "sql-concurrent-reversal-fund",
			ProviderEventID: "sql-concurrent-reversal-fund-event",
			Narration:       "SQL concurrent reversal funding",
		})

		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.ReverseTransfer(ctx, DemoInstitutionID, original.ID, fmt.Sprintf("sql-concurrent-reversal-%02d", i))
		})

		var winners []*Transfer
		conflicts := 0
		for i, result := range results {
			if result.err == nil {
				winners = append(winners, result.transfer)
				continue
			}
			if errors.Is(result.err, ErrConflict) {
				conflicts++
				continue
			}
			t.Fatalf("concurrent reversal request %d returned unexpected error: %v", i, result.err)
		}
		if len(winners) != 1 {
			t.Fatalf("expected exactly one reversal to succeed, got %d", len(winners))
		}
		if conflicts != 9 {
			t.Fatalf("expected nine reversal attempts to conflict, got %d", conflicts)
		}
		assertSQLBalance(t, svc, ctx, 0)
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
