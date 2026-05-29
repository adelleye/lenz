package corebanking

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestMockOutboundCallsProviderAdapter(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "provider-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "provider outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "provider-test-fund",
		Provider:          "test_setup",
		ProviderReference: "provider-test-fund-ref",
		ProviderEventID:   "provider-test-fund-event",
		Narration:         "provider adapter test funding",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "provider-adapter-out",
		Narration:      "outbound through provider",
	})
	if err != nil {
		t.Fatal(err)
	}

	if provider.initiateCalls != 1 {
		t.Fatalf("expected one provider InitiateTransfer call, got %d", provider.initiateCalls)
	}
	if provider.lastInitiate.AmountMinor != 10000 || provider.lastInitiate.AccountID != DemoCustomerAccountID {
		t.Fatalf("provider received wrong request: %+v", provider.lastInitiate)
	}
	if transfer.ProviderReference != "provider-out-ref" || transfer.Narration != "provider outbound" {
		t.Fatalf("transfer did not use provider result: %+v", transfer)
	}
}

func TestMockOutboundIdempotencyPreventsDuplicateProviderInitiation(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "idem-provider-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "provider outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "idem-provider-fund",
		Provider:          "test_setup",
		ProviderReference: "idem-provider-fund-ref",
		ProviderEventID:   "idem-provider-fund-event",
		Narration:         "fund test account",
	}); err != nil {
		t.Fatal(err)
	}

	req := TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "provider-adapter-idem-out",
		Narration:      "outbound",
	}
	first, err := svc.MockOutbound(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.MockOutbound(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected duplicate idempotency request to return original transfer")
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected one provider InitiateTransfer call, got %d", provider.initiateCalls)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000)
}

func TestMockOutboundExplicitFinalStatusSettlesPendingWithoutProviderInitiation(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "scripted-settlement-ref",
			Status:            TransferStatusPending,
			Narration:         "scripted pending outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "scripted-settlement-fund",
		Provider:          "test_setup",
		ProviderReference: "scripted-settlement-fund-ref",
		ProviderEventID:   "scripted-settlement-fund-event",
		Narration:         "fund scripted settlement account",
	}); err != nil {
		t.Fatal(err)
	}

	pending, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "scripted-settlement-pending",
		ProviderReference: "scripted-settlement-ref",
		Status:            TransferStatusPending,
		Narration:         "scripted pending outbound",
	})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "scripted-settlement-success",
		ProviderReference: "scripted-settlement-ref",
		ProviderEventID:   "scripted-settlement-event",
		Status:            TransferStatusSucceeded,
		Narration:         "scripted settlement success",
	})
	if err != nil {
		t.Fatal(err)
	}

	if settled.ID != pending.ID {
		t.Fatalf("scripted settlement should update pending transfer: pending=%s settled=%s", pending.ID, settled.ID)
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected only initial pending provider initiation, got %d calls", provider.initiateCalls)
	}
	assertStatus(t, settled, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000)
	assertMemoryTransferHold(t, store, pending.ID, HoldStatusConsumed)
	assertJournalBalanced(t, store, settled)
}

func TestMockOutboundIdempotencyRejectsChangedRequest(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "idem-provider-conflict-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "provider outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "idem-provider-conflict-fund",
		Provider:          "test_setup",
		ProviderReference: "idem-provider-conflict-fund-ref",
		ProviderEventID:   "idem-provider-conflict-fund-event",
		Narration:         "fund test account",
	}); err != nil {
		t.Fatal(err)
	}

	req := TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "provider-adapter-idem-conflict",
		ProviderReference: "idem-provider-conflict-ref",
	}
	if _, err := svc.MockOutbound(ctx, req); err != nil {
		t.Fatal(err)
	}
	req.AmountMinor = 15000
	if _, err := svc.MockOutbound(ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed mock outbound request to return conflict, got %v", err)
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected conflict before second provider call, got %d calls", provider.initiateCalls)
	}
}

func TestMockOutboundRecordsProviderUnknownOnTimeout(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{initiateErr: context.DeadlineExceeded}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "unknown-provider-fund",
		Provider:          "test_setup",
		ProviderReference: "unknown-provider-fund-ref",
		ProviderEventID:   "unknown-provider-fund-event",
		Narration:         "fund test account",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "unknown-provider-out",
		ProviderReference: "unknown-provider-out-ref",
		Narration:         "outbound",
	})
	if err != nil {
		t.Fatal(err)
	}

	if transfer.Status != TransferStatusPending || transfer.ProviderStatus != TransferProviderStatusUnknown || transfer.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("provider unknown transfer mismatch: %+v", transfer)
	}
	if transfer.JournalEntryID != nil {
		t.Fatalf("provider unknown transfer should not post a journal: %+v", transfer)
	}
	if hold := store.holds[transfer.ID]; hold.Status != HoldStatusActive || hold.AmountMinor != 10000 {
		t.Fatalf("provider unknown outbound should keep an active hold: %+v", hold)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000, 50000)
}

func TestExternalOutboundSuccessPostsJournalAndConsumesHold(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "external-out-success-fund",
	})

	result := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-out-success", 12000, MockProviderScenarioSuccess))

	if result.Status != TransferStatusSucceeded ||
		result.ProviderStatus != TransferStatusSucceeded ||
		result.LedgerStatus != LedgerStatusPosted ||
		result.ReconciliationStatus != ReconciliationStatusMatched ||
		result.JournalEntryID == nil ||
		result.HoldID == nil {
		t.Fatalf("external outbound success mismatch: %+v", result)
	}
	transfer, err := svc.GetTransfer(ctx, DemoInstitutionID, result.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	assertJournalBalanced(t, store, transfer)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 38000)
	assertMemoryTransferHold(t, store, result.TransferID, HoldStatusConsumed)
}

func TestExternalOutboundCreatedSourceAccountSucceeds(t *testing.T) {
	ctx, svc, store := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "External", "Source", "external.source@example.com", uniqueAccountNumber("73"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "external-created-source-fund",
	})
	input := externalOutboundTestInput("external-created-source", 12000, MockProviderScenarioSuccess)
	input.SourceAccountID = account.ID

	result := externalOutbound(t, svc, ctx, input)

	if result.SourceAccountID != account.ID || result.Status != TransferStatusSucceeded || result.JournalEntryID == nil || result.HoldID == nil {
		t.Fatalf("created source external outbound mismatch: %+v", result)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, account.ID, 38000)
	assertMemoryTransferHold(t, store, result.TransferID, HoldStatusConsumed)
}

func TestExternalOutboundFailurePendingAndUnknownLifecycle(t *testing.T) {
	tests := []struct {
		name               string
		scenario           string
		wantStatus         string
		wantProviderStatus string
		wantLedgerStatus   string
		wantReconStatus    string
		wantAvailable      int64
		wantLedger         int64
		wantHoldStatus     string
		wantReviewQueue    bool
	}{
		{name: "failed", scenario: MockProviderScenarioFailed, wantStatus: TransferStatusFailed, wantProviderStatus: TransferStatusFailed, wantLedgerStatus: LedgerStatusNoPosting, wantReconStatus: ReconciliationStatusNoAction, wantAvailable: 50000, wantLedger: 50000, wantHoldStatus: HoldStatusReleased},
		{name: "pending", scenario: MockProviderScenarioPending, wantStatus: TransferStatusPending, wantProviderStatus: TransferStatusPending, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusPending, wantAvailable: 40000, wantLedger: 50000, wantHoldStatus: HoldStatusActive},
		{name: "provider_unknown", scenario: MockProviderScenarioProviderUnknown, wantStatus: TransferStatusPending, wantProviderStatus: TransferProviderStatusUnknown, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusManualReview, wantAvailable: 40000, wantLedger: 50000, wantHoldStatus: HoldStatusActive, wantReviewQueue: true},
		{name: "timeout", scenario: MockProviderScenarioTimeout, wantStatus: TransferStatusPending, wantProviderStatus: TransferProviderStatusUnknown, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusManualReview, wantAvailable: 40000, wantLedger: 50000, wantHoldStatus: HoldStatusActive, wantReviewQueue: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			mustInternalCredit(t, svc, ctx, InternalCreditInput{
				InstitutionID:  DemoInstitutionID,
				AccountID:      DemoCustomerAccountID,
				AmountMinor:    50000,
				CurrencyID:     "NGN",
				IdempotencyKey: "external-out-" + tt.name + "-fund",
			})
			journalCountBefore := len(store.journals)

			result := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-out-"+tt.name, 10000, tt.scenario))

			if result.Status != tt.wantStatus ||
				result.ProviderStatus != tt.wantProviderStatus ||
				result.LedgerStatus != tt.wantLedgerStatus ||
				result.ReconciliationStatus != tt.wantReconStatus ||
				result.JournalEntryID != nil ||
				result.HoldID == nil {
				t.Fatalf("external outbound %s mismatch: %+v", tt.name, result)
			}
			if len(store.journals) != journalCountBefore {
				t.Fatalf("%s created a fake journal: before=%d after=%d", tt.name, journalCountBefore, len(store.journals))
			}
			assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, tt.wantAvailable, tt.wantLedger)
			assertMemoryTransferHold(t, store, result.TransferID, tt.wantHoldStatus)
			items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: tt.wantProviderStatus})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantReviewQueue {
				assertReconciliationItem(t, items, result.TransferID, "provider_unknown", ReconciliationActionRequeryProvider)
			} else {
				assertNoReconciliationItem(t, items, result.TransferID)
			}
		})
	}
}

func TestExternalTransferRequeryOutboundOutcomes(t *testing.T) {
	tests := []struct {
		name               string
		initialScenario    string
		requeryScenario    string
		wantStatus         string
		wantProviderStatus string
		wantLedgerStatus   string
		wantReconStatus    string
		wantHoldStatus     string
		wantAvailable      int64
		wantLedger         int64
		wantJournalDelta   int
		wantMessage        string
		wantReviewQueue    bool
	}{
		{name: "pending_to_success", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioSuccess, wantStatus: TransferStatusSucceeded, wantProviderStatus: TransferStatusSucceeded, wantLedgerStatus: LedgerStatusPosted, wantReconStatus: ReconciliationStatusMatched, wantHoldStatus: HoldStatusConsumed, wantAvailable: 40000, wantLedger: 40000, wantJournalDelta: 1, wantMessage: "requery_succeeded"},
		{name: "pending_to_failed", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioFailed, wantStatus: TransferStatusFailed, wantProviderStatus: TransferStatusFailed, wantLedgerStatus: LedgerStatusNoPosting, wantReconStatus: ReconciliationStatusNoAction, wantHoldStatus: HoldStatusReleased, wantAvailable: 50000, wantLedger: 50000, wantMessage: "requery_failed"},
		{name: "pending_still_pending", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioPending, wantStatus: TransferStatusPending, wantProviderStatus: TransferStatusPending, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusPending, wantHoldStatus: HoldStatusActive, wantAvailable: 40000, wantLedger: 50000, wantMessage: "requery_pending"},
		{name: "provider_unknown_to_success", initialScenario: MockProviderScenarioProviderUnknown, requeryScenario: MockProviderScenarioSuccess, wantStatus: TransferStatusSucceeded, wantProviderStatus: TransferStatusSucceeded, wantLedgerStatus: LedgerStatusPosted, wantReconStatus: ReconciliationStatusMatched, wantHoldStatus: HoldStatusConsumed, wantAvailable: 40000, wantLedger: 40000, wantJournalDelta: 1, wantMessage: "requery_succeeded"},
		{name: "provider_unknown_to_no_response", initialScenario: MockProviderScenarioProviderUnknown, requeryScenario: MockProviderScenarioTimeout, wantStatus: TransferStatusPending, wantProviderStatus: TransferProviderStatusUnknown, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusManualReview, wantHoldStatus: HoldStatusActive, wantAvailable: 40000, wantLedger: 50000, wantMessage: "requery_provider_unknown", wantReviewQueue: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			mustInternalCredit(t, svc, ctx, InternalCreditInput{
				InstitutionID:  DemoInstitutionID,
				AccountID:      DemoCustomerAccountID,
				AmountMinor:    50000,
				CurrencyID:     "NGN",
				IdempotencyKey: "external-requery-" + tt.name + "-fund",
			})
			pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-requery-"+tt.name, 10000, tt.initialScenario))
			journalCountBefore := len(store.journals)

			result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{
				InstitutionID: DemoInstitutionID,
				TransferID:    pending.TransferID,
				Scenario:      tt.requeryScenario,
				Note:          "operator requery",
			})

			if result.Status != tt.wantStatus ||
				result.ProviderStatus != tt.wantProviderStatus ||
				result.LedgerStatus != tt.wantLedgerStatus ||
				result.ReconciliationStatus != tt.wantReconStatus ||
				result.Message != tt.wantMessage {
				t.Fatalf("requery %s mismatch: %+v", tt.name, result)
			}
			if len(store.journals) != journalCountBefore+tt.wantJournalDelta {
				t.Fatalf("journal count mismatch: before=%d after=%d want_delta=%d", journalCountBefore, len(store.journals), tt.wantJournalDelta)
			}
			assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, tt.wantAvailable, tt.wantLedger)
			assertMemoryTransferHold(t, store, pending.TransferID, tt.wantHoldStatus)
			if tt.wantJournalDelta == 1 {
				transfer, err := svc.GetTransfer(ctx, DemoInstitutionID, pending.TransferID)
				if err != nil {
					t.Fatal(err)
				}
				assertJournalBalanced(t, store, transfer)
				replay := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{
					InstitutionID: DemoInstitutionID,
					TransferID:    pending.TransferID,
					Scenario:      tt.requeryScenario,
				})
				if replay.TransferID != result.TransferID || len(store.journals) != journalCountBefore+1 {
					t.Fatalf("final requery was not deterministic: replay=%+v journal_count=%d", replay, len(store.journals))
				}
			}
			items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: tt.wantProviderStatus})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantReviewQueue {
				assertReconciliationItem(t, items, pending.TransferID, "provider_unknown", ReconciliationActionRequeryProvider)
			} else {
				assertNoReconciliationItem(t, items, pending.TransferID)
			}
		})
	}
}

func TestExternalTransferRequeryInboundPendingSuccessCreditsOnce(t *testing.T) {
	ctx, svc, store := newTestService(t)
	pending := externalInbound(t, svc, ctx, externalInboundPayload("external-requery-in-event", "external-requery-in-ref", TransferStatusPending, 12000))
	if pending.TransferID == nil {
		t.Fatalf("pending inbound missing transfer id: %+v", pending)
	}
	journalCountBefore := len(store.journals)

	result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{
		InstitutionID: DemoInstitutionID,
		TransferID:    *pending.TransferID,
		Scenario:      MockProviderScenarioSuccess,
	})

	if result.Status != TransferStatusSucceeded ||
		result.ProviderStatus != TransferStatusSucceeded ||
		result.LedgerStatus != LedgerStatusPosted ||
		result.ReconciliationStatus != ReconciliationStatusMatched ||
		result.JournalEntryID == nil ||
		result.Message != "requery_succeeded" {
		t.Fatalf("inbound requery success mismatch: %+v", result)
	}
	if len(store.journals) != journalCountBefore+1 {
		t.Fatalf("inbound requery journal count mismatch: before=%d after=%d", journalCountBefore, len(store.journals))
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 12000)
	replay := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{
		InstitutionID: DemoInstitutionID,
		TransferID:    *pending.TransferID,
		Scenario:      MockProviderScenarioSuccess,
	})
	if replay.TransferID != result.TransferID || len(store.journals) != journalCountBefore+1 {
		t.Fatalf("inbound final requery was not deterministic: replay=%+v journal_count=%d", replay, len(store.journals))
	}
}

func TestExternalTransferRequeryNoOpsAndRejectsUnsafeTransfers(t *testing.T) {
	t.Run("already final", func(t *testing.T) {
		ctx, svc, store := newTestService(t)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "external-requery-final-fund",
		})
		final := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-requery-final", 10000, MockProviderScenarioSuccess))
		journalCountBefore := len(store.journals)

		result := externalRequery(t, svc, ctx, ExternalTransferRequeryInput{
			InstitutionID: DemoInstitutionID,
			TransferID:    final.TransferID,
			Scenario:      MockProviderScenarioFailed,
		})

		if result.Message != "already_final" || result.Status != TransferStatusSucceeded || len(store.journals) != journalCountBefore {
			t.Fatalf("already-final requery mismatch: result=%+v journals=%d", result, len(store.journals))
		}
	})

	t.Run("internal transfer rejected", func(t *testing.T) {
		ctx, svc, _ := newTestService(t)
		internal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    10000,
			CurrencyID:     "NGN",
			IdempotencyKey: "external-requery-internal",
		})
		if _, err := svc.ExternalTransferRequery(ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: internal.ID}); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("internal transfer requery error = %v, want invalid request", err)
		}
	})

	t.Run("unsupported provider rejected", func(t *testing.T) {
		ctx, svc, store := newTestService(t)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "external-requery-unsupported-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-requery-unsupported", 10000, MockProviderScenarioPending))
		store.mu.Lock()
		transfer := store.transfers[pending.TransferID]
		transfer.Provider = "real_nibss"
		store.transfers[pending.TransferID] = transfer
		store.mu.Unlock()

		if _, err := svc.ExternalTransferRequery(ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID}); !errors.Is(err, ErrUnsupportedProvider) {
			t.Fatalf("unsupported provider requery error = %v, want unsupported provider", err)
		}
	})

	t.Run("provider reference mismatch rejected", func(t *testing.T) {
		ctx, svc, _ := newTestService(t)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "external-requery-mismatch-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-requery-mismatch", 10000, MockProviderScenarioPending))

		if _, err := svc.ExternalTransferRequery(ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID, ProviderReference: "wrong-reference"}); !errors.Is(err, ErrConflict) {
			t.Fatalf("provider reference mismatch error = %v, want conflict", err)
		}
	})

	t.Run("provider status mismatch rejected", func(t *testing.T) {
		ctx := context.Background()
		store := newMemoryStore()
		provider := &spyTransferProvider{
			initiateResult: ProviderTransferResult{
				Provider:          ProviderMockNIP,
				ProviderReference: "external-requery-provider-mismatch-ref",
				Status:            TransferStatusPending,
				ProviderStatus:    TransferStatusPending,
			},
			requeryResult: ProviderTransferResult{
				Provider:          ProviderMockNIP,
				ProviderReference: "external-requery-provider-mismatch-ref",
				Status:            TransferStatusPending,
				ProviderStatus:    TransferStatusSucceeded,
			},
		}
		svc := NewService(store, provider)
		if _, err := svc.SeedDemo(ctx); err != nil {
			t.Fatal(err)
		}
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "external-requery-provider-mismatch-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("external-requery-provider-mismatch", 10000, MockProviderScenarioPending))

		if _, err := svc.ExternalTransferRequery(ctx, ExternalTransferRequeryInput{InstitutionID: DemoInstitutionID, TransferID: pending.TransferID}); !errors.Is(err, ErrConflict) {
			t.Fatalf("provider status mismatch error = %v, want conflict", err)
		}
	})
}

func TestExternalOutboundRejectsBeforeProviderCall(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*memoryStore)
		input  ExternalOutboundTransferInput
		want   error
		funded bool
	}{
		{name: "insufficient funds", input: externalOutboundTestInput("external-out-insufficient", 60000, MockProviderScenarioSuccess), want: ErrInsufficient, funded: true},
		{name: "frozen source", setup: func(store *memoryStore) { setMemoryAccountStatus(store, DemoCustomerAccountID, AccountStatusFrozen) }, input: externalOutboundTestInput("external-out-frozen", 10000, MockProviderScenarioSuccess), want: ErrInvalidRequest, funded: true},
		{name: "pnd source", setup: func(store *memoryStore) {
			setMemoryAccountStatus(store, DemoCustomerAccountID, AccountStatusPostNoDebit)
		}, input: externalOutboundTestInput("external-out-pnd", 10000, MockProviderScenarioSuccess), want: ErrInvalidRequest, funded: true},
		{name: "unsupported provider", input: func() ExternalOutboundTransferInput {
			input := externalOutboundTestInput("external-out-real-provider", 10000, MockProviderScenarioSuccess)
			input.Provider = "real_nibss"
			return input
		}(), want: ErrUnsupportedProvider, funded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newMemoryStore()
			provider := &spyTransferProvider{initiateResult: ProviderTransferResult{Provider: ProviderMockNIP, ProviderReference: "should-not-run", Status: TransferStatusSucceeded}}
			svc := NewService(store, provider)
			if _, err := svc.SeedDemo(ctx); err != nil {
				t.Fatal(err)
			}
			if tt.funded {
				mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 50000, CurrencyID: "NGN", IdempotencyKey: "fund-" + tt.name})
			}
			if tt.setup != nil {
				tt.setup(store)
			}

			_, err := svc.ExternalOutboundTransfer(ctx, tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if provider.initiateCalls != 0 {
				t.Fatalf("provider should not be called before validation failure, got %d calls", provider.initiateCalls)
			}
			if _, ok := store.idempotency[DemoInstitutionID+"|"+tt.input.IdempotencyKey]; ok {
				t.Fatalf("validation failure created transfer state for %s", tt.input.IdempotencyKey)
			}
		})
	}
}

func TestExternalOutboundIdempotencyReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "external-idem-provider-ref",
			Status:            TransferStatusSucceeded,
			ProviderStatus:    TransferStatusSucceeded,
			Narration:         "provider accepted once",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "external-idem-fund",
	})
	input := externalOutboundTestInput("external-idem", 10000, MockProviderScenarioSuccess)

	first := externalOutbound(t, svc, ctx, input)
	second := externalOutbound(t, svc, ctx, input)

	if first.TransferID != second.TransferID {
		t.Fatalf("same request replay returned different transfer: first=%s second=%s", first.TransferID, second.TransferID)
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("same idempotency replay should call provider once, got %d", provider.initiateCalls)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000)

	changed := input
	changed.AmountMinor = 15000
	if _, err := svc.ExternalOutboundTransfer(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected changed idempotency request conflict, got %v", err)
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("changed idempotency request should not call provider again, got %d", provider.initiateCalls)
	}
}

func TestExternalInboundEventSuccessReplayHistoryAndAudit(t *testing.T) {
	ctx, svc, store := newTestService(t)
	payload := externalInboundPayload("external-in-success-event", "external-in-success-ref", TransferStatusSucceeded, 15000)
	journalCountBefore := len(store.journals)

	first := externalInbound(t, svc, ctx, payload)
	second := externalInbound(t, svc, ctx, payload)

	if first.TransferID == nil || second.TransferID == nil || *first.TransferID != *second.TransferID {
		t.Fatalf("duplicate event did not replay first transfer: first=%+v second=%+v", first, second)
	}
	if first.Status != TransferStatusSucceeded ||
		first.ProviderStatus != TransferStatusSucceeded ||
		first.LedgerStatus != LedgerStatusPosted ||
		first.ReconciliationStatus != ReconciliationStatusMatched ||
		first.JournalEntryID == nil {
		t.Fatalf("external inbound success mismatch: %+v", first)
	}
	if len(store.journals) != journalCountBefore+1 {
		t.Fatalf("success replay created unexpected journals: before=%d after=%d", journalCountBefore, len(store.journals))
	}
	transfer, err := svc.GetTransfer(ctx, DemoInstitutionID, *first.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	assertJournalBalanced(t, store, transfer)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 15000)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertTransactionPresent(t, history, *first.TransferID, 15000, TransactionDirectionCredit)

	events, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if countAuditEvents(events, AuditActionExternalInboundSucceeded, func(event AuditEvent) bool {
		return auditString(event.TransferID) == *first.TransferID
	}) != 1 {
		t.Fatalf("expected one external inbound success audit event, got %+v", events)
	}
}

func TestExternalInboundEventMismatchCreatesManualReviewWithoutPosting(t *testing.T) {
	ctx, svc, store := newTestService(t)
	other := createMemoryCustomerAccount(t, svc, ctx, "Inbound", "Other", "inbound.other@example.com", uniqueAccountNumber("74"))

	original := externalInbound(t, svc, ctx, externalInboundPayload("external-in-conflict-event", "external-in-conflict-ref", TransferStatusSucceeded, 10000))
	journalCountAfterOriginal := len(store.journals)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
	assertBalance(t, svc, ctx, DemoInstitutionID, other.ID, 0)

	amountChanged := externalInboundPayload("external-in-conflict-event", "external-in-conflict-ref", TransferStatusSucceeded, 20000)
	amountReview := externalInbound(t, svc, ctx, amountChanged)
	if amountReview.HTTPStatus != http.StatusConflict ||
		amountReview.TransferID == nil ||
		amountReview.Status != TransferStatusFailed ||
		amountReview.ProviderStatus != TransferStatusSucceeded ||
		amountReview.LedgerStatus != LedgerStatusNoPosting ||
		amountReview.ReconciliationStatus != ReconciliationStatusManualReview ||
		amountReview.JournalEntryID != nil ||
		amountReview.Message != "provider_event_payload_conflict" {
		t.Fatalf("amount mismatch review mismatch: %+v", amountReview)
	}

	destinationChanged := externalInboundPayload("external-in-conflict-event", "external-in-conflict-ref", TransferStatusSucceeded, 10000)
	destinationChanged["destination_account_number"] = other.AccountNumber
	destinationReview := externalInbound(t, svc, ctx, destinationChanged)
	if destinationReview.HTTPStatus != http.StatusConflict ||
		destinationReview.TransferID == nil ||
		destinationReview.JournalEntryID != nil ||
		destinationReview.LedgerStatus != LedgerStatusNoPosting ||
		destinationReview.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("destination mismatch review mismatch: %+v", destinationReview)
	}

	duplicateReview := externalInbound(t, svc, ctx, amountChanged)
	if duplicateReview.TransferID == nil || *duplicateReview.TransferID != *amountReview.TransferID {
		t.Fatalf("same mismatch payload did not replay review: first=%+v second=%+v", amountReview, duplicateReview)
	}
	if original.TransferID == nil || *original.TransferID == *amountReview.TransferID {
		t.Fatalf("manual review reused original successful transfer: original=%+v review=%+v", original, amountReview)
	}
	if len(store.journals) != journalCountAfterOriginal {
		t.Fatalf("mismatch created a fake journal: before=%d after=%d", journalCountAfterOriginal, len(store.journals))
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
	assertBalance(t, svc, ctx, DemoInstitutionID, other.ID, 0)

	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, items, *amountReview.TransferID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)
	assertReconciliationItem(t, items, *destinationReview.TransferID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)
}

func TestExternalInboundEventPendingAndFailedDoNotPost(t *testing.T) {
	tests := []struct {
		name             string
		status           string
		wantLedgerStatus string
		wantReconStatus  string
	}{
		{name: "pending", status: TransferStatusPending, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusPending},
		{name: "failed", status: TransferStatusFailed, wantLedgerStatus: LedgerStatusNoPosting, wantReconStatus: ReconciliationStatusNoAction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, store := newTestService(t)
			journalCountBefore := len(store.journals)

			result := externalInbound(t, svc, ctx, externalInboundPayload("external-in-"+tt.name+"-event", "external-in-"+tt.name+"-ref", tt.status, 12000))

			if result.TransferID == nil ||
				result.Status != tt.status ||
				result.ProviderStatus != tt.status ||
				result.LedgerStatus != tt.wantLedgerStatus ||
				result.ReconciliationStatus != tt.wantReconStatus ||
				result.JournalEntryID != nil {
				t.Fatalf("external inbound %s mismatch: %+v", tt.name, result)
			}
			if len(store.journals) != journalCountBefore {
				t.Fatalf("%s event created a fake journal: before=%d after=%d", tt.name, journalCountBefore, len(store.journals))
			}
			assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
			history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assertTransactionPresent(t, history, *result.TransferID, 0, TransactionDirectionCredit)
		})
	}
}

func TestExternalInboundEventUnknownDestinationManualReviewNoCredit(t *testing.T) {
	ctx, svc, store := newTestService(t)
	payload := externalInboundPayload("external-in-unknown-event", "external-in-unknown-ref", TransferStatusSucceeded, 13000)
	payload["destination_account_number"] = "0000000007"
	journalCountBefore := len(store.journals)

	result := externalInbound(t, svc, ctx, payload)

	if result.TransferID == nil ||
		result.Status != TransferStatusFailed ||
		result.ProviderStatus != TransferStatusSucceeded ||
		result.LedgerStatus != LedgerStatusNoPosting ||
		result.ReconciliationStatus != ReconciliationStatusManualReview ||
		result.JournalEntryID != nil ||
		result.Message != "unknown_destination" {
		t.Fatalf("unknown destination review mismatch: %+v", result)
	}
	if len(store.journals) != journalCountBefore {
		t.Fatalf("unknown destination created a fake journal: before=%d after=%d", journalCountBefore, len(store.journals))
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("unknown destination appeared in customer history: %+v", history)
	}
	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, items, *result.TransferID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)
}

func TestExternalInboundEventBlockedDestinationManualReviewAndUnsupportedProvider(t *testing.T) {
	ctx, svc, store := newTestService(t)
	setMemoryAccountStatus(store, DemoCustomerAccountID, AccountStatusFrozen)
	journalCountBefore := len(store.journals)

	result := externalInbound(t, svc, ctx, externalInboundPayload("external-in-frozen-event", "external-in-frozen-ref", TransferStatusSucceeded, 10000))
	if result.TransferID == nil ||
		result.Status != TransferStatusFailed ||
		result.ProviderStatus != TransferStatusSucceeded ||
		result.LedgerStatus != LedgerStatusNoPosting ||
		result.ReconciliationStatus != ReconciliationStatusManualReview ||
		result.JournalEntryID != nil ||
		result.Message != "blocked_destination_account" {
		t.Fatalf("blocked destination review mismatch: %+v", result)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
	if len(store.journals) != journalCountBefore {
		t.Fatalf("blocked destination created a journal: before=%d after=%d", journalCountBefore, len(store.journals))
	}
	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferStatusSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, items, *result.TransferID, "provider_succeeded_ledger_not_posted", ReconciliationActionInspectJournal)

	payload := externalInboundPayload("external-in-provider-event", "external-in-provider-ref", TransferStatusSucceeded, 10000)
	payload["provider"] = "real_nibss"
	if _, err := svc.ExternalInboundEvent(ctx, externalInboundInput(t, payload)); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider, got %v", err)
	}
}

func TestMockInboundCallsProviderAdapter(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		webhookEvent: ProviderWebhookEvent{
			Provider:          ProviderMockNIP,
			InstitutionID:     DemoInstitutionID,
			AccountID:         DemoCustomerAccountID,
			Direction:         TransferDirectionInbound,
			Status:            TransferStatusSucceeded,
			AmountMinor:       15000,
			CurrencyID:        "NGN",
			IdempotencyKey:    "provider-adapter-in",
			ProviderReference: "provider-in-ref",
			ProviderEventID:   "provider-in-event",
			Narration:         "provider inbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockInbound(ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     999,
		IdempotencyKey:  "payload-idem",
		ProviderEventID: "payload-event",
	})
	if err != nil {
		t.Fatal(err)
	}

	if provider.parseCalls != 1 {
		t.Fatalf("expected one provider ParseWebhook call, got %d", provider.parseCalls)
	}
	if provider.lastHeaders["Idempotency-Key"] != "payload-idem" {
		t.Fatalf("provider did not receive idempotency header: %+v", provider.lastHeaders)
	}
	if transfer.AmountMinor != 15000 || transfer.ProviderReference != "provider-in-ref" {
		t.Fatalf("transfer did not use parsed provider webhook event: %+v", transfer)
	}
}
