//go:build integration

package corebanking

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestSQLReconciliationQueueGoal10(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	normal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-recon-normal",
		Reference:      "sql-recon-normal-ref",
	})
	providerUnknownTransfer, err := svc.repository.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       1111,
		CurrencyID:        "NGN",
		IdempotencyKey:    "sql-recon-provider-unknown",
		Provider:          ProviderMockNIP,
		ProviderReference: "sql-recon-provider-unknown-ref",
		ProviderEventID:   "sql-recon-provider-unknown-event",
		ProviderStatus:    TransferProviderStatusUnknown,
		Narration:         "SQL provider unknown transfer",
	})
	providerUnknown := mustTransfer(t, providerUnknownTransfer, err)
	manualReview := createSQLReversalDeficitTransfer(t, svc, ctx, "sql-recon-manual")
	stalePending := mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       3000,
		IdempotencyKey:    "sql-recon-stale-pending",
		ProviderEventID:   "sql-recon-stale-pending-event",
		ProviderReference: "sql-recon-stale-pending-ref",
		Status:            TransferStatusPending,
	})
	setSQLReconciliationTransferCreatedAt(t, db, stalePending.ID, time.Now().UTC().Add(-2*time.Hour))
	mismatch := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    7000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-recon-mismatch",
		Reference:      "sql-recon-mismatch-ref",
	})
	setSQLTransferStatuses(t, db, mismatch.ID, TransferStatusSucceeded, TransferStatusFailed, LedgerStatusPosted, ReconciliationStatusManualReview)

	items, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{StalePendingMinutes: 60})
	if err != nil {
		t.Fatal(err)
	}
	assertMissingReconciliationItem(t, items, normal.ID)
	assertReconciliationItem(t, items, providerUnknown.ID, "provider_unknown", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, items, manualReview.ID, "reversal_deficit", ReconciliationActionManualCustomerReceivableReview)
	assertReconciliationItem(t, items, stalePending.ID, "stale_pending", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, items, mismatch.ID, "provider_failed_ledger_posted", ReconciliationActionContactProvider)

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
	if len(ledgerItems) != 1 || ledgerItems[0].TransferID != manualReview.ID {
		t.Fatalf("ledger_status filter mismatch: %+v", ledgerItems)
	}
	reconItems, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{ReconciliationStatus: ReconciliationStatusManualReview})
	if err != nil {
		t.Fatal(err)
	}
	assertReconciliationItem(t, reconItems, providerUnknown.ID, "provider_unknown", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, reconItems, manualReview.ID, "reversal_deficit", ReconciliationActionManualCustomerReceivableReview)
	assertReconciliationItem(t, reconItems, mismatch.ID, "provider_failed_ledger_posted", ReconciliationActionContactProvider)

	paged, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(paged) != 2 {
		t.Fatalf("expected first page of two items, got %+v", paged)
	}
	next, err := svc.ListReconciliationItems(ctx, DemoInstitutionID, ListReconciliationItemsOptions{
		Limit:            3,
		BeforeCreatedAt:  &paged[len(paged)-1].CreatedAt,
		BeforeTransferID: paged[len(paged)-1].TransferID,
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range append(paged, next...) {
		if seen[item.TransferID] {
			t.Fatalf("pagination returned duplicate reconciliation item %s", item.TransferID)
		}
		seen[item.TransferID] = true
	}
}

func TestSQLMarkReconciliationItemReviewedAuditsAndDoesNotMutateMoney(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	reversal := createSQLReversalDeficitTransfer(t, svc, ctx, "sql-recon-mark")
	beforeBalance := mustBalance(t, svc, ctx, DemoInstitutionID, reversal.AccountID)
	beforeJournalCount, beforePostingCount := sqlJournalPostingCounts(t, db)

	item, err := svc.MarkReconciliationItemReviewed(ctx, MarkReconciliationItemReviewedInput{
		InstitutionID:    DemoInstitutionID,
		TransferID:       reversal.ID,
		ResolutionNote:   "SQL ops reviewed deficit and opened receivable ticket",
		ResolutionStatus: ReconciliationReviewStatusReviewed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.ReviewStatus == nil || *item.ReviewStatus != ReconciliationReviewStatusReviewed || item.ReviewNote == nil || *item.ReviewNote == "" || item.ReviewedAt == nil {
		t.Fatalf("review metadata was not stored: %+v", item)
	}
	afterBalance := mustBalance(t, svc, ctx, DemoInstitutionID, reversal.AccountID)
	assertBalanceSnapshotEqual(t, beforeBalance, afterBalance)
	afterJournalCount, afterPostingCount := sqlJournalPostingCounts(t, db)
	if afterJournalCount != beforeJournalCount || afterPostingCount != beforePostingCount {
		t.Fatalf("mark-reviewed mutated journals/postings: before journals=%d postings=%d after journals=%d postings=%d", beforeJournalCount, beforePostingCount, afterJournalCount, afterPostingCount)
	}
	events, err := svc.repository.ListAuditEvents(ctx, DemoInstitutionID)
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

func createSQLReversalDeficitTransfer(t *testing.T, svc *Service, ctx context.Context, keyPrefix string) *Transfer {
	t.Helper()
	account := createSQLCustomerAccount(t, svc, ctx, "SQLRecon", keyPrefix, keyPrefix+".sql@example.com", uniqueAccountNumber("93"), "SQL Recon "+keyPrefix)
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

func setSQLReconciliationTransferCreatedAt(t *testing.T, db *sqlx.DB, transferID string, createdAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE transfers SET created_at = $1 WHERE id = $2`, createdAt, transferID); err != nil {
		t.Fatal(err)
	}
}

func setSQLTransferStatuses(t *testing.T, db *sqlx.DB, transferID, status, providerStatus, ledgerStatus, reconciliationStatus string) {
	t.Helper()
	if _, err := db.Exec(`
UPDATE transfers
SET status = $1,
    provider_status = $2,
    ledger_status = $3,
    reconciliation_status = $4
WHERE id = $5`, status, providerStatus, ledgerStatus, reconciliationStatus, transferID); err != nil {
		t.Fatal(err)
	}
}

func sqlJournalPostingCounts(t *testing.T, db *sqlx.DB) (int, int) {
	t.Helper()
	var journalCount int
	if err := db.Get(&journalCount, `SELECT COUNT(*) FROM journal_entries`); err != nil {
		t.Fatal(err)
	}
	var postingCount int
	if err := db.Get(&postingCount, `SELECT COUNT(*) FROM postings`); err != nil {
		t.Fatal(err)
	}
	return journalCount, postingCount
}
