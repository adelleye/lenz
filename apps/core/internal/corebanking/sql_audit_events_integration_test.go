//go:build integration

package corebanking

import (
	"context"
	"testing"
)

func TestSQLAuditEventsGoal09(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	account := createSQLCustomerAccount(t, svc, ctx, "SQLAudit", "Source", "sql.audit.source@example.com", "8734567890", "SQL Audit Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "SQLAudit", "Dest", "sql.audit.dest@example.com", "8734567891", "SQL Audit Dest")

	creditInput := InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 50000, CurrencyID: "NGN", IdempotencyKey: "sql-audit-credit-001", Reference: "sql-audit-credit-ref"}
	credit := mustInternalCredit(t, svc, ctx, creditInput)
	duplicateCredit := mustInternalCredit(t, svc, ctx, creditInput)
	if duplicateCredit.ID != credit.ID {
		t.Fatalf("duplicate credit idempotency created a new transfer: first=%s duplicate=%s", credit.ID, duplicateCredit.ID)
	}
	debit := mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 5000, CurrencyID: "NGN", IdempotencyKey: "sql-audit-debit-001", Reference: "sql-audit-debit-ref"})
	internalTransfer := mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: account.ID, DestinationAccountID: destination.ID, AmountMinor: 7000, CurrencyID: "NGN", IdempotencyKey: "sql-audit-transfer-001", Reference: "sql-audit-transfer-ref"})

	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "sql-audit-freeze-ref", Reason: "Authorization: Bearer sql-secret-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "sql-audit-unfreeze-ref", Reason: "review clear"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "sql-audit-pnd-ref", Reason: "ops hold"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "sql-audit-pnd-off-ref", Reason: "ops clear"}); err != nil {
		t.Fatal(err)
	}
	lien := mustPlaceSQLLien(t, svc, ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 1000, CurrencyID: "NGN", Reference: "sql-audit-lien-ref", Reason: "ops lien"})
	if _, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, LienID: lien.ID, Reference: "sql-audit-lien-release-ref", Reason: "ops clear"}); err != nil {
		t.Fatal(err)
	}

	events, err := svc.repository.ListAuditEvents(ctx, DemoInstitutionID)
	if err != nil {
		t.Fatal(err)
	}
	assertAuditEventPresent(t, events, AuditActionCustomerCreated, func(event AuditEvent) bool {
		return auditString(event.CustomerID) != ""
	})
	assertAuditEventPresent(t, events, AuditActionAccountCreated, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID
	})
	assertPostedTransferAudit(t, events, AuditActionInternalCreditPosted, account.ID, credit)
	assertPostedTransferAudit(t, events, AuditActionInternalDebitPosted, account.ID, debit)
	assertPostedTransferAudit(t, events, AuditActionInternalTransferPosted, account.ID, internalTransfer)
	assertAuditEventPresent(t, events, AuditActionAccountFrozen, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && event.Metadata["reason"] == "[redacted]"
	})
	assertAuditEventPresent(t, events, AuditActionAccountUnfrozen, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.OldStatus) == AccountStatusFrozen && auditString(event.NewStatus) == AccountStatusActive
	})
	assertAuditEventPresent(t, events, AuditActionPNDActivated, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.NewStatus) == AccountStatusPostNoDebit
	})
	assertAuditEventPresent(t, events, AuditActionPNDDeactivated, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.OldStatus) == AccountStatusPostNoDebit
	})
	assertAuditEventPresent(t, events, AuditActionLienPlaced, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.Reference) == "sql-audit-lien-ref"
	})
	assertAuditEventPresent(t, events, AuditActionLienReleased, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.Reference) == "sql-audit-lien-release-ref"
	})
	if countAuditEvents(events, AuditActionInternalCreditPosted, func(event AuditEvent) bool {
		return auditString(event.IdempotencyKey) == creditInput.IdempotencyKey
	}) != 1 {
		t.Fatalf("duplicate credit replay should keep one audit row for idempotency key %s", creditInput.IdempotencyKey)
	}
	assertAuditMetadataSafe(t, events)

	otherTenantEvents, err := svc.repository.ListAuditEvents(ctx, "99999999-9999-9999-9999-999999999999")
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantEvents) != 0 {
		t.Fatalf("audit events leaked across tenants: %+v", otherTenantEvents)
	}
}

func TestSQLAuditFailureRollsBackMoneyAndAccountControl(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)
	account := createSQLCustomerAccount(t, svc, ctx, "SQLAudit", "Rollback", "sql.audit.rollback@example.com", "8734567892", "SQL Audit Rollback")

	if _, err := db.ExecContext(ctx, `
CREATE OR REPLACE FUNCTION fail_audit_events_for_test() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit insert failed for test';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER fail_audit_events_for_test
BEFORE INSERT ON audit_events
FOR EACH ROW EXECUTE FUNCTION fail_audit_events_for_test();`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TRIGGER IF EXISTS fail_audit_events_for_test ON audit_events; DROP FUNCTION IF EXISTS fail_audit_events_for_test();`)
	})

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-audit-fail-credit", Reference: "sql-audit-fail-credit-ref"}); err == nil {
		t.Fatal("expected audit insert failure to fail internal credit")
	}
	assertSQLBalanceForAccount(t, svc, ctx, account.ID, 0, 0)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-audit-fail-credit", 0)

	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "sql-audit-fail-freeze", Reason: "ops hold"}); err == nil {
		t.Fatal("expected audit insert failure to fail freeze")
	}
	assertSQLAccountStatus(t, ctx, db, account.ID, AccountStatusActive)
}
