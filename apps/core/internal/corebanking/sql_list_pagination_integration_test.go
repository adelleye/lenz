//go:build integration

package corebanking

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

func TestSQLTransferListCursorPagination(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	base := time.Date(2026, 5, 26, 12, 30, 0, 0, time.UTC)

	older := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 1000, IdempotencyKey: "sql-transfer-list-older", ProviderEventID: "sql-transfer-list-older-event"})
	newerA := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 1001, IdempotencyKey: "sql-transfer-list-newer-a", ProviderEventID: "sql-transfer-list-newer-a-event"})
	newerB := mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 1002, IdempotencyKey: "sql-transfer-list-newer-b", ProviderEventID: "sql-transfer-list-newer-b-event"})
	setSQLTransferCreatedAt(t, ctx, db, older.ID, base)
	setSQLTransferCreatedAt(t, ctx, db, newerA.ID, base.Add(time.Minute))
	setSQLTransferCreatedAt(t, ctx, db, newerB.ID, base.Add(time.Minute))

	firstPage, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("expected first page of two transfers, got %+v", firstPage)
	}
	assertTransfersNewestFirst(t, firstPage)

	secondPage, err := svc.ListTransfers(ctx, DemoInstitutionID, ListTransfersOptions{
		Limit:            2,
		BeforeCreatedAt:  &firstPage[len(firstPage)-1].CreatedAt,
		BeforeTransferID: firstPage[len(firstPage)-1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoDuplicateTransfers(t, append(firstPage, secondPage...))
	assertTransferPresent(t, append(firstPage, secondPage...), older.ID)

	otherTenantTransfers, err := svc.ListTransfers(ctx, "99999999-9999-9999-9999-999999999999", ListTransfersOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if otherTenantTransfers == nil || len(otherTenantTransfers) != 0 {
		t.Fatalf("expected cross-tenant SQL transfer list to be [], got %+v", otherTenantTransfers)
	}
}

func TestSQLAuditEventListCursorPagination(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	base := time.Date(2026, 5, 26, 13, 30, 0, 0, time.UTC)

	older := insertSQLAuditEventForList(t, db, ctx, "sql.audit.list.older", base)
	newerA := insertSQLAuditEventForList(t, db, ctx, "sql.audit.list.newer_a", base.Add(time.Minute))
	newerB := insertSQLAuditEventForList(t, db, ctx, "sql.audit.list.newer_b", base.Add(time.Minute))

	firstPage, err := svc.repository.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage) != 2 {
		t.Fatalf("expected first page of two audit events, got %+v", firstPage)
	}
	assertAuditEventsNewestFirst(t, firstPage)

	secondPage, err := svc.repository.ListAuditEvents(ctx, DemoInstitutionID, ListAuditEventsOptions{
		Limit:              2,
		BeforeCreatedAt:    &firstPage[len(firstPage)-1].CreatedAt,
		BeforeAuditEventID: firstPage[len(firstPage)-1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	combined := append(firstPage, secondPage...)
	assertNoDuplicateAuditEvents(t, combined)
	assertAuditEventPresent(t, combined, older.Action, func(event AuditEvent) bool {
		return event.ID == older.ID
	})
	assertAuditEventPresent(t, combined, newerA.Action, func(event AuditEvent) bool {
		return event.ID == newerA.ID
	})
	assertAuditEventPresent(t, combined, newerB.Action, func(event AuditEvent) bool {
		return event.ID == newerB.ID
	})

	otherTenantEvents, err := svc.repository.ListAuditEvents(ctx, "99999999-9999-9999-9999-999999999999", ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if otherTenantEvents == nil || len(otherTenantEvents) != 0 {
		t.Fatalf("expected cross-tenant SQL audit list to be [], got %+v", otherTenantEvents)
	}
}

func insertSQLAuditEventForList(t *testing.T, db *sqlx.DB, ctx context.Context, action string, createdAt time.Time) *AuditEvent {
	t.Helper()
	var inserted *AuditEvent
	if err := WithTx(ctx, db, func(tx TxRunner) error {
		event, err := insertAuditEvent(ctx, tx, auditEventInput{
			InstitutionID: DemoInstitutionID,
			Action:        action,
			EntityType:    "transfer",
			EntityID:      action,
			CreatedAt:     createdAt,
		})
		inserted = event
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return inserted
}

func assertTransferPresent(t *testing.T, transfers []Transfer, transferID string) {
	t.Helper()
	for _, transfer := range transfers {
		if transfer.ID == transferID {
			return
		}
	}
	t.Fatalf("transfer %s missing from list: %+v", transferID, transfers)
}
