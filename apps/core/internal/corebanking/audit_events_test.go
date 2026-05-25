package corebanking

import (
	"strings"
	"testing"
)

func TestAuditEventsGoal09MemoryStore(t *testing.T) {
	ctx, svc, store := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "Audit", "Source", "audit.source@example.com", uniqueAccountNumber("85"))
	destination := createMemoryCustomerAccount(t, svc, ctx, "Audit", "Dest", "audit.dest@example.com", uniqueAccountNumber("86"))

	creditInput := InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 50000, CurrencyID: "NGN", IdempotencyKey: "audit-credit-001", Reference: "audit-credit-ref"}
	credit := mustInternalCredit(t, svc, ctx, creditInput)
	duplicateCredit := mustInternalCredit(t, svc, ctx, creditInput)
	if duplicateCredit.ID != credit.ID {
		t.Fatalf("duplicate credit idempotency created a new transfer: first=%s duplicate=%s", credit.ID, duplicateCredit.ID)
	}
	debit := mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 5000, CurrencyID: "NGN", IdempotencyKey: "audit-debit-001", Reference: "audit-debit-ref"})
	internalTransfer := mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: account.ID, DestinationAccountID: destination.ID, AmountMinor: 7000, CurrencyID: "NGN", IdempotencyKey: "audit-transfer-001", Reference: "audit-transfer-ref"})

	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "audit-freeze-ref", Reason: "Authorization: Bearer secret-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "audit-unfreeze-ref", Reason: "review clear"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "audit-pnd-ref", Reason: "ops hold"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "audit-pnd-off-ref", Reason: "ops clear"}); err != nil {
		t.Fatal(err)
	}
	lien, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 1000, CurrencyID: "NGN", Reference: "audit-lien-ref", Reason: "ops lien"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, LienID: lien.ID, Reference: "audit-lien-release-ref", Reason: "ops clear"}); err != nil {
		t.Fatal(err)
	}

	events, err := store.ListAuditEvents(ctx, DemoInstitutionID)
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
		return auditString(event.AccountID) == account.ID && auditString(event.Reference) == "audit-lien-ref"
	})
	assertAuditEventPresent(t, events, AuditActionLienReleased, func(event AuditEvent) bool {
		return auditString(event.AccountID) == account.ID && auditString(event.Reference) == "audit-lien-release-ref"
	})
	if countAuditEvents(events, AuditActionInternalCreditPosted, func(event AuditEvent) bool {
		return auditString(event.IdempotencyKey) == creditInput.IdempotencyKey
	}) != 1 {
		t.Fatalf("duplicate credit replay should keep one audit row for idempotency key %s", creditInput.IdempotencyKey)
	}
	assertAuditMetadataSafe(t, events)

	otherTenantEvents, err := store.ListAuditEvents(ctx, "99999999-9999-9999-9999-999999999999")
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantEvents) != 0 {
		t.Fatalf("audit events leaked across tenants: %+v", otherTenantEvents)
	}
}

func assertPostedTransferAudit(t *testing.T, events []AuditEvent, action, accountID string, transfer *Transfer) {
	t.Helper()
	assertAuditEventPresent(t, events, action, func(event AuditEvent) bool {
		return auditString(event.AccountID) == accountID &&
			auditString(event.TransferID) == transfer.ID &&
			transfer.JournalEntryID != nil &&
			auditString(event.JournalEntryID) == *transfer.JournalEntryID
	})
}

func assertAuditEventPresent(t *testing.T, events []AuditEvent, action string, match func(AuditEvent) bool) AuditEvent {
	t.Helper()
	for _, event := range events {
		if event.Action == action && event.InstitutionID == DemoInstitutionID && match(event) {
			if event.RequestID == "" || event.EntityType == "" || event.EntityID == "" {
				t.Fatalf("audit event missing durable identity fields: %+v", event)
			}
			return event
		}
	}
	t.Fatalf("missing audit action %s in events %+v", action, events)
	return AuditEvent{}
}

func countAuditEvents(events []AuditEvent, action string, match func(AuditEvent) bool) int {
	count := 0
	for _, event := range events {
		if event.Action == action && match(event) {
			count++
		}
	}
	return count
}

func assertAuditMetadataSafe(t *testing.T, events []AuditEvent) {
	t.Helper()
	for _, event := range events {
		for key, value := range event.Metadata {
			if unsafeAuditMetadataKey(key) || containsSensitiveAuditText(value) {
				t.Fatalf("unsafe audit metadata persisted for action %s: %s=%q", event.Action, key, value)
			}
		}
	}
}

func containsSensitiveAuditText(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "authorization") ||
		strings.Contains(value, "bearer ") ||
		strings.Contains(value, "token") ||
		strings.Contains(value, "secret") ||
		strings.Contains(value, "password") ||
		strings.Contains(value, "bvn") ||
		strings.Contains(value, "nin")
}

func auditString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
