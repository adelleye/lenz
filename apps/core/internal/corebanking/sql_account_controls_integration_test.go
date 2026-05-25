//go:build integration

package corebanking

import (
	"context"
	"errors"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestSQLAccountControlsGoal08(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := seededSQLService(t, db, ctx)

	lienAccount := createSQLCustomerAccount(t, svc, ctx, "SQL", "Lien", "sql.lien@example.com", "8234567890", "SQL Lien")
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: lienAccount.ID, AmountMinor: 50000, CurrencyID: "NGN", IdempotencyKey: "sql-control-lien-funding"})
	lien := mustPlaceSQLLien(t, svc, ctx, AccountLienInput{InstitutionID: DemoInstitutionID, AccountID: lienAccount.ID, AmountMinor: 15000, CurrencyID: "NGN", Reference: "sql-lien-ref", Reason: "ops lien"})
	assertSQLBalanceForAccount(t, svc, ctx, lienAccount.ID, 35000, 50000)
	assertSQLLienRow(t, ctx, db, lien.ID, HoldStatusActive, "sql-lien-ref")
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: lienAccount.ID, AmountMinor: 40000, CurrencyID: "NGN", IdempotencyKey: "sql-over-lien-debit"}); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected lien to reduce spendable balance, got %v", err)
	}
	released, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{InstitutionID: DemoInstitutionID, AccountID: lienAccount.ID, LienID: lien.ID, Reference: "sql-lien-release-ref"})
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != HoldStatusReleased || released.ReleasedAt == nil {
		t.Fatalf("released SQL lien mismatch: %+v", released)
	}
	assertSQLBalanceForAccount(t, svc, ctx, lienAccount.ID, 50000, 50000)
	assertSQLLienRow(t, ctx, db, lien.ID, HoldStatusReleased, "sql-lien-ref")
	mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: lienAccount.ID, AmountMinor: 40000, CurrencyID: "NGN", IdempotencyKey: "sql-post-lien-debit"})

	freezeAccount := createSQLCustomerAccount(t, svc, ctx, "SQL", "Freeze", "sql.freeze@example.com", "8234567891", "SQL Freeze")
	freezeDestination := createSQLCustomerAccount(t, svc, ctx, "SQL", "FreezeDestination", "sql.freeze.destination@example.com", "8234567894", "SQL Freeze Destination")
	freezeSender := createSQLCustomerAccount(t, svc, ctx, "SQL", "FreezeSender", "sql.freeze.sender@example.com", "8234567895", "SQL Freeze Sender")
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: freezeAccount.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-freeze-funding"})
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: freezeSender.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-freeze-sender-funding"})
	frozen, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: freezeAccount.ID, Reference: "sql-freeze-ref", Reason: "ops freeze"})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Status != AccountStatusFrozen {
		t.Fatalf("expected frozen SQL account, got %+v", frozen)
	}
	assertSQLAccountStatus(t, ctx, db, freezeAccount.ID, AccountStatusFrozen)
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: freezeAccount.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-frozen-credit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL credit to fail, got %v", err)
	}
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: freezeAccount.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-frozen-debit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL debit to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: freezeAccount.ID, DestinationAccountID: freezeDestination.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-frozen-transfer-out"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL transfer out to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: freezeSender.ID, DestinationAccountID: freezeAccount.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-frozen-transfer-in"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL transfer in to fail, got %v", err)
	}
	active, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: freezeAccount.ID, Reference: "sql-unfreeze-ref", Reason: "ops clear"})
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active SQL account, got %+v", active)
	}

	pndSource := createSQLCustomerAccount(t, svc, ctx, "SQL", "PNDSource", "sql.pnd.source@example.com", "8234567892", "SQL PND Source")
	pndDestination := createSQLCustomerAccount(t, svc, ctx, "SQL", "PNDDestination", "sql.pnd.destination@example.com", "8234567893", "SQL PND Destination")
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-funding"})
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: DemoCustomerAccountID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-transfer-funding"})
	pnd, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, Reference: "sql-pnd-ref", Reason: "ops pnd"})
	if err != nil {
		t.Fatal(err)
	}
	if pnd.Status != AccountStatusPostNoDebit {
		t.Fatalf("expected SQL PND account, got %+v", pnd)
	}
	assertSQLAccountStatus(t, ctx, db, pndSource.ID, AccountStatusPostNoDebit)
	if _, err := svc.InternalDebit(ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-debit"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected SQL PND debit to fail, got %v", err)
	}
	if _, err := svc.InternalTransfer(ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: pndSource.ID, DestinationAccountID: pndDestination.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-transfer-out"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected SQL PND transfer out to fail, got %v", err)
	}
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-credit-in"})
	mustInternalTransfer(t, svc, ctx, InternalTransferInput{InstitutionID: DemoInstitutionID, SourceAccountID: DemoCustomerAccountID, DestinationAccountID: pndSource.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-transfer-in"})
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, Reference: "sql-pnd-off-ref", Reason: "ops clear"}); err != nil {
		t.Fatal(err)
	}
	mustInternalDebit(t, svc, ctx, InternalDebitInput{InstitutionID: DemoInstitutionID, AccountID: pndSource.ID, AmountMinor: 1000, CurrencyID: "NGN", IdempotencyKey: "sql-pnd-removed-debit"})

	transitionAccount := createSQLCustomerAccount(t, svc, ctx, "SQL", "Transitions", "sql.transitions@example.com", "8234567896", "SQL Transitions")
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-active-unfreeze", Reason: "not frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected active SQL account unfreeze to fail, got %v", err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-active-pnd-off", Reason: "not pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected active SQL account PND deactivation to fail, got %v", err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-transition-pnd", Reason: "ops pnd"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-transition-pnd-again", Reason: "already pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected repeated SQL PND activation to fail, got %v", err)
	}
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-pnd-unfreeze", Reason: "not frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected SQL PND account unfreeze to fail, got %v", err)
	}
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-transition-freeze", Reason: "security escalation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-transition-freeze-again", Reason: "already frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected repeated SQL freeze to fail, got %v", err)
	}
	if _, err := svc.ActivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-frozen-pnd-on", Reason: "frozen"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL account PND activation to fail, got %v", err)
	}
	if _, err := svc.DeactivatePostNoDebit(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-frozen-pnd-off", Reason: "not pnd"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected frozen SQL account PND deactivation to fail, got %v", err)
	}
	if _, err := svc.UnfreezeAccount(ctx, AccountControlInput{InstitutionID: DemoInstitutionID, AccountID: transitionAccount.ID, Reference: "sql-transition-unfreeze", Reason: "review clear"}); err != nil {
		t.Fatal(err)
	}
	assertSQLAccountStatus(t, ctx, db, transitionAccount.ID, AccountStatusActive)

	if _, err := svc.ReleaseAccountLien(ctx, ReleaseLienInput{InstitutionID: "99999999-9999-9999-9999-999999999999", AccountID: lienAccount.ID, LienID: lien.ID, Reference: "cross-release"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant lien release to fail, got %v", err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func mustPlaceSQLLien(t *testing.T, svc *Service, ctx context.Context, input AccountLienInput) *AccountHold {
	t.Helper()
	hold, err := svc.PlaceAccountLien(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return hold
}

func assertSQLBalanceForAccount(t *testing.T, svc *Service, ctx context.Context, accountID string, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch for %s: got %+v want available=%d ledger=%d", accountID, balance, wantAvailable, wantLedger)
	}
}

func assertSQLAccountStatus(t *testing.T, ctx context.Context, db *sqlx.DB, accountID, want string) {
	t.Helper()
	var status string
	if err := db.GetContext(ctx, &status, `SELECT status FROM accounts WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, accountID); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("SQL account status mismatch: got %s want %s", status, want)
	}
}

func assertSQLLienRow(t *testing.T, ctx context.Context, db *sqlx.DB, lienID, wantStatus, wantReference string) {
	t.Helper()
	var row struct {
		Status    string  `db:"status"`
		Reference string  `db:"reference"`
		Transfer  *string `db:"transfer_id"`
	}
	if err := db.GetContext(ctx, &row, `SELECT status, reference, transfer_id FROM account_holds WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, lienID); err != nil {
		t.Fatal(err)
	}
	if row.Status != wantStatus || row.Reference != wantReference || row.Transfer != nil {
		t.Fatalf("SQL lien row mismatch: %+v", row)
	}
}
