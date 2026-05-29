package corebanking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSuccessfulTransferInPostsBalancedLedger(t *testing.T) {
	ctx, svc, store := newTestService(t)
	transfer := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     50000,
		IdempotencyKey:  "in-1",
		ProviderEventID: "evt-in-1",
		Narration:       "inbound",
	})

	assertStatus(t, transfer, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	assertJournalBalanced(t, store, transfer)
}

func TestSuccessfulTransferOutPostsBalancedLedger(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 80000, IdempotencyKey: "fund", ProviderEventID: "evt-fund"})

	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		IdempotencyKey: "out-1",
		Narration:      "outbound",
	})

	assertStatus(t, transfer, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	assertJournalBalanced(t, store, transfer)
}

func TestPostingDeltasRespectCreditAndDebitNormalBalances(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 80000, IdempotencyKey: "normal-in", ProviderEventID: "evt-normal-in"})
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 80000)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoClearingAccountID, 80000)

	mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 30000, IdempotencyKey: "normal-out"})
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoClearingAccountID, 50000)
}

func TestPendingOutboundCreatesHoldAndReducesAvailable(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "hold-fund", ProviderEventID: "evt-hold-fund"})

	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       20000,
		IdempotencyKey:    "hold-out",
		ProviderReference: "hold-out-ref",
		Status:            TransferStatusPending,
	})

	assertStatus(t, transfer, TransferStatusPending)
	if transfer.JournalEntryID != nil {
		t.Fatalf("pending outbound should not post a journal: %+v", transfer)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 30000, 50000)
	hold := store.holds[transfer.ID]
	if hold.Status != HoldStatusActive || hold.AmountMinor != 20000 {
		t.Fatalf("pending outbound hold mismatch: %+v", hold)
	}
}

func TestFailedPendingOutboundReleasesHold(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "hold-fail-fund", ProviderEventID: "evt-hold-fail-fund"})
	pending := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       20000,
		IdempotencyKey:    "hold-fail-out",
		ProviderReference: "hold-fail-out-ref",
		Status:            TransferStatusPending,
	})

	failed := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusFailed,
		AmountMinor:       20000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "hold-fail-settle",
		ProviderReference: "hold-fail-out-ref",
		ProviderEventID:   "evt-hold-fail-settle",
		FailureReason:     "provider_failed",
	})

	if failed.ID != pending.ID {
		t.Fatalf("expected settlement to update pending transfer: pending=%s failed=%s", pending.ID, failed.ID)
	}
	assertStatus(t, failed, TransferStatusFailed)
	if failed.JournalEntryID != nil {
		t.Fatalf("failed pending outbound should not post a journal: %+v", failed)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	if hold := store.holds[pending.ID]; hold.Status != HoldStatusReleased {
		t.Fatalf("failed outbound should release hold: %+v", hold)
	}
}

func TestSuccessfulPendingOutboundPostsAndConsumesHold(t *testing.T) {
	ctx, svc, store := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "hold-success-fund", ProviderEventID: "evt-hold-success-fund"})
	pending := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       20000,
		IdempotencyKey:    "hold-success-out",
		ProviderReference: "hold-success-out-ref",
		Status:            TransferStatusPending,
	})

	succeeded := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       20000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "hold-success-settle",
		ProviderReference: "hold-success-out-ref",
		ProviderEventID:   "evt-hold-success-settle",
	})

	if succeeded.ID != pending.ID {
		t.Fatalf("expected settlement to update pending transfer: pending=%s succeeded=%s", pending.ID, succeeded.ID)
	}
	assertStatus(t, succeeded, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 30000)
	assertJournalBalanced(t, store, succeeded)
	if hold := store.holds[pending.ID]; hold.Status != HoldStatusConsumed {
		t.Fatalf("successful outbound should consume hold: %+v", hold)
	}
}

func TestBalanceEnquiryTracksActiveReleasedAndConsumedHolds(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "balance-hold-fund", ProviderEventID: "evt-balance-hold-fund"})

	pendingToFail := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       20000,
		IdempotencyKey:    "balance-hold-fail-out",
		ProviderReference: "balance-hold-fail-ref",
		Status:            TransferStatusPending,
	})
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 30000, 50000)

	failed := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusFailed,
		AmountMinor:       20000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "balance-hold-fail-settle",
		ProviderReference: "balance-hold-fail-ref",
		ProviderEventID:   "evt-balance-hold-fail-settle",
	})
	if failed.ID != pendingToFail.ID {
		t.Fatalf("failed settlement should update pending transfer: pending=%s failed=%s", pendingToFail.ID, failed.ID)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000, 50000)

	pendingToSucceed := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       15000,
		IdempotencyKey:    "balance-hold-success-out",
		ProviderReference: "balance-hold-success-ref",
		Status:            TransferStatusPending,
	})
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 35000, 50000)

	succeeded := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       15000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "balance-hold-success-settle",
		ProviderReference: "balance-hold-success-ref",
		ProviderEventID:   "evt-balance-hold-success-settle",
	})
	if succeeded.ID != pendingToSucceed.ID {
		t.Fatalf("successful settlement should update pending transfer: pending=%s succeeded=%s", pendingToSucceed.ID, succeeded.ID)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 35000, 35000)
}

func TestInsufficientFundsDoesNotDebit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "out-insufficient",
	})

	assertStatus(t, transfer, TransferStatusFailed)
	if transfer.JournalEntryID != nil {
		t.Fatalf("failed transfer should not have journal entry")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestDuplicateIdempotencyKeyDoesNotDoublePost(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	req := TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-1", ProviderEventID: "evt-idem-1"}
	first := mockInbound(t, svc, ctx, req)
	second := mockInbound(t, svc, ctx, req)

	if first.ID != second.ID {
		t.Fatalf("expected duplicate idempotency request to return original transfer")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
}

func TestDuplicateProviderEventDoesNotDoubleCredit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	first := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-1", ProviderEventID: "evt-provider"})
	second := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-2", ProviderEventID: "evt-provider"})

	if first.ID != second.ID {
		t.Fatalf("expected duplicate provider event to return original transfer")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
}

func TestDuplicateProviderEventRejectsChangedPayload(t *testing.T) {
	tests := []struct {
		name   string
		second func(accountID string) TransferRequest
	}{
		{
			name: "amount",
			second: func(accountID string) TransferRequest {
				return TransferRequest{AccountID: accountID, AmountMinor: 20000, IdempotencyKey: "idem-provider-payload-amount-2", ProviderEventID: "evt-provider-payload-amount"}
			},
		},
		{
			name: "account",
			second: func(accountID string) TransferRequest {
				return TransferRequest{AccountID: accountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-payload-account-2", ProviderEventID: "evt-provider-payload-account"}
			},
		},
		{
			name: "status",
			second: func(accountID string) TransferRequest {
				return TransferRequest{AccountID: accountID, AmountMinor: 10000, IdempotencyKey: "idem-provider-payload-status-2", ProviderEventID: "evt-provider-payload-status", Status: TransferStatusFailed}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, _ := newTestService(t)
			otherAccount := createMemoryCustomerAccount(t, svc, ctx, "Provider", "Replay", "provider.replay."+tt.name+"@example.com", uniqueAccountNumber("75"))
			eventID := "evt-provider-payload-" + tt.name
			if _, err := svc.MockInbound(ctx, TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     10000,
				IdempotencyKey:  "idem-provider-payload-" + tt.name + "-1",
				ProviderEventID: eventID,
			}); err != nil {
				t.Fatal(err)
			}
			second := tt.second(DemoCustomerAccountID)
			if tt.name == "account" {
				second = tt.second(otherAccount.ID)
			}
			if _, err := svc.MockInbound(ctx, second); !errors.Is(err, ErrConflict) {
				t.Fatalf("expected changed provider-event %s to return conflict, got %v", tt.name, err)
			}
			assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 10000)
			assertBalance(t, svc, ctx, DemoInstitutionID, otherAccount.ID, 0)
		})
	}
}

func TestUnsupportedProviderWebhookRejectedBeforeMoneyMovement(t *testing.T) {
	ctx, svc, store := newTestService(t)
	_, err := svc.MockProviderWebhook(ctx, "unsupported_provider", []byte(`{
		"account_id":"44444444-4444-4444-4444-444444444444",
		"amount_minor":10000,
		"idempotency_key":"unsupported-provider",
		"provider_event_id":"unsupported-provider-event"
	}`), map[string]string{"X-Institution-ID": DemoInstitutionID})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected unsupported provider to be rejected, got %v", err)
	}
	if len(store.transfers) != 0 || len(store.journals) != 0 || len(store.postings) != 0 {
		t.Fatalf("unsupported provider should not move money: transfers=%d journals=%d postings=%d", len(store.transfers), len(store.journals), len(store.postings))
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestExternalNameEnquiryReturnsFoundAndDoesNotMoveMoney(t *testing.T) {
	ctx, svc, store := newTestService(t)
	beforeRows := memoryMoneyRowCounts(store)
	beforeBalance := mustBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID)

	result, err := svc.ExternalNameEnquiry(ctx, ExternalNameEnquiryInput{
		InstitutionID:              DemoInstitutionID,
		DestinationInstitutionCode: mockNIPDemoBankCode,
		AccountNumber:              mockNIPDemoAccountNumber,
		CurrencyID:                 "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != ProviderMockNIP ||
		result.DestinationInstitutionCode != mockNIPDemoBankCode ||
		result.AccountNumber != mockNIPDemoAccountNumber ||
		result.AccountName != mockNIPDemoAccountName ||
		result.ProviderReference == nil ||
		result.Status != NameEnquiryStatusFound ||
		result.Message != "account_found" ||
		result.CreatedAt.IsZero() {
		t.Fatalf("name enquiry result mismatch: %+v", result)
	}

	afterRows := memoryMoneyRowCounts(store)
	if afterRows != beforeRows {
		t.Fatalf("name enquiry created money rows: before=%+v after=%+v", beforeRows, afterRows)
	}
	afterBalance := mustBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID)
	assertNameEnquiryBalanceUnchanged(t, beforeBalance, afterBalance)
	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("name enquiry should not create transaction history, got %+v", history)
	}
}

func TestExternalNameEnquiryReturnsControlledProviderStatuses(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	tests := []struct {
		name                       string
		destinationInstitutionCode string
		accountNumber              string
		wantStatus                 string
		wantMessage                string
	}{
		{
			name:                       "unknown account",
			destinationInstitutionCode: mockNIPDemoBankCode,
			accountNumber:              "9990000002",
			wantStatus:                 NameEnquiryStatusNotFound,
			wantMessage:                "account_not_found",
		},
		{
			name:                       "provider unavailable",
			destinationInstitutionCode: mockNIPUnavailableBankCode,
			accountNumber:              mockNIPDemoAccountNumber,
			wantStatus:                 NameEnquiryStatusProviderUnavailable,
			wantMessage:                "provider_unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.ExternalNameEnquiry(ctx, ExternalNameEnquiryInput{
				InstitutionID:              DemoInstitutionID,
				DestinationInstitutionCode: tt.destinationInstitutionCode,
				AccountNumber:              tt.accountNumber,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tt.wantStatus || result.Message != tt.wantMessage || result.AccountName != "" || result.ProviderReference != nil {
				t.Fatalf("unexpected controlled name enquiry result: %+v", result)
			}
		})
	}
}

func TestExternalNameEnquiryRejectsUnsupportedProviderWithoutMoneyMovement(t *testing.T) {
	ctx, svc, store := newTestService(t)
	beforeRows := memoryMoneyRowCounts(store)

	_, err := svc.ExternalNameEnquiry(ctx, ExternalNameEnquiryInput{
		InstitutionID:              DemoInstitutionID,
		Provider:                   "unsupported_provider",
		DestinationInstitutionCode: mockNIPDemoBankCode,
		AccountNumber:              mockNIPDemoAccountNumber,
	})
	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
	afterRows := memoryMoneyRowCounts(store)
	if afterRows != beforeRows {
		t.Fatalf("unsupported provider should not create money rows: before=%+v after=%+v", beforeRows, afterRows)
	}
}

func TestPendingTransferAppearsInHistory(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	transfer := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     10000,
		IdempotencyKey:  "pending-1",
		ProviderEventID: "evt-pending-1",
		Status:          TransferStatusPending,
	})

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TransferID != transfer.ID || history[0].Status != TransferStatusPending || history[0].SignedAmountMinor != 0 {
		t.Fatalf("pending history row mismatch: %+v", history)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestFailedTransferDoesNotLoseMoney(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "fund-failed", ProviderEventID: "evt-fund-failed"})
	transfer := mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "failed-1", Status: TransferStatusFailed})

	assertStatus(t, transfer, TransferStatusFailed)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 50000)
	if transfer.JournalEntryID != nil {
		t.Fatalf("failed transfer should not post a journal")
	}
}

func TestReversalCreatesNewLedgerEvent(t *testing.T) {
	ctx, svc, store := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 25000, IdempotencyKey: "rev-in", ProviderEventID: "evt-rev-in"})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "reverse-1")

	if reversal.ID == original.ID || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		t.Fatalf("reversal did not reference original: %+v", reversal)
	}
	if reversal.JournalEntryID == nil || *reversal.JournalEntryID == *original.JournalEntryID {
		t.Fatalf("reversal should create a distinct journal entry")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
	assertJournalBalanced(t, store, reversal)
	if reversal.LedgerStatus != LedgerStatusPosted || reversal.ReconciliationStatus != ReconciliationStatusMatched {
		t.Fatalf("sufficient reversal should be normally posted: %+v", reversal)
	}
}

func TestReversalUsesOriginalInternalTransferCounterparty(t *testing.T) {
	ctx, svc, store := newTestService(t)
	source := createMemoryCustomerAccount(t, svc, ctx, "Reverse", "Source", "reverse.source@example.com", uniqueAccountNumber("90"))
	destination := createMemoryCustomerAccount(t, svc, ctx, "Reverse", "Destination", "reverse.destination@example.com", uniqueAccountNumber("91"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    20000,
		CurrencyID:     "NGN",
		IdempotencyKey: "reverse-internal-fund",
	})
	original := mustInternalTransfer(t, svc, ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          7000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "reverse-internal-transfer",
		Reference:            "reverse-internal-transfer-ref",
	})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "reverse-internal-transfer-reversal")

	if reversal.Direction != TransferDirectionReversal || reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		t.Fatalf("reversal did not reference original internal transfer: %+v", reversal)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, source.ID, 20000)
	assertBalance(t, svc, ctx, DemoInstitutionID, destination.ID, 0)
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoClearingAccountID, 20000)
	assertJournalBalanced(t, store, reversal)
}

func TestReversalDeficitIsMarkedForManualReview(t *testing.T) {
	ctx, svc, store := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "spent-rev-in", ProviderEventID: "evt-spent-rev-in"})
	mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 20000, IdempotencyKey: "spent-rev-out"})

	reversal := reverseTransfer(t, svc, ctx, original.ID, "spent-reverse-1")

	assertStatus(t, reversal, TransferStatusSucceeded)
	if reversal.JournalEntryID == nil {
		t.Fatalf("spent-funds reversal should still create a journal")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, -20000)
	assertJournalBalanced(t, store, reversal)
	if reversal.LedgerStatus != LedgerStatusReversalDeficit || reversal.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("deficit reversal should be marked for manual review: %+v", reversal)
	}
}

func TestStandardAccountCannotSpendReversalDeficit(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "deficit-spend-in", ProviderEventID: "evt-deficit-spend-in"})
	mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 20000, IdempotencyKey: "deficit-spend-out"})
	reverseTransfer(t, svc, ctx, original.ID, "deficit-spend-reverse")

	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    1,
		IdempotencyKey: "deficit-spend-attempt",
	})

	assertStatus(t, transfer, TransferStatusFailed)
	if transfer.FailureReason == nil || *transfer.FailureReason != "insufficient_funds" {
		t.Fatalf("deficit spend should fail as insufficient funds: %+v", transfer)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, -20000)
}

func TestReversalRejectsUnrelatedIdempotencyKey(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "collision-in", ProviderEventID: "evt-collision-in"})

	_, err := svc.ReverseTransfer(ctx, DemoInstitutionID, original.ID, "collision-in")

	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected idempotency collision to be rejected, got %v", err)
	}
	stillOriginal, err := svc.GetTransfer(ctx, DemoInstitutionID, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillOriginal.Direction != TransferDirectionInbound {
		t.Fatalf("idempotency collision mutated original transfer: %+v", stillOriginal)
	}
}

func TestSecondReversalWithNewIdempotencyKeyIsRejected(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	original := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 50000, IdempotencyKey: "double-rev-in", ProviderEventID: "evt-double-rev-in"})

	first := reverseTransfer(t, svc, ctx, original.ID, "double-rev-reverse-1")

	_, err := svc.ReverseTransfer(ctx, DemoInstitutionID, original.ID, "double-rev-reverse-2")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected second reversal with new idempotency key to be rejected as conflict, got %v", err)
	}

	replay := reverseTransfer(t, svc, ctx, original.ID, "double-rev-reverse-1")
	if replay.ID != first.ID {
		t.Fatalf("expected same-key replay to return the original reversal: first=%s replay=%s", first.ID, replay.ID)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 0)
}

func TestTenantScopingPreventsCrossTenantReads(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "tenant-in", ProviderEventID: "evt-tenant-in"})

	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant balance read to fail, got %v", err)
	}
	if _, err := svc.GetTransactions(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID, ListTransactionsOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant history read to fail, got %v", err)
	}
}

func TestProductionReadsRequireInstitutionScope(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	if _, err := svc.GetBalance(ctx, "", DemoCustomerAccountID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing institution scope to fail, got %v", err)
	}
	if _, err := svc.GetTransactions(ctx, "", DemoCustomerAccountID, ListTransactionsOptions{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing institution scope to fail for history, got %v", err)
	}
	if _, err := svc.ListTransfers(ctx, "", ListTransfersOptions{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing institution scope to fail for transfer list, got %v", err)
	}
}

func TestTransactionHistoryComesFromLenzRecords(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	inbound := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 12000, IdempotencyKey: "hist-in", ProviderEventID: "evt-hist-in"})
	outbound := mockOutbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 2000, IdempotencyKey: "hist-out"})

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("expected two Lenz transaction records, got %d", len(history))
	}
	ids := map[string]bool{history[0].TransferID: true, history[1].TransferID: true}
	if !ids[inbound.ID] || !ids[outbound.ID] {
		t.Fatalf("history did not reference Lenz transfer IDs: %+v", history)
	}
	for _, txn := range history {
		if txn.JournalEntryID == nil {
			t.Fatalf("succeeded history row must come from Lenz journal/posting record: %+v", txn)
		}
	}
}

func TestTransactionHistoryDefaultsToOneHundredAndOrdersNewestFirst(t *testing.T) {
	ctx, svc, store := newTestService(t)
	base := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	var newestID string
	for i := 0; i < 105; i++ {
		transfer := mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     1000 + int64(i),
			IdempotencyKey:  "hist-default-in-" + uuid.NewString(),
			ProviderEventID: "hist-default-event-" + uuid.NewString(),
		})
		createdAt := base.Add(time.Duration(i) * time.Minute)
		setTransferCreatedAt(t, store, transfer.ID, createdAt)
		if i == 104 {
			newestID = transfer.ID
		}
	}

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != DefaultTransactionHistoryLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultTransactionHistoryLimit, len(history))
	}
	if history[0].TransferID != newestID {
		t.Fatalf("expected newest transaction first, got %+v", history[0])
	}
	assertHistoryNewestFirst(t, history)
}

func TestTransactionHistoryCapsLimitAndPaginatesBeforeCreatedAt(t *testing.T) {
	ctx, svc, store := newTestService(t)
	base := time.Date(2026, 5, 16, 13, 0, 0, 0, time.UTC)
	for i := 0; i < 205; i++ {
		transfer := mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     2000 + int64(i),
			IdempotencyKey:  "hist-page-in-" + uuid.NewString(),
			ProviderEventID: "hist-page-event-" + uuid.NewString(),
		})
		setTransferCreatedAt(t, store, transfer.ID, base.Add(time.Duration(i)*time.Minute))
	}

	capped, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != MaxTransactionHistoryLimit {
		t.Fatalf("expected limit capped at %d, got %d", MaxTransactionHistoryLimit, len(capped))
	}
	assertHistoryNewestFirst(t, capped)

	cursor := capped[49].CreatedAt
	page, err := svc.GetTransactions(ctx, DemoInstitutionID, DemoCustomerAccountID, ListTransactionsOptions{
		Limit:           25,
		BeforeCreatedAt: &cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 25 {
		t.Fatalf("expected 25 paged rows, got %d", len(page))
	}
	for _, txn := range page {
		if !txn.CreatedAt.Before(cursor) {
			t.Fatalf("paged row was not before cursor %s: %+v", cursor, txn)
		}
	}
	assertHistoryNewestFirst(t, page)
}

func TestTransferListDefaultsCapsTenantScopeAndCursorPagination(t *testing.T) {
	ctx, svc, store := newTestService(t)
	base := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	otherTenantID := "99999999-9999-9999-9999-999999999999"
	for i := 1; i <= 205; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		if i >= 204 {
			createdAt = base.Add(205 * time.Minute)
		}
		putMemoryTransferForList(t, store, numberedTestUUID("11111111-1111-1111-1111", i), DemoInstitutionID, createdAt)
	}
	otherTenantTransfer := putMemoryTransferForList(t, store, numberedTestUUID("99999999-9999-9999-9999", 1), otherTenantID, base.Add(300*time.Minute))

	transfers, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(transfers) != DefaultTransferListLimit {
		t.Fatalf("expected default transfer limit %d, got %d", DefaultTransferListLimit, len(transfers))
	}
	assertTransfersNewestFirst(t, transfers)
	assertTransferListMissing(t, transfers, otherTenantTransfer.ID)
	if transfers[0].ID != numberedTestUUID("11111111-1111-1111-1111", 205) || transfers[1].ID != numberedTestUUID("11111111-1111-1111-1111", 204) {
		t.Fatalf("expected created_at tie to be ordered by transfer id desc, got first rows %+v", transfers[:2])
	}

	capped, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != MaxTransferListLimit {
		t.Fatalf("expected transfer limit capped at %d, got %d", MaxTransferListLimit, len(capped))
	}
	assertTransfersNewestFirst(t, capped)

	firstPage, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{
		Limit:            2,
		BeforeCreatedAt:  &firstPage[len(firstPage)-1].CreatedAt,
		BeforeTransferID: firstPage[len(firstPage)-1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateTransfers(t, append(firstPage, secondPage...))
	assertTransfersNewestFirst(t, secondPage)

	otherTenantTransfers, err := svc.ListTransfers(ctx, otherTenantID, ListTransfersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantTransfers) != 1 || otherTenantTransfers[0].ID != otherTenantTransfer.ID {
		t.Fatalf("expected transfer list to stay tenant scoped, got %+v", otherTenantTransfers)
	}
}

func TestTransferListRejectsInvalidCursor(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	now := time.Now().UTC()

	if _, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{BeforeTransferID: DemoCustomerAccountID}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected transfer cursor without created_at to fail, got %v", err)
	}
	if _, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{BeforeCreatedAt: &now, BeforeTransferID: "not-a-uuid"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid transfer cursor id to fail, got %v", err)
	}
}

func TestAuditEventListDefaultsCapsTenantScopeAndCursorPagination(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	base := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)
	otherTenantID := "99999999-9999-9999-9999-999999999999"
	for i := 1; i <= 205; i++ {
		createdAt := base.Add(time.Duration(i) * time.Minute)
		if i >= 204 {
			createdAt = base.Add(205 * time.Minute)
		}
		putMemoryAuditEventForList(store, numberedTestUUID("22222222-2222-2222-2222", i), DemoInstitutionID, createdAt)
	}
	otherTenantEvent := putMemoryAuditEventForList(store, numberedTestUUID("99999999-9999-9999-9999", 2), otherTenantID, base.Add(300*time.Minute))

	events, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != DefaultAuditEventListLimit {
		t.Fatalf("expected default audit limit %d, got %d", DefaultAuditEventListLimit, len(events))
	}
	assertAuditEventsNewestFirst(t, events)
	assertAuditEventListMissing(t, events, otherTenantEvent.ID)
	if events[0].ID != numberedTestUUID("22222222-2222-2222-2222", 205) || events[1].ID != numberedTestUUID("22222222-2222-2222-2222", 204) {
		t.Fatalf("expected created_at tie to be ordered by audit id desc, got first rows %+v", events[:2])
	}

	capped, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped) != MaxAuditEventListLimit {
		t.Fatalf("expected audit limit capped at %d, got %d", MaxAuditEventListLimit, len(capped))
	}

	firstPage, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	secondPage, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{
		Limit:              2,
		BeforeCreatedAt:    &firstPage[len(firstPage)-1].CreatedAt,
		BeforeAuditEventID: firstPage[len(firstPage)-1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateAuditEvents(t, append(firstPage, secondPage...))
	assertAuditEventsNewestFirst(t, secondPage)

	otherTenantEvents, err := store.ListAuditEvents(ctx, otherTenantID, ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantEvents) != 1 || otherTenantEvents[0].ID != otherTenantEvent.ID {
		t.Fatalf("expected audit list to stay tenant scoped, got %+v", otherTenantEvents)
	}

	emptyEvents, err := store.ListAuditEvents(ctx, "88888888-8888-8888-8888-888888888888", ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if emptyEvents == nil || len(emptyEvents) != 0 {
		t.Fatalf("expected empty audit list to be [], got %+v", emptyEvents)
	}
}

func TestAuditEventListRejectsInvalidCursor(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{BeforeAuditEventID: DemoCustomerAccountID}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected audit cursor without created_at to fail, got %v", err)
	}
	if _, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{BeforeCreatedAt: &now, BeforeAuditEventID: "not-a-uuid"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid audit cursor id to fail, got %v", err)
	}
}
