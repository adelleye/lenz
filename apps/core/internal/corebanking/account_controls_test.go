package corebanking

import (
	"context"
	"errors"
	"testing"
)

func TestAccountFreezeBlocksMoneyMovementAndUnfreezeRestores(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	source := createMemoryCustomerAccount(t, svc, ctx, "Freeze", "Source", "freeze.source@example.com", uniqueAccountNumber("80"))
	destination := createMemoryCustomerAccount(t, svc, ctx, "Freeze", "Destination", "freeze.destination@example.com", uniqueAccountNumber("81"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "freeze-funding"})

	frozen, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, Reference: "freeze-ref", Reason: "suspected compromise"})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Status != AccountStatusFrozen {
		t.Fatalf("expected frozen status, got %+v", frozen)
	}
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "frozen-credit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account credit to fail, got %v", err)
	}
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "frozen-debit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account debit to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: source.ID, DestinationAccountID: destination.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "frozen-transfer-out"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account transfer out to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "frozen-transfer-in"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account transfer in to fail, got %v", err)
	}

	active, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, Reference: "unfreeze-ref", Reason: "review cleared"})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active status, got %+v", active)
	}
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "unfrozen-credit"})
	mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "unfrozen-debit"})
	mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: source.ID, DestinationAccountID: destination.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "unfrozen-transfer-out"})
	mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: destination.ID, DestinationAccountID: source.ID, AmountMinor: 500, CurrencyID: "NGN", IdempotencyKey: "unfrozen-transfer-in"})
}

func TestPostNoDebitBlocksOutflowsAndAllowsInflows(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	source := createMemoryCustomerAccount(t, svc, ctx, "PND", "Source", "pnd.source@example.com", uniqueAccountNumber("82"))
	destination := createMemoryCustomerAccount(t, svc, ctx, "PND", "Destination", "pnd.destination@example.com", uniqueAccountNumber("83"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "pnd-funding"})
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "pnd-source-funding"})

	pnd, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, Reference: "pnd-ref", Reason: "ops hold"})
	if err != nil {
		t.Fatal(err)
	}
	if pnd.Status != AccountStatusPostNoDebit {
		t.Fatalf("expected PND status, got %+v", pnd)
	}
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "pnd-debit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected PND debit to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: source.ID, DestinationAccountID: destination.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "pnd-transfer-out"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected PND transfer out to fail, got %v", err)
	}
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "pnd-credit-in"})
	mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "pnd-transfer-in"})

	active, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, Reference: "pnd-off-ref", Reason: "ops cleared"})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active status, got %+v", active)
	}
	mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: source.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "pnd-removed-debit"})
}

func TestAccountControlStateTransitionsAreStrict(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "Control", "Transitions", "control.transitions@example.com", uniqueAccountNumber("86"))

	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "active-unfreeze", Reason: "not frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected active account unfreeze to fail, got %v", err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "active-pnd-off", Reason: "not pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected active account PND deactivation to fail, got %v", err)
	}

	pnd, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "activate-pnd", Reason: "ops hold"})
	if err != nil {
		t.Fatal(err)
	}
	if pnd.Status != AccountStatusPostNoDebit {
		t.Fatalf("expected PND status, got %+v", pnd)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "activate-pnd-again", Reason: "already pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected repeated PND activation to fail, got %v", err)
	}
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "pnd-unfreeze", Reason: "not frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected PND account unfreeze to fail, got %v", err)
	}
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "freeze-pnd", Reason: "security escalation"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected PND account freeze to fail, got %v", err)
	}

	activeFromPND, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "deactivate-pnd-before-freeze", Reason: "ops clear"})
	if err != nil {
		t.Fatal(err)
	}
	if activeFromPND.Status != AccountStatusActive {
		t.Fatalf("expected active status after PND deactivation, got %+v", activeFromPND)
	}

	frozen, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "freeze-active", Reason: "security escalation"})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Status != AccountStatusFrozen {
		t.Fatalf("expected frozen status, got %+v", frozen)
	}
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "freeze-again", Reason: "already frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected repeated freeze to fail, got %v", err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "frozen-pnd-on", Reason: "frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account PND activation to fail, got %v", err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "frozen-pnd-off", Reason: "not pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen account PND deactivation to fail, got %v", err)
	}

	active, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, Reference: "unfreeze-frozen", Reason: "review clear"})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active status, got %+v", active)
	}
}

func TestAccountLienReducesAvailableAndReleaseRestores(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "Lien", "Holder", "lien.holder@example.com", uniqueAccountNumber("84"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 30000, CurrencyID: "NGN", IdempotencyKey: "lien-funding"})

	lien, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 12000, CurrencyID: "NGN", Reference: "lien-ref", Reason: "ops lien"})
	if err != nil {
		t.Fatal(err)
	}
	if lien.Status != HoldStatusActive || lien.TransferID != nil || lien.Reference != "lien-ref" {
		t.Fatalf("lien row mismatch: %+v", lien)
	}
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != 18000 || balance.LedgerMinor != 30000 {
		t.Fatalf("lien balance mismatch: %+v", balance)
	}
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "over-lien-debit"}); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected debit above available to fail, got %v", err)
	}
	if _, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 19000, CurrencyID: "NGN", Reference: "over-lien-ref", Reason: "too much"}); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected over-lien to fail, got %v", err)
	}

	released, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, LienID: lien.ID, Reference: "lien-release-ref"})
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != HoldStatusReleased || released.ReleasedAt == nil {
		t.Fatalf("released lien mismatch: %+v", released)
	}
	balance, err = svc.GetBalance(ctx, DemoInstitutionID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != 30000 || balance.LedgerMinor != 30000 {
		t.Fatalf("released lien balance mismatch: %+v", balance)
	}
	mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "post-lien-debit"})
}

func TestAccountLienReplayRequiresSamePayload(t *testing.T) {
	ctx, svc, store := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "LienReplay", "Holder", "lien.replay@example.com", uniqueAccountNumber("87"))
	otherAccount := createMemoryCustomerAccount(t, svc, ctx, "LienReplay", "Other", "lien.replay.other@example.com", uniqueAccountNumber("88"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 40000, CurrencyID: "NGN", IdempotencyKey: "lien-replay-funding"})
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: otherAccount.ID, AmountMinor: 25000, CurrencyID: "NGN", IdempotencyKey: "lien-replay-other-funding"})

	input := AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 12000, CurrencyID: "NGN", Reference: "lien-replay-ref", Reason: "ops lien"}
	lien, err := svc.PlaceAccountLien(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 12000, CurrencyID: "NGN", Reference: "lien-replay-ref", Reason: "ops lien replay"})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != lien.ID {
		t.Fatalf("expected same-payload replay to return lien %s, got %s", lien.ID, replay.ID)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, account.ID, 28000, 40000)
	assertBalancePair(t, svc, ctx, DemoInstitutionID, otherAccount.ID, 25000, 25000)
	assertMemoryActiveLienReferenceCount(t, store, "lien-replay-ref", 1)
	assertMemoryLienAuditReferenceCount(t, store, "lien-replay-ref", 1)

	conflicts := []struct {
		name  string
		input AccountLienInput
	}{
		{
			name:  "different_amount",
			input: AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 13000, CurrencyID: "NGN", Reference: "lien-replay-ref", Reason: "changed amount"},
		},
		{
			name:  "different_currency",
			input: AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 12000, CurrencyID: "USD", Reference: "lien-replay-ref", Reason: "changed currency"},
		},
		{
			name:  "different_account",
			input: AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: otherAccount.ID, AmountMinor: 12000, CurrencyID: "NGN", Reference: "lien-replay-ref", Reason: "changed account"},
		},
	}
	for _, tt := range conflicts {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.PlaceAccountLien(ctx, tt.input); !errors.Is(err, ErrConflict) {
				t.Fatalf("expected replay mismatch to fail with conflict, got %v", err)
			}
			assertBalancePair(t, svc, ctx, DemoInstitutionID, account.ID, 28000, 40000)
			assertBalancePair(t, svc, ctx, DemoInstitutionID, otherAccount.ID, 25000, 25000)
			assertMemoryActiveLienReferenceCount(t, store, "lien-replay-ref", 1)
			assertMemoryLienAuditReferenceCount(t, store, "lien-replay-ref", 1)
		})
	}
}

func TestAccountControlsRejectInvalidAndCrossTenantInputs(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, Reason: "missing reference"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing reference to fail, got %v", err)
	}
	if _, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 0, CurrencyID: "NGN", Reference: "bad-lien", Reason: "zero"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid lien amount to fail, got %v", err)
	}
	if _, err := svc.PlaceAccountLien(ctx, AccountLienInput{InstitutionID: "99999999-9999-9999-9999-999999999999", AccountID: DemoCustomerAccountID, AmountMinor: 1000, CurrencyID: "NGN", Reference: "cross-lien", Reason: "cross"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant lien to fail, got %v", err)
	}
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: "99999999-9999-9999-9999-999999999999", AccountID: DemoCustomerAccountID, Reference: "cross-freeze", Reason: "cross"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant freeze to fail, got %v", err)
	}
}

func assertMemoryActiveLienReferenceCount(t *testing.T, store *memoryStore, reference string, want int) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()

	count := 0
	for _, hold := range store.holds {
		if hold.InstitutionID == DemoInstitutionID && hold.Reference == reference && hold.TransferID == nil && hold.Status == HoldStatusActive {
			count++
		}
	}
	if count != want {
		t.Fatalf("active lien count for reference %q = %d, want %d", reference, count, want)
	}
}

func assertMemoryLienAuditReferenceCount(t *testing.T, store *memoryStore, reference string, want int) {
	t.Helper()
	events, err := store.ListAuditEvents(context.Background(), DemoInstitutionID, ListAuditEventsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	count := countAuditEvents(events, AuditActionLienPlaced, func(event AuditEvent) bool {
		return event.Reference != nil && *event.Reference == reference
	})
	if count != want {
		t.Fatalf("lien audit count for reference %q = %d, want %d", reference, count, want)
	}
}
