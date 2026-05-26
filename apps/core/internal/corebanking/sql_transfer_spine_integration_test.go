//go:build integration

package corebanking

import (
	"context"
	"errors"
	"testing"
)

func TestSQLRepositoryTransferSpineIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

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

func TestSQLExternalOutboundTransferLifecycleIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-external-outbound-fund",
	})

	successResult := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-external-outbound-success", 12000, MockProviderScenarioSuccess))
	successTransfer := mustGetSQLTransfer(t, svc, ctx, successResult.TransferID)
	if successResult.HoldID == nil || successTransfer.JournalEntryID == nil {
		t.Fatalf("successful external outbound should return hold and journal: result=%+v transfer=%+v", successResult, successTransfer)
	}
	assertStatus(t, successTransfer, TransferStatusSucceeded)
	assertSQLBalance(t, svc, ctx, 38000)
	assertSQLJournalBalanced(t, svc, ctx, successTransfer, 12000)
	assertSQLTransferHold(t, ctx, db, successTransfer.ID, HoldStatusConsumed)

	failedResult := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-external-outbound-failed", 5000, MockProviderScenarioFailed))
	failedTransfer := mustGetSQLTransfer(t, svc, ctx, failedResult.TransferID)
	if failedTransfer.JournalEntryID != nil || failedTransfer.LedgerStatus != LedgerStatusNoPosting || failedTransfer.ReconciliationStatus != ReconciliationStatusNoAction {
		t.Fatalf("failed external outbound should not post journal: %+v", failedTransfer)
	}
	assertSQLBalance(t, svc, ctx, 38000)
	assertSQLTransferHold(t, ctx, db, failedTransfer.ID, HoldStatusReleased)
	assertSQLJournalCountByTransfer(t, ctx, db, failedTransfer.ID, 0)

	pendingResult := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-external-outbound-pending", 7000, MockProviderScenarioPending))
	pendingTransfer := mustGetSQLTransfer(t, svc, ctx, pendingResult.TransferID)
	if pendingTransfer.JournalEntryID != nil || pendingTransfer.LedgerStatus != LedgerStatusPending || pendingTransfer.ReconciliationStatus != ReconciliationStatusPending {
		t.Fatalf("pending external outbound should keep hold without journal: %+v", pendingTransfer)
	}
	assertSQLBalancePair(t, svc, ctx, 31000, 38000)
	assertSQLTransferHold(t, ctx, db, pendingTransfer.ID, HoldStatusActive)
	assertSQLJournalCountByTransfer(t, ctx, db, pendingTransfer.ID, 0)

	unknownResult := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-external-outbound-unknown", 6000, MockProviderScenarioProviderUnknown))
	unknownTransfer := mustGetSQLTransfer(t, svc, ctx, unknownResult.TransferID)
	if unknownTransfer.ProviderStatus != TransferProviderStatusUnknown ||
		unknownTransfer.LedgerStatus != LedgerStatusPending ||
		unknownTransfer.ReconciliationStatus != ReconciliationStatusManualReview ||
		unknownTransfer.JournalEntryID != nil {
		t.Fatalf("provider_unknown external outbound mismatch: %+v", unknownTransfer)
	}
	assertSQLBalancePair(t, svc, ctx, 25000, 38000)
	assertSQLTransferHold(t, ctx, db, unknownTransfer.ID, HoldStatusActive)
	assertSQLJournalCountByTransfer(t, ctx, db, unknownTransfer.ID, 0)
	reconItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferProviderStatusUnknown})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, reconItems, unknownTransfer.ID, "provider_unknown", ReconciliationActionRequeryProvider)

	if _, err := svc.ExternalOutboundTransfer(ctx, externalOutboundTestInput("sql-external-outbound-overspend", 26000, MockProviderScenarioSuccess)); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected active holds to block overspend, got %v", err)
	}
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-external-outbound-overspend", 0)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLHistoryRow(t, history, successTransfer.ID, TransferStatusSucceeded, -12000, true)
	assertSQLHistoryRow(t, history, failedTransfer.ID, TransferStatusFailed, 0, false)
	assertSQLHistoryRow(t, history, pendingTransfer.ID, TransferStatusPending, 0, false)
	assertSQLHistoryRow(t, history, unknownTransfer.ID, TransferStatusPending, 0, false)

	if _, err := svc.ExternalOutboundTransfer(ctx, ExternalOutboundTransferInput{
		InstitutionID:              "99999999-9999-9999-9999-999999999999",
		SourceAccountID:            DemoCustomerAccountID,
		DestinationInstitutionCode: mockNIPDemoBankCode,
		DestinationAccountNumber:   mockNIPDemoAccountNumber,
		AmountMinor:                1000,
		CurrencyID:                 "NGN",
		IdempotencyKey:             "sql-external-outbound-cross-tenant",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant external outbound to fail, got %v", err)
	}

	if err := assertAllSQLJournalsBalanced(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestSQLExternalInboundEventLifecycleIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	successPayload := externalInboundPayload("sql-external-inbound-success-event", "sql-external-inbound-success-ref", TransferStatusSucceeded, 11000)
	success := externalInbound(t, svc, ctx, successPayload)
	if success.TransferID == nil || success.JournalEntryID == nil {
		t.Fatalf("successful inbound should return transfer and journal: %+v", success)
	}
	successTransfer := mustGetSQLTransfer(t, svc, ctx, *success.TransferID)
	assertStatus(t, successTransfer, TransferStatusSucceeded)
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalBalanced(t, svc, ctx, successTransfer, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, successTransfer.ID, 1)

	duplicate := externalInbound(t, svc, ctx, successPayload)
	if duplicate.TransferID == nil || *duplicate.TransferID != successTransfer.ID {
		t.Fatalf("duplicate inbound event should replay first transfer: first=%+v duplicate=%+v", success, duplicate)
	}
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, successTransfer.ID, 1)

	mismatchPayload := externalInboundPayload("sql-external-inbound-success-event", "sql-external-inbound-success-ref", TransferStatusSucceeded, 12000)
	mismatch := externalInbound(t, svc, ctx, mismatchPayload)
	if mismatch.HTTPStatus != 409 || mismatch.TransferID == nil || mismatch.JournalEntryID != nil || mismatch.LedgerStatus != LedgerStatusNoPosting || mismatch.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("mismatched inbound event should return manual review conflict: %+v", mismatch)
	}
	mismatchTransfer := mustGetSQLTransfer(t, svc, ctx, *mismatch.TransferID)
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, mismatchTransfer.ID, 0)
	reviewItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, reviewItems, mismatchTransfer.ID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)

	pending := externalInbound(t, svc, ctx, externalInboundPayload("sql-external-inbound-pending-event", "sql-external-inbound-pending-ref", TransferStatusPending, 7000))
	if pending.TransferID == nil || pending.JournalEntryID != nil || pending.LedgerStatus != LedgerStatusPending || pending.ReconciliationStatus != ReconciliationStatusPending {
		t.Fatalf("pending inbound should record without journal: %+v", pending)
	}
	pendingTransfer := mustGetSQLTransfer(t, svc, ctx, *pending.TransferID)
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, pendingTransfer.ID, 0)

	failed := externalInbound(t, svc, ctx, externalInboundPayload("sql-external-inbound-failed-event", "sql-external-inbound-failed-ref", TransferStatusFailed, 8000))
	if failed.TransferID == nil || failed.JournalEntryID != nil || failed.LedgerStatus != LedgerStatusNoPosting || failed.ReconciliationStatus != ReconciliationStatusNoAction {
		t.Fatalf("failed inbound should record without journal: %+v", failed)
	}
	failedTransfer := mustGetSQLTransfer(t, svc, ctx, *failed.TransferID)
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, failedTransfer.ID, 0)

	unknownPayload := externalInboundPayload("sql-external-inbound-unknown-event", "sql-external-inbound-unknown-ref", TransferStatusSucceeded, 9000)
	unknownPayload["destination_account_number"] = "0000000017"
	unknown := externalInbound(t, svc, ctx, unknownPayload)
	if unknown.TransferID == nil || unknown.JournalEntryID != nil || unknown.Message != "unknown_destination" || unknown.LedgerStatus != LedgerStatusNoPosting || unknown.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("unknown destination inbound should return review result: %+v", unknown)
	}
	unknownTransfer := mustGetSQLTransfer(t, svc, ctx, *unknown.TransferID)
	assertSQLBalance(t, svc, ctx, 11000)
	assertSQLJournalCountByTransfer(t, ctx, db, unknownTransfer.ID, 0)
	reviewItems, err = svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, reviewItems, unknownTransfer.ID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertSQLHistoryRow(t, history, successTransfer.ID, TransferStatusSucceeded, 11000, true)
	assertSQLHistoryRow(t, history, pendingTransfer.ID, TransferStatusPending, 0, false)
	assertSQLHistoryRow(t, history, failedTransfer.ID, TransferStatusFailed, 0, false)

	crossTenantPayload := externalInboundPayload("sql-external-inbound-cross-tenant-event", "sql-external-inbound-cross-tenant-ref", TransferStatusSucceeded, 6000)
	if _, err := svc.ExternalInboundEvent(ctx, externalInboundSQLInput(t, "99999999-9999-9999-9999-999999999999", crossTenantPayload)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant inbound event not to credit demo account, got %v", err)
	}
	assertSQLBalance(t, svc, ctx, 11000)

	if err := assertAllSQLJournalsBalanced(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestSQLExternalTransferRequeryIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	t.Run("outbound_success_consumes_hold_posts_once", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-requery-out-success-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-requery-out-success", 7000, MockProviderScenarioPending))
		assertSQLBalancePair(t, svc, ctx, 43000, 50000)
		assertSQLTransferHold(t, ctx, db, pending.TransferID, HoldStatusActive)

		result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID, Scenario: MockProviderScenarioSuccess})
		transfer := mustGetSQLTransfer(t, svc, ctx, result.TransferID)
		assertStatus(t, transfer, TransferStatusSucceeded)
		if transfer.ProviderStatus != TransferStatusSucceeded || transfer.LedgerStatus != LedgerStatusPosted || transfer.ReconciliationStatus != ReconciliationStatusMatched || transfer.JournalEntryID == nil {
			t.Fatalf("successful requery transfer mismatch: %+v", transfer)
		}
		assertSQLBalance(t, svc, ctx, 43000)
		assertSQLTransferHold(t, ctx, db, transfer.ID, HoldStatusConsumed)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 1)
		assertSQLJournalBalanced(t, svc, ctx, transfer, 7000)

		replay := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID, Scenario: MockProviderScenarioSuccess})
		if replay.TransferID != result.TransferID {
			t.Fatalf("requery replay returned different transfer: first=%+v replay=%+v", result, replay)
		}
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("outbound_failed_releases_hold_no_journal", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-requery-out-failed-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-requery-out-failed", 5000, MockProviderScenarioPending))

		result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID, Scenario: MockProviderScenarioFailed})
		transfer := mustGetSQLTransfer(t, svc, ctx, result.TransferID)
		if transfer.Status != TransferStatusFailed || transfer.ProviderStatus != TransferStatusFailed || transfer.LedgerStatus != LedgerStatusNoPosting || transfer.ReconciliationStatus != ReconciliationStatusNoAction || transfer.JournalEntryID != nil {
			t.Fatalf("failed requery transfer mismatch: %+v", transfer)
		}
		assertSQLBalance(t, svc, ctx, 50000)
		assertSQLTransferHold(t, ctx, db, transfer.ID, HoldStatusReleased)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 0)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("provider_unknown_unavailable_stays_visible", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-requery-out-unknown-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-requery-out-unknown", 6000, MockProviderScenarioProviderUnknown))

		result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID, Scenario: MockProviderScenarioTimeout})
		transfer := mustGetSQLTransfer(t, svc, ctx, result.TransferID)
		if transfer.Status != TransferStatusPending || transfer.ProviderStatus != TransferProviderStatusUnknown || transfer.LedgerStatus != LedgerStatusPending || transfer.ReconciliationStatus != ReconciliationStatusManualReview || transfer.JournalEntryID != nil {
			t.Fatalf("provider_unknown requery transfer mismatch: %+v", transfer)
		}
		assertSQLBalancePair(t, svc, ctx, 44000, 50000)
		assertSQLTransferHold(t, ctx, db, transfer.ID, HoldStatusActive)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 0)
		reconItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferProviderStatusUnknown})
		if err != nil {
			t.Fatal(err)
		}
		assertReconciliationItem(t, reconItems, transfer.ID, "provider_unknown", ReconciliationActionRequeryProvider)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("inbound_pending_success_credits_once", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		pending := externalInbound(t, svc, ctx, externalInboundPayload("sql-requery-in-event", "sql-requery-in-ref", TransferStatusPending, 7000))
		if pending.TransferID == nil {
			t.Fatalf("pending inbound missing transfer id: %+v", pending)
		}

		result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: *pending.TransferID, Scenario: MockProviderScenarioSuccess})
		transfer := mustGetSQLTransfer(t, svc, ctx, result.TransferID)
		assertStatus(t, transfer, TransferStatusSucceeded)
		if transfer.ProviderStatus != TransferStatusSucceeded || transfer.LedgerStatus != LedgerStatusPosted || transfer.ReconciliationStatus != ReconciliationStatusMatched || transfer.JournalEntryID == nil {
			t.Fatalf("inbound requery transfer mismatch: %+v", transfer)
		}
		assertSQLBalance(t, svc, ctx, 7000)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 1)
		assertSQLJournalBalanced(t, svc, ctx, transfer, 7000)

		externalRequery(t, svc, ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: *pending.TransferID, Scenario: MockProviderScenarioSuccess})
		assertSQLBalance(t, svc, ctx, 7000)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("concurrent_outbound_success_has_one_effect", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "sql-requery-concurrent-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("sql-requery-concurrent", 9000, MockProviderScenarioPending))

		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			result, err := svc.ExternalTransferRequery(ctx, ExternalTransferRequeryInput{
				InstitutionID: DemoInstitutionID,
				TransferID:    pending.TransferID,
				Scenario:      MockProviderScenarioSuccess,
			})
			if err != nil {
				return nil, err
			}
			return svc.GetTransfer(ctx, DemoInstitutionID, result.TransferID)
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, 41000)
		assertSQLTransferHold(t, ctx, db, transfer.ID, HoldStatusConsumed)
		assertSQLJournalCountByTransfer(t, ctx, db, transfer.ID, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})
}
