package corebanking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReconciliationQueueIncludesExceptionsAndExcludesNormalTransfers(t *testing.T) {
	ctx, svc, store := newTestService(t)
	normal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "recon-normal-credit",
		Reference:      "recon-normal-credit-ref",
	})
	providerUnknown := createProviderUnknownTransfer(t, store, ctx, "recon-provider-unknown")
	manualReview := createReversalDeficitTransfer(t, svc, ctx, "recon-manual-review")
	stalePending := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       3000,
		IdempotencyKey:    "recon-stale-pending",
		ProviderEventID:   "recon-stale-pending-event",
		ProviderReference: "recon-stale-pending-ref",
		Status:            TransferStatusPending,
	})
	setTransferCreatedAt(t, store, stalePending.ID, time.Now().UTC().Add(-2*time.Hour))
	mismatch := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    7000,
		CurrencyID:     "NGN",
		IdempotencyKey: "recon-provider-ledger-mismatch",
		Reference:      "recon-provider-ledger-mismatch-ref",
	})
	setMemoryTransferStatuses(t, store, mismatch.ID, TransferStatusSucceeded, TransferStatusFailed, LedgerStatusPosted, ReconciliationStatusManualReview)

	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{StalePendingMinutes: 60})
	if err != nil {
		t.Fatal(err)
	}
	assertMissingReconciliationItem(t, items, normal.ID)
	assertReconciliationItem(t, items, providerUnknown.ID, "provider_unknown", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, items, manualReview.ID, "reversal_deficit", ReconciliationActionManualCustomerReceivableReview)
	assertReconciliationItem(t, items, stalePending.ID, "stale_pending", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, items, mismatch.ID, "provider_failed_ledger_posted", ReconciliationActionContactProvider)
}

func TestReconciliationQueueFiltersAndPagination(t *testing.T) {
	ctx, svc, store := newTestService(t)
	providerUnknown := createProviderUnknownTransfer(t, store, ctx, "recon-filter-provider")
	reversal := createReversalDeficitTransfer(t, svc, ctx, "recon-filter-reversal")
	mismatch := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    6000,
		CurrencyID:     "NGN",
		IdempotencyKey: "recon-filter-mismatch",
		Reference:      "recon-filter-mismatch-ref",
	})
	setMemoryTransferStatuses(t, store, mismatch.ID, TransferStatusSucceeded, TransferStatusFailed, LedgerStatusPosted, ReconciliationStatusManualReview)
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	setTransferCreatedAt(t, store, providerUnknown.ID, base)
	setTransferCreatedAt(t, store, reversal.ID, base)
	setTransferCreatedAt(t, store, mismatch.ID, base)

	providerItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: TransferProviderStatusUnknown})
	if err != nil {
		t.Fatal(err)
	}
	if len(providerItems) != 1 || providerItems[0].TransferID != providerUnknown.ID {
		t.Fatalf("provider_status filter mismatch: %+v", providerItems)
	}
	ledgerItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{LedgerStatus: LedgerStatusReversalDeficit})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledgerItems) != 1 || ledgerItems[0].TransferID != reversal.ID {
		t.Fatalf("ledger_status filter mismatch: %+v", ledgerItems)
	}
	reconciliationItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ReconciliationStatus: ReconciliationStatusManualReview})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, reconciliationItems, providerUnknown.ID, "provider_unknown", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, reconciliationItems, reversal.ID, "reversal_deficit", ReconciliationActionManualCustomerReceivableReview)
	assertReconciliationItem(t, reconciliationItems, mismatch.ID, "provider_failed_ledger_posted", ReconciliationActionContactProvider)

	firstPage, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("expected first page of two items, got %+v", firstPage)
	}
	secondPage, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{
		Limit:            2,
		BeforeCreatedAt:  &firstPage[len(firstPage)-1].CreatedAt,
		BeforeTransferID: firstPage[len(firstPage)-1].TransferID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("expected second page of one item, got %+v", secondPage)
	}
	seen := map[string]bool{}
	for _, item := range append(firstPage, secondPage...) {
		if seen[item.TransferID] {
			t.Fatalf("pagination returned duplicate reconciliation item %s", item.TransferID)
		}
		seen[item.TransferID] = true
	}
}

func TestReconciliationQueueTenantScopeAndInvalidInputs(t *testing.T) {
	ctx, svc, store := newTestService(t)
	itemTransfer := createProviderUnknownTransfer(t, store, ctx, "recon-tenant")

	items, err := svc.ListReconciliationItems(ctx, "99999999-9999-9999-9999-999999999999", ListReconciliationItemsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("reconciliation queue leaked cross-tenant items: %+v", items)
	}
	if _, err := svc.GetReconciliationItem(ctx, "99999999-9999-9999-9999-999999999999", itemTransfer.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant detail lookup to return not found, got %v", err)
	}
	now := time.Now().UTC()
	if _, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{BeforeTransferID: DemoCustomerAccountID}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected cursor without created_at to fail, got %v", err)
	}
	if _, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{BeforeCreatedAt: &now, BeforeTransferID: "not-a-uuid"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid cursor to fail, got %v", err)
	}
	if _, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ProviderStatus: "bad"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid filter to fail, got %v", err)
	}
}

func TestMarkReconciliationItemReviewedAuditsAndDoesNotMutateMoney(t *testing.T) {
	ctx, svc, store := newTestService(t)
	reversal := createReversalDeficitTransfer(t, svc, ctx, "recon-mark")
	beforeTransfer, err := svc.GetTransfer(ctx, DemoInstitutionID, reversal.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeBalance := mustBalance(t, svc, ctx, DemoInstitutionID, reversal.AccountID)
	beforeJournalCount, beforePostingCount := memoryJournalPostingCounts(store)

	item, err := svc.MarkReconciliationItemReviewed(ctx, MarkReconciliationItemReviewedInput{
		InstitutionID:    DemoInstitutionID,
		TransferID:       reversal.ID,
		ResolutionNote:   "Ops reviewed deficit and opened customer receivable ticket",
		ResolutionStatus: ReconciliationReviewStatusReviewed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ReviewStatus == nil || *item.ReviewStatus != ReconciliationReviewStatusReviewed || item.ReviewNote == nil || *item.ReviewNote == "" || item.ReviewedAt == nil {
		t.Fatalf("review metadata was not stored: %+v", item)
	}
	afterTransfer, err := svc.GetTransfer(ctx, DemoInstitutionID, reversal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterTransfer.Status != beforeTransfer.Status ||
		afterTransfer.ProviderStatus != beforeTransfer.ProviderStatus ||
		afterTransfer.LedgerStatus != beforeTransfer.LedgerStatus ||
		afterTransfer.ReconciliationStatus != beforeTransfer.ReconciliationStatus ||
		afterTransfer.AmountMinor != beforeTransfer.AmountMinor ||
		afterTransfer.AccountID != beforeTransfer.AccountID ||
		optionalAuditValue(afterTransfer.JournalEntryID) != optionalAuditValue(beforeTransfer.JournalEntryID) {
		t.Fatalf("mark-reviewed mutated transfer money/status fields: before=%+v after=%+v", beforeTransfer, afterTransfer)
	}
	afterBalance := mustBalance(t, svc, ctx, DemoInstitutionID, reversal.AccountID)
	assertBalanceSnapshotEqual(t, beforeBalance, afterBalance)
	afterJournalCount, afterPostingCount := memoryJournalPostingCounts(store)
	if afterJournalCount != beforeJournalCount || afterPostingCount != beforePostingCount {
		t.Fatalf("mark-reviewed mutated journals/postings: before journals=%d postings=%d after journals=%d postings=%d", beforeJournalCount, beforePostingCount, afterJournalCount, afterPostingCount)
	}
	events, err := store.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertAuditEventPresent(t, events, AuditActionReconciliationReviewed, func(event AuditEvent) bool {
		return auditString(event.TransferID) == reversal.ID &&
			auditString(event.NewStatus) == ReconciliationReviewStatusReviewed &&
			event.Metadata["review_reason"] == "reversal_deficit"
	})
	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertMissingReconciliationItem(t, items, reversal.ID)
}

func TestMarkReconciliationItemReviewedRequiresReviewableTransferAndNote(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	normal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    1000,
		CurrencyID:     "NGN",
		IdempotencyKey: "recon-mark-normal",
	})
	if _, err := svc.MarkReconciliationItemReviewed(ctx, MarkReconciliationItemReviewedInput{InstitutionID: DemoInstitutionID, TransferID: normal.ID, ResolutionNote: "review", ResolutionStatus: ReconciliationReviewStatusReviewed}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected normal transfer mark-reviewed to return not found, got %v", err)
	}
	if _, err := svc.MarkReconciliationItemReviewed(ctx, MarkReconciliationItemReviewedInput{InstitutionID: DemoInstitutionID, TransferID: normal.ID, ResolutionStatus: ReconciliationReviewStatusReviewed}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing note to fail, got %v", err)
	}
}

func createProviderUnknownTransfer(t *testing.T, store *memoryStore, ctx context.Context, idempotencyKey string) *Transfer {
	t.Helper()
	transfer, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       1111,
		CurrencyID:        "NGN",
		IdempotencyKey:    idempotencyKey,
		Provider:          ProviderMockNIP,
		ProviderReference: idempotencyKey + "-ref",
		ProviderEventID:   idempotencyKey + "-event",
		ProviderStatus:    TransferProviderStatusUnknown,
		Narration:         "provider unknown test transfer",
	})
	return mustTransfer(t, transfer, err)
}

func createReversalDeficitTransfer(t *testing.T, svc *Service, ctx context.Context, keyPrefix string) *Transfer {
	t.Helper()
	account := createMemoryCustomerAccount(t, svc, ctx, "Recon", keyPrefix, keyPrefix+"@example.com", uniqueAccountNumber("87"))
	original := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         account.ID,
		AmountMinor:       9000,
		IdempotencyKey:    keyPrefix + "-in",
		ProviderEventID:   keyPrefix + "-in-event",
		ProviderReference: keyPrefix + "-in-ref",
	})
	mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      account.ID,
		AmountMinor:    4000,
		IdempotencyKey: keyPrefix + "-spend",
	})
	return reverseTransfer(t, svc, ctx, original.ID, keyPrefix+"-reverse")
}

func setMemoryTransferStatuses(t *testing.T, store *memoryStore, transferID, status, providerStatus, ledgerStatus, reconciliationStatus string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	transfer := store.transfers[transferID]
	transfer.Status = status
	transfer.ProviderStatus = providerStatus
	transfer.LedgerStatus = ledgerStatus
	transfer.ReconciliationStatus = reconciliationStatus
	store.transfers[transferID] = transfer
}

func memoryJournalPostingCounts(store *memoryStore) (int, int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	postingCount := 0
	for _, postings := range store.postings {
		postingCount += len(postings)
	}
	return len(store.journals), postingCount
}

func mustBalance(t *testing.T, svc *Service, ctx context.Context, institutionID, accountID string) *AccountBalance {
	t.Helper()
	balance, err := svc.GetBalance(ctx, institutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	return balance
}

func assertBalanceSnapshotEqual(t *testing.T, before, after *AccountBalance) {
	t.Helper()
	if before.AccountID != after.AccountID ||
		before.InstitutionID != after.InstitutionID ||
		before.AvailableMinor != after.AvailableMinor ||
		before.LedgerMinor != after.LedgerMinor ||
		before.CurrencyID != after.CurrencyID ||
		optionalAuditValue(before.LastJournalEntryID) != optionalAuditValue(after.LastJournalEntryID) {
		t.Fatalf("mark-reviewed mutated balance: before=%+v after=%+v", before, after)
	}
}

func assertReconciliationItem(t *testing.T, items []ReconciliationItem, transferID, reviewReason, recommendedAction string) ReconciliationItem {
	t.Helper()
	for _, item := range items {
		if item.TransferID != transferID {
			continue
		}
		if item.ReviewReason != reviewReason || item.RecommendedNextAction != recommendedAction {
			t.Fatalf("reconciliation item mismatch for transfer %s: %+v", transferID, item)
		}
		return item
	}
	t.Fatalf("missing reconciliation item %s in %+v", transferID, items)
	return ReconciliationItem{}
}

func assertMissingReconciliationItem(t *testing.T, items []ReconciliationItem, transferID string) {
	t.Helper()
	for _, item := range items {
		if item.TransferID == transferID {
			t.Fatalf("unexpected reconciliation item %s in %+v", transferID, items)
		}
	}
}
