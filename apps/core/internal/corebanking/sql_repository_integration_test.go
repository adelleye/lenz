//go:build integration

package corebanking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
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

func TestSQLRepositoryCustomerCreateGetIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}

	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Adaeze",
		LastName:      "Okafor",
		Email:         "adaeze.sql@example.com",
		Phone:         "+2348012345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	if customer.ID == "" || customer.InstitutionID != DemoInstitutionID || customer.BranchID != DemoBranchID || customer.CustomerType != CustomerTypeIndividual || customer.Status != "active" {
		t.Fatalf("created customer has wrong scope/data: %+v", customer)
	}
	if customer.KYCTier != CustomerKYCTier1 || customer.BVNStatus != CustomerIdentityStatusNotCollected || customer.NINStatus != CustomerIdentityStatusNotCollected {
		t.Fatalf("created customer has wrong identity defaults: %+v", customer)
	}

	got, err := svc.GetCustomer(ctx, DemoInstitutionID, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != customer.ID || got.Email != customer.Email {
		t.Fatalf("get customer mismatch: got %+v want %+v", got, customer)
	}

	var row Customer
	if err := db.GetContext(ctx, &row, customerSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, customer.ID); err != nil {
		t.Fatal(err)
	}
	if row.ID != customer.ID || row.CustomerType != CustomerTypeIndividual || row.FirstName != "Adaeze" || row.Phone != "+2348012345678" || row.KYCTier != CustomerKYCTier1 || row.BVNStatus != CustomerIdentityStatusNotCollected || row.NINStatus != CustomerIdentityStatusNotCollected {
		t.Fatalf("customer row was not created correctly: %+v", row)
	}

	var meta struct {
		CustomerType string `db:"customer_type"`
		KYCTier      string `db:"kyc_tier"`
		BVNStatus    string `db:"bvn_status"`
		NINStatus    string `db:"nin_status"`
	}
	if err := db.GetContext(ctx, &meta, `
SELECT
	meta->>'customer_type' AS customer_type,
	meta->>'kyc_tier' AS kyc_tier,
	meta->>'bvn_status' AS bvn_status,
	meta->>'nin_status' AS nin_status
FROM customers
WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, customer.ID); err != nil {
		t.Fatal(err)
	}
	if meta.CustomerType != CustomerTypeIndividual || meta.KYCTier != CustomerKYCTier1 || meta.BVNStatus != CustomerIdentityStatusNotCollected || meta.NINStatus != CustomerIdentityStatusNotCollected {
		t.Fatalf("customer metadata was not stored correctly: %+v", meta)
	}

	if _, err := svc.GetCustomer(ctx, "99999999-9999-9999-9999-999999999999", customer.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-institution customer read to fail as not found, got %v", err)
	}
	if _, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      "99999999-9999-9999-9999-999999999999",
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Ada",
		LastName:      "Missing",
		Email:         "ada.missing@example.com",
		Phone:         "+2348012340000",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing branch to fail as not found, got %v", err)
	}
}

func TestSQLRepositoryAccountCreateGetListIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Account",
		LastName:      "Owner",
		Email:         "account.owner@example.com",
		Phone:         "+2348012345679",
	})
	if err != nil {
		t.Fatal(err)
	}
	emptyAccounts, err := svc.ListCustomerAccounts(ctx, DemoInstitutionID, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if emptyAccounts == nil || len(emptyAccounts) != 0 {
		t.Fatalf("expected new customer to have empty account list, got %+v", emptyAccounts)
	}

	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567890",
		Name:          "Account Owner Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.ID == "" || account.InstitutionID != DemoInstitutionID || account.CustomerID == nil || *account.CustomerID != customer.ID || account.AccountNumber != "1234567890" {
		t.Fatalf("created account has wrong scope/data: %+v", account)
	}
	if account.Kind != AccountKindCustomer || account.ProductType != AccountProductStandardWallet || account.AllowNegative || account.CurrencyID != "NGN" || account.NormalBalance != NormalBalanceCredit || account.Status != "active" {
		t.Fatalf("created account has wrong defaults: %+v", account)
	}

	var row Account
	if err := db.GetContext(ctx, &row, accountSelectSQL+` WHERE institution_id = $1 AND id = $2`, DemoInstitutionID, account.ID); err != nil {
		t.Fatal(err)
	}
	if row.ID != account.ID || row.CustomerID == nil || *row.CustomerID != customer.ID || row.AccountNumber != "1234567890" || row.AllowNegative {
		t.Fatalf("account row mismatch: %+v", row)
	}

	var balance AccountBalance
	if err := db.GetContext(ctx, &balance, `SELECT account_id, institution_id, available_minor, ledger_minor, currency_id, last_journal_entry_id, updated_at FROM account_balances WHERE institution_id = $1 AND account_id = $2`, DemoInstitutionID, account.ID); err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != 0 || balance.LedgerMinor != 0 || balance.CurrencyID != "NGN" || balance.LastJournalEntryID != nil {
		t.Fatalf("initial account balance mismatch: %+v", balance)
	}

	got, err := svc.GetAccount(ctx, DemoInstitutionID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != account.ID || got.AccountNumber != account.AccountNumber {
		t.Fatalf("get account mismatch: got %+v want %+v", got, account)
	}

	accounts, err := svc.ListCustomerAccounts(ctx, DemoInstitutionID, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("expected customer account list to include created account, got %+v", accounts)
	}

	if _, err := svc.GetAccount(ctx, "99999999-9999-9999-9999-999999999999", account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-institution account read to fail as not found, got %v", err)
	}
	crossAccounts, err := svc.ListCustomerAccounts(ctx, "99999999-9999-9999-9999-999999999999", customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if crossAccounts == nil || len(crossAccounts) != 0 {
		t.Fatalf("expected cross-institution account list to be empty, got %+v", crossAccounts)
	}

	_, err = svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567890",
		Name:          "Duplicate Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate account number to return conflict, got %v", err)
	}

	_, err = svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    "99999999-9999-9999-9999-999999999999",
		AccountNumber: "1234567891",
		Name:          "Missing Customer Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing customer account create to fail as not found, got %v", err)
	}
	var orphanAccounts int
	if err := db.GetContext(ctx, &orphanAccounts, `SELECT COUNT(*) FROM accounts WHERE institution_id = $1 AND account_number = $2`, DemoInstitutionID, "1234567891"); err != nil {
		t.Fatal(err)
	}
	if orphanAccounts != 0 {
		t.Fatalf("failed account create should not leave account rows, found %d", orphanAccounts)
	}
}

func TestSQLRepositoryBalanceEnquiryIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Balance",
		LastName:      "Owner",
		Email:         "balance.owner@example.com",
		Phone:         "+2348012345681",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567894",
		Name:          "Balance Owner Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 0, 0)
	if _, err := svc.GetBalance(ctx, DemoInstitutionID, "99999999-9999-9999-9999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing account balance read to fail as not found, got %v", err)
	}
	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", account.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-institution balance read to fail as not found, got %v", err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	mockInbound(t, svc, ctx, TransferRequest{
		AccountID:         account.ID,
		AmountMinor:       50000,
		IdempotencyKey:    "sql-balance-in-001",
		ProviderEventID:   "sql-balance-provider-event-001",
		ProviderReference: "sql-balance-provider-ref-001",
		Narration:         "SQL balance funding",
	})
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 50000, 50000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	pendingToFail := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         account.ID,
		AmountMinor:       20000,
		IdempotencyKey:    "sql-balance-out-pending-fail",
		ProviderReference: "sql-balance-out-pending-fail-ref",
		Status:            TransferStatusPending,
		Narration:         "SQL balance pending fail",
	})
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 30000, 50000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	failed := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         account.ID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusFailed,
		AmountMinor:       20000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "sql-balance-out-pending-fail-settle",
		ProviderReference: "sql-balance-out-pending-fail-ref",
		ProviderEventID:   "sql-balance-provider-event-fail-settle",
		FailureReason:     "provider_failed",
		Narration:         "SQL balance failed settlement",
	})
	if failed.ID != pendingToFail.ID {
		t.Fatalf("failed settlement should update pending transfer: pending=%s failed=%s", pendingToFail.ID, failed.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 50000, 50000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	pendingToSucceed := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:         account.ID,
		AmountMinor:       15000,
		IdempotencyKey:    "sql-balance-out-pending-success",
		ProviderReference: "sql-balance-out-pending-success-ref",
		Status:            TransferStatusPending,
		Narration:         "SQL balance pending success",
	})
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 35000, 50000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	succeeded := mockProviderEvent(t, svc, ctx, ProviderWebhookEvent{
		InstitutionID:     DemoInstitutionID,
		AccountID:         account.ID,
		Direction:         TransferDirectionOutbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       15000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "sql-balance-out-pending-success-settle",
		ProviderReference: "sql-balance-out-pending-success-ref",
		ProviderEventID:   "sql-balance-provider-event-success-settle",
		Narration:         "SQL balance successful settlement",
	})
	if succeeded.ID != pendingToSucceed.ID {
		t.Fatalf("successful settlement should update pending transfer: pending=%s succeeded=%s", pendingToSucceed.ID, succeeded.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 35000, 35000)
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM account_balances WHERE institution_id = $1 AND account_id = $2`, DemoInstitutionID, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetBalance(ctx, DemoInstitutionID, account.ID); !errors.Is(err, ErrDataIntegrity) {
		t.Fatalf("expected missing balance row to return data integrity error, got %v", err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err == nil {
		t.Fatal("expected reconciliation helper to catch missing account balance row")
	}
}

func TestSQLRepositoryInternalCreditIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Internal",
		LastName:      "Credit",
		Email:         "internal.credit@example.com",
		Phone:         "+2348012345682",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567895",
		Name:          "Internal Credit Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}

	credit, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    25000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-credit-001",
		Reference:      "sql-internal-credit-ref-001",
		Narration:      "SQL internal credit proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, credit, TransferStatusSucceeded)
	if credit.Direction != TransferDirectionInbound || credit.Provider != ProviderLedgerInternal || credit.ProviderReference != "sql-internal-credit-ref-001" || credit.JournalEntryID == nil {
		t.Fatalf("internal credit transfer mismatch: %+v", credit)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 25000, 25000)
	assertSQLJournalBalanced(t, svc, ctx, credit, 25000)
	assertSQLInternalCreditRows(t, ctx, db, credit, account.ID, DemoClearingAccountID, 25000)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].TransferID != credit.ID || history[0].SignedMinor != 25000 || history[0].JournalEntryID == nil {
		t.Fatalf("internal credit history mismatch: %+v", history)
	}

	duplicate, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    25000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-credit-001",
		Reference:      "sql-internal-credit-ref-001",
		Narration:      "SQL internal credit duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != credit.ID {
		t.Fatalf("duplicate idempotency key posted a new internal credit: first=%s duplicate=%s", credit.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-credit-001", 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalCreditConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Concurrent",
		LastName:      "Credit",
		Email:         "concurrent.credit@example.com",
		Phone:         "+2348012345683",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: "1234567896",
		Name:          "Concurrent Credit Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}

	const amount = int64(30000)
	const idempotencyKey = "sql-internal-credit-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalCredit(ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: idempotencyKey,
			Reference:      "sql-internal-credit-concurrent-ref",
			Narration:      fmt.Sprintf("SQL concurrent internal credit %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, amount, amount)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, amount, amount)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalCreditRows(t, ctx, db, transfer, account.ID, DemoClearingAccountID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Internal", "Debit", "internal.debit@example.com", "1234567897", "Internal Debit Wallet")

	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    40000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-fund",
		Narration:      "SQL internal debit funding",
	}); err != nil {
		t.Fatal(err)
	}

	debit, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    15000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-001",
		Reference:      "sql-internal-debit-ref-001",
		Narration:      "SQL internal debit proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, debit, TransferStatusSucceeded)
	if debit.Direction != TransferDirectionOutbound || debit.Provider != ProviderLedgerInternal || debit.ProviderReference != "sql-internal-debit-ref-001" || debit.JournalEntryID == nil {
		t.Fatalf("internal debit transfer mismatch: %+v", debit)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 25000, 25000)
	assertSQLJournalBalanced(t, svc, ctx, debit, 15000)
	assertSQLInternalDebitRows(t, ctx, db, debit, account.ID, DemoClearingAccountID, 15000)

	history, err := svc.GetTransactions(ctx, DemoInstitutionID, account.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seenDebit := false
	for _, txn := range history {
		if txn.TransferID == debit.ID && txn.SignedMinor == -15000 && txn.JournalEntryID != nil {
			seenDebit = true
		}
	}
	if !seenDebit {
		t.Fatalf("internal debit history mismatch: %+v", history)
	}

	duplicate, err := svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    15000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-001",
		Reference:      "sql-internal-debit-ref-001",
		Narration:      "SQL internal debit duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != debit.ID {
		t.Fatalf("duplicate idempotency key posted a new internal debit: first=%s duplicate=%s", debit.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-debit-001", 1)

	_, err = svc.InternalDebit(ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    99999,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-insufficient",
		Narration:      "SQL internal debit insufficient",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 25000, 25000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-debit-insufficient", 0)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "Debit", "concurrent.debit@example.com", "1234567898", "Concurrent Debit Wallet")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-concurrent-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(10000)
	const idempotencyKey = "sql-internal-debit-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalDebit(ctx, InternalDebitInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: idempotencyKey,
			Reference:      "sql-internal-debit-concurrent-ref",
			Narration:      fmt.Sprintf("SQL concurrent internal debit %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 20000, 20000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 20000, 20000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalDebitRows(t, ctx, db, transfer, account.ID, DemoClearingAccountID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalDebitConcurrentDistinctNoOverspend(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	account := createSQLCustomerAccount(t, svc, ctx, "Distinct", "Debit", "distinct.debit@example.com", "1234567899", "Distinct Debit Wallet")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-debit-distinct-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(7000)
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalDebit(ctx, InternalDebitInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      account.ID,
			AmountMinor:    amount,
			CurrencyID:     "NGN",
			IdempotencyKey: fmt.Sprintf("sql-internal-debit-distinct-attempt-%02d", i),
			Narration:      fmt.Sprintf("SQL distinct internal debit %02d", i),
		})
	})

	successes := 0
	insufficient := 0
	for i, result := range results {
		switch {
		case result.err == nil:
			successes++
			assertStatus(t, result.transfer, TransferStatusSucceeded)
			assertSQLJournalBalanced(t, svc, ctx, result.transfer, amount)
		case errors.Is(result.err, ErrInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected distinct debit result %d: transfer=%+v err=%v", i, result.transfer, result.err)
		}
	}
	if successes != 4 || insufficient != 6 {
		t.Fatalf("expected 4 successful debits and 6 insufficient rejections, got successes=%d insufficient=%d", successes, insufficient)
	}
	assertSQLAccountBalancePair(t, svc, ctx, account.ID, 2000, 2000)
	assertSQLAccountBalancePair(t, svc, ctx, DemoClearingAccountID, 2000, 2000)
	assertSQLTransferCountByIdempotencyPrefix(t, ctx, db, "sql-internal-debit-distinct-attempt-", 4)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalTransferIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Internal", "TransferSource", "internal.transfer.source@example.com", "2234567890", "Internal Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Internal", "TransferDestination", "internal.transfer.destination@example.com", "2234567891", "Internal Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    40000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-fund",
		Narration:      "SQL internal transfer funding",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          15000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-001",
		Reference:            "sql-internal-transfer-ref-001",
		Narration:            "SQL internal transfer proof",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, transfer, TransferStatusSucceeded)
	if transfer.AccountID != source.ID || transfer.Direction != TransferDirectionOutbound || transfer.Provider != ProviderLedgerInternal || transfer.ProviderReference != "sql-internal-transfer-ref-001" || transfer.JournalEntryID == nil {
		t.Fatalf("internal transfer mismatch: %+v", transfer)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, 15000)
	assertSQLInternalTransferRows(t, ctx, db, transfer, source.ID, destination.ID, 15000)

	sourceHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, source.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceHistory) != 2 || sourceHistory[0].TransferID != transfer.ID || sourceHistory[0].SignedMinor != -15000 || sourceHistory[0].Direction != TransferDirectionOutbound {
		t.Fatalf("source history mismatch: %+v", sourceHistory)
	}
	destinationHistory, err := svc.GetTransactions(ctx, DemoInstitutionID, destination.ID, ListTransactionsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(destinationHistory) != 1 || destinationHistory[0].TransferID != transfer.ID || destinationHistory[0].SignedMinor != 15000 || destinationHistory[0].Direction != TransferDirectionInbound {
		t.Fatalf("destination history mismatch: %+v", destinationHistory)
	}

	duplicate, err := svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          15000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-001",
		Reference:            "sql-internal-transfer-ref-001",
		Narration:            "SQL internal transfer duplicate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != transfer.ID {
		t.Fatalf("duplicate idempotency key posted a new internal transfer: first=%s duplicate=%s", transfer.ID, duplicate.ID)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-transfer-001", 1)

	_, err = svc.InternalTransfer(ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      source.ID,
		DestinationAccountID: destination.ID,
		AmountMinor:          99999,
		CurrencyID:           "NGN",
		IdempotencyKey:       "sql-internal-transfer-insufficient",
		Narration:            "SQL internal transfer insufficient",
	})
	if !errors.Is(err, ErrInsufficient) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 25000, 25000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 15000, 15000)
	assertSQLTransferCountByIdempotency(t, ctx, db, "sql-internal-transfer-insufficient", 0)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalTransferConcurrentIdempotency(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "TransferSource", "concurrent.transfer.source@example.com", "2234567892", "Concurrent Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Concurrent", "TransferDestination", "concurrent.transfer.destination@example.com", "2234567893", "Concurrent Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-concurrent-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(10000)
	const idempotencyKey = "sql-internal-transfer-concurrent"
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalTransfer(ctx, InternalTransferInput{
			InstitutionID:        DemoInstitutionID,
			SourceAccountID:      source.ID,
			DestinationAccountID: destination.ID,
			AmountMinor:          amount,
			CurrencyID:           "NGN",
			IdempotencyKey:       idempotencyKey,
			Reference:            "sql-internal-transfer-concurrent-ref",
			Narration:            fmt.Sprintf("SQL concurrent internal transfer %02d", i),
		})
	})

	transfer := assertConcurrentReplay(t, results)
	assertStatus(t, transfer, TransferStatusSucceeded)
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 20000, 20000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 10000, 10000)
	assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
	assertSQLInternalTransferRows(t, ctx, db, transfer, source.ID, destination.ID, amount)
	assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryInternalTransferConcurrentDistinctNoOverspend(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	source := createSQLCustomerAccount(t, svc, ctx, "Distinct", "TransferSource", "distinct.transfer.source@example.com", "2234567894", "Distinct Transfer Source")
	destination := createSQLCustomerAccount(t, svc, ctx, "Distinct", "TransferDestination", "distinct.transfer.destination@example.com", "2234567895", "Distinct Transfer Destination")
	if _, err := svc.InternalCredit(ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      source.ID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "sql-internal-transfer-distinct-fund",
	}); err != nil {
		t.Fatal(err)
	}

	const amount = int64(7000)
	results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
		return svc.InternalTransfer(ctx, InternalTransferInput{
			InstitutionID:        DemoInstitutionID,
			SourceAccountID:      source.ID,
			DestinationAccountID: destination.ID,
			AmountMinor:          amount,
			CurrencyID:           "NGN",
			IdempotencyKey:       fmt.Sprintf("sql-internal-transfer-distinct-attempt-%02d", i),
			Narration:            fmt.Sprintf("SQL distinct internal transfer %02d", i),
		})
	})

	successes := 0
	insufficient := 0
	for i, result := range results {
		switch {
		case result.err == nil:
			successes++
			assertStatus(t, result.transfer, TransferStatusSucceeded)
			assertSQLJournalBalanced(t, svc, ctx, result.transfer, amount)
		case errors.Is(result.err, ErrInsufficient):
			insufficient++
		default:
			t.Fatalf("unexpected distinct internal transfer result %d: transfer=%+v err=%v", i, result.transfer, result.err)
		}
	}
	if successes != 4 || insufficient != 6 {
		t.Fatalf("expected 4 successful transfers and 6 insufficient rejections, got successes=%d insufficient=%d", successes, insufficient)
	}
	assertSQLAccountBalancePair(t, svc, ctx, source.ID, 2000, 2000)
	assertSQLAccountBalancePair(t, svc, ctx, destination.ID, 28000, 28000)
	assertSQLTransferCountByIdempotencyPrefix(t, ctx, db, "sql-internal-transfer-distinct-attempt-", 4)
	assertSQLReplayIntegrity(t, ctx, db)
}

func TestSQLRepositoryAccountCreateConcurrentDuplicateNumber(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	svc := NewService(NewRepository(db), NewMockNIPProvider())

	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Concurrent",
		LastName:      "Account",
		Email:         "concurrent.account@example.com",
		Phone:         "+2348012345680",
	})
	if err != nil {
		t.Fatal(err)
	}

	const accountNumber = "1234567892"
	const requestCount = 10
	start := make(chan struct{})
	results := make(chan error, requestCount)
	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.CreateAccount(ctx, CreateAccountInput{
				InstitutionID: DemoInstitutionID,
				CustomerID:    customer.ID,
				AccountNumber: accountNumber,
				Name:          "Concurrent Wallet",
				ProductType:   AccountProductStandardWallet,
				CurrencyID:    "NGN",
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent account create error: %v", err)
		}
	}
	if successes != 1 || conflicts != requestCount-1 {
		t.Fatalf("expected one success and %d conflicts, got successes=%d conflicts=%d", requestCount-1, successes, conflicts)
	}

	var accountRows int
	if err := db.GetContext(ctx, &accountRows, `SELECT COUNT(*) FROM accounts WHERE institution_id = $1 AND account_number = $2`, DemoInstitutionID, accountNumber); err != nil {
		t.Fatal(err)
	}
	if accountRows != 1 {
		t.Fatalf("expected one account row for duplicate account number, got %d", accountRows)
	}

	var balanceRows int
	if err := db.GetContext(ctx, &balanceRows, `
SELECT COUNT(*)
FROM account_balances b
JOIN accounts a ON a.institution_id = b.institution_id AND a.id = b.account_id
WHERE a.institution_id = $1 AND a.account_number = $2`, DemoInstitutionID, accountNumber); err != nil {
		t.Fatal(err)
	}
	if balanceRows != 1 {
		t.Fatalf("expected one balance row for duplicate account number, got %d", balanceRows)
	}
}

func TestWithTxCommitsAndRollsBackMoneyMovementIntegration(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	repo := newSQLRepository(db)

	if _, err := repo.EnsureDemoData(ctx); err != nil {
		t.Fatal(err)
	}

	commitInput := RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       17000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "withtx-commit",
		Provider:          ProviderMockNIP,
		ProviderReference: "withtx-commit-ref",
		ProviderEventID:   "withtx-commit-event",
		Narration:         "WithTx commit proof",
	}
	if err := WithTx(ctx, db, func(tx TxRunner) error {
		_, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, commitInput)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertRepositoryBalance(t, repo, ctx, 17000, 17000)

	rollbackInput := RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       9000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "withtx-rollback",
		Provider:          ProviderMockNIP,
		ProviderReference: "withtx-rollback-ref",
		ProviderEventID:   "withtx-rollback-event",
		Narration:         "WithTx rollback proof",
	}
	forcedRollback := errors.New("force rollback after posting")
	err := WithTx(ctx, db, func(tx TxRunner) error {
		if _, err := repo.sqlTransferRepository.recordTransfer(ctx, tx, rollbackInput); err != nil {
			return err
		}
		return forcedRollback
	})
	if !errors.Is(err, forcedRollback) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}
	assertRepositoryBalance(t, repo, ctx, 17000, 17000)

	var rollbackRows int
	if err := db.GetContext(ctx, &rollbackRows, `SELECT COUNT(*) FROM transfers WHERE institution_id = $1 AND idempotency_key = $2`, DemoInstitutionID, rollbackInput.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if rollbackRows != 0 {
		t.Fatalf("rollback transfer should not be committed, found %d rows", rollbackRows)
	}
}

func TestSQLRepositoryTransferSpineIntegrationConcurrentReplay(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()

	t.Run("provider_event_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)

		const eventID = "sql-concurrent-provider-event"
		const amount = int64(3333)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockInbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    fmt.Sprintf("sql-concurrent-provider-event-%02d", i),
				ProviderEventID:   eventID,
				ProviderReference: "sql-concurrent-provider-ref",
				Narration:         "SQL concurrent provider event replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByProviderEvent(t, ctx, db, eventID, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("inbound_idempotency_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)

		const idempotencyKey = "sql-concurrent-inbound-idempotency"
		const amount = int64(2222)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockInbound(ctx, TransferRequest{
				AccountID:       DemoCustomerAccountID,
				AmountMinor:     amount,
				IdempotencyKey:  idempotencyKey,
				ProviderEventID: fmt.Sprintf("sql-concurrent-inbound-idempotency-event-%02d", i),
				Narration:       "SQL concurrent inbound idempotency replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("outbound_idempotency_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     50000,
			IdempotencyKey:  "sql-concurrent-outbound-funding",
			ProviderEventID: "sql-concurrent-outbound-funding-event",
			Narration:       "SQL concurrent outbound funding",
		})

		const idempotencyKey = "sql-concurrent-outbound-idempotency"
		const amount = int64(12000)
		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockOutbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    idempotencyKey,
				ProviderReference: "sql-concurrent-outbound-idempotency-ref",
				Narration:         "SQL concurrent outbound idempotency replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, 50000-amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByIdempotency(t, ctx, db, idempotencyKey, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})

	t.Run("pending_settlement_replay", func(t *testing.T) {
		resetIntegrationSchema(t, db)
		svc := seededSQLService(t, db, ctx)
		mockInbound(t, svc, ctx, TransferRequest{
			AccountID:       DemoCustomerAccountID,
			AmountMinor:     50000,
			IdempotencyKey:  "sql-concurrent-settlement-funding",
			ProviderEventID: "sql-concurrent-settlement-funding-event",
			Narration:       "SQL concurrent settlement funding",
		})

		const providerReference = "sql-concurrent-settlement-ref"
		const amount = int64(7000)
		pending := mockOutbound(t, svc, ctx, TransferRequest{
			AccountID:         DemoCustomerAccountID,
			AmountMinor:       amount,
			IdempotencyKey:    "sql-concurrent-settlement-pending",
			ProviderReference: providerReference,
			Status:            TransferStatusPending,
			Narration:         "SQL concurrent settlement pending outbound",
		})
		assertStatus(t, pending, TransferStatusPending)
		assertSQLBalancePair(t, svc, ctx, 50000-amount, 50000)

		results := runConcurrentTransfers(t, 10, func(i int) (*Transfer, error) {
			return svc.MockOutbound(ctx, TransferRequest{
				AccountID:         DemoCustomerAccountID,
				AmountMinor:       amount,
				IdempotencyKey:    fmt.Sprintf("sql-concurrent-settlement-%02d", i),
				ProviderReference: providerReference,
				ProviderEventID:   fmt.Sprintf("sql-concurrent-settlement-event-%02d", i),
				Status:            TransferStatusSucceeded,
				Narration:         "SQL concurrent pending settlement replay",
			})
		})

		transfer := assertConcurrentReplay(t, results)
		if transfer.ID != pending.ID {
			t.Fatalf("settlement replay returned different transfer: pending=%s got=%s", pending.ID, transfer.ID)
		}
		assertStatus(t, transfer, TransferStatusSucceeded)
		assertSQLBalance(t, svc, ctx, 50000-amount)
		assertSQLJournalBalanced(t, svc, ctx, transfer, amount)
		assertSQLTransferCountByProviderReference(t, ctx, db, providerReference, TransferDirectionOutbound, 1)
		assertSQLJournalCountByProviderReference(t, ctx, db, providerReference, TransferDirectionOutbound, 1)
		assertSQLReplayIntegrity(t, ctx, db)
	})
}

func integrationDB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv("LENZ_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set LENZ_INTEGRATION_DATABASE_URL or DATABASE_URL to run SQL integration tests")
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		t.Fatalf("connect integration database: %v", err)
	}
	t.Cleanup(func() {
		resetIntegrationSchema(t, db)
		_ = db.Close()
	})
	resetIntegrationSchema(t, db)
	return db
}

type concurrentTransferResult struct {
	transfer *Transfer
	err      error
}

func seededSQLService(t *testing.T, db *sqlx.DB, ctx context.Context) *Service {
	t.Helper()
	svc := NewService(NewRepository(db), NewMockNIPProvider())
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return svc
}

func createSQLCustomerAccount(t *testing.T, svc *Service, ctx context.Context, firstName, lastName, email, accountNumber, accountName string) *Account {
	t.Helper()
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     firstName,
		LastName:      lastName,
		Email:         email,
		Phone:         "+2348012345684",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    customer.ID,
		AccountNumber: accountNumber,
		Name:          accountName,
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func runConcurrentTransfers(t *testing.T, count int, fn func(int) (*Transfer, error)) []concurrentTransferResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]concurrentTransferResult, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			transfer, err := fn(i)
			results[i] = concurrentTransferResult{transfer: transfer, err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

func assertConcurrentReplay(t *testing.T, results []concurrentTransferResult) *Transfer {
	t.Helper()
	var first *Transfer
	for i, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent replay request %d returned error: %v", i, result.err)
		}
		if result.transfer == nil {
			t.Fatalf("concurrent replay request %d returned nil transfer", i)
		}
		if first == nil {
			first = result.transfer
			continue
		}
		if result.transfer.ID != first.ID {
			t.Fatalf("concurrent replay request %d returned transfer %s, want %s", i, result.transfer.ID, first.ID)
		}
	}
	return first
}

func assertRepositoryBalance(t *testing.T, repo *SQLRepository, ctx context.Context, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := repo.GetBalance(ctx, DemoInstitutionID, DemoCustomerAccountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want available=%d ledger=%d", balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
	}
}

func assertSQLReplayIntegrity(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	if err := assertAllSQLJournalsBalanced(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := assertSQLBalancesMatchPostings(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertNoSQLDuplicateProviderEvents(t, ctx, db)
	assertNoSQLDuplicateIdempotencyKeys(t, ctx, db)
	t.Log("journal_mismatches=0 balance_mismatches=0 provider_event_duplicate_count=0 idempotency_duplicate_count=0")
}

func assertNoSQLDuplicateProviderEvents(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	var duplicates int
	if err := db.GetContext(ctx, &duplicates, `
SELECT COUNT(*)
FROM (
	SELECT institution_id, provider, provider_event_id
	FROM provider_events
	GROUP BY institution_id, provider, provider_event_id
	HAVING COUNT(*) > 1
) duplicate_provider_events`); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("provider_event duplicate count = %d, want 0", duplicates)
	}
}

func assertNoSQLDuplicateIdempotencyKeys(t *testing.T, ctx context.Context, db *sqlx.DB) {
	t.Helper()
	var duplicates int
	if err := db.GetContext(ctx, &duplicates, `
SELECT COUNT(*)
FROM (
	SELECT institution_id, idempotency_key
	FROM transfers
	GROUP BY institution_id, idempotency_key
	HAVING COUNT(*) > 1
) duplicate_idempotency_keys`); err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("idempotency duplicate count = %d, want 0", duplicates)
	}
}

func assertSQLTransferCountByProviderEvent(t *testing.T, ctx context.Context, db *sqlx.DB, providerEventID string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND provider_event_id = $2`, DemoInstitutionID, providerEventID); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for provider_event_id %q = %d, want %d", providerEventID, count, want)
	}
}

func assertSQLTransferCountByIdempotency(t *testing.T, ctx context.Context, db *sqlx.DB, idempotencyKey string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND idempotency_key = $2`, DemoInstitutionID, idempotencyKey); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for idempotency_key %q = %d, want %d", idempotencyKey, count, want)
	}
}

func assertSQLTransferCountByIdempotencyPrefix(t *testing.T, ctx context.Context, db *sqlx.DB, prefix string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND idempotency_key LIKE $2`, DemoInstitutionID, prefix+"%"); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for idempotency_key prefix %q = %d, want %d", prefix, count, want)
	}
}

func assertSQLTransferCountByProviderReference(t *testing.T, ctx context.Context, db *sqlx.DB, providerReference, direction string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers
WHERE institution_id = $1 AND provider_reference = $2 AND direction = $3`, DemoInstitutionID, providerReference, direction); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("transfer count for provider_reference %q direction %q = %d, want %d", providerReference, direction, count, want)
	}
}

func assertSQLJournalCountByProviderReference(t *testing.T, ctx context.Context, db *sqlx.DB, providerReference, direction string, want int) {
	t.Helper()
	var count int
	if err := db.GetContext(ctx, &count, `
SELECT COUNT(*)
FROM transfers t
JOIN journal_entries je ON je.institution_id = t.institution_id AND je.transfer_id = t.id
WHERE t.institution_id = $1 AND t.provider_reference = $2 AND t.direction = $3`, DemoInstitutionID, providerReference, direction); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("journal count for provider_reference %q direction %q = %d, want %d", providerReference, direction, count, want)
	}
}

func resetIntegrationSchema(t *testing.T, db *sqlx.DB) {
	t.Helper()
	_, err := db.Exec(`
TRUNCATE TABLE
	audit_events,
	provider_events,
	account_holds,
	transfers,
	postings,
	journal_entries,
	account_balances,
	accounts,
	customers,
	branches,
	institutions,
	countries,
	currencies
RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset integration schema: %v", err)
	}
}

func assertSQLBalance(t *testing.T, svc *Service, ctx context.Context, want int64) {
	t.Helper()
	assertSQLBalancePair(t, svc, ctx, want, want)
}

func assertSQLBalancePair(t *testing.T, svc *Service, ctx context.Context, wantAvailable, wantLedger int64) {
	t.Helper()
	assertSQLAccountBalancePair(t, svc, ctx, DemoCustomerAccountID, wantAvailable, wantLedger)
}

func assertSQLAccountBalancePair(t *testing.T, svc *Service, ctx context.Context, accountID string, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch for account %s: got available=%d ledger=%d want available=%d ledger=%d", accountID, balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
	}
}

func assertSQLJournalBalanced(t *testing.T, svc *Service, ctx context.Context, transfer *Transfer, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected transfer to have journal entry: %+v", transfer)
	}
	journal, err := svc.GetJournal(ctx, transfer.InstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Balanced || journal.JournalEntry.TotalDebitMinor != amountMinor || journal.JournalEntry.TotalCreditMinor != amountMinor || len(journal.Postings) != 2 {
		t.Fatalf("journal is not balanced for %d: %+v", amountMinor, journal)
	}
	var debit, credit int64
	for _, posting := range journal.Postings {
		switch posting.Direction {
		case PostingDebit:
			debit += posting.AmountMinor
		case PostingCredit:
			credit += posting.AmountMinor
		}
	}
	if debit != amountMinor || credit != amountMinor {
		t.Fatalf("posting totals mismatch: debit=%d credit=%d want=%d", debit, credit, amountMinor)
	}
}

func assertSQLInternalCreditRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, accountID, sourceAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal credit to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal credit transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal credit, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	source := postings[sourceAccountID]
	customer := postings[accountID]
	if source.direction != PostingDebit || source.amount != amountMinor || customer.direction != PostingCredit || customer.amount != amountMinor {
		t.Fatalf("internal credit postings should debit source and credit customer: %+v", rows)
	}
}

func assertSQLInternalDebitRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, accountID, destinationAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal debit to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal debit transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal debit, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	customer := postings[accountID]
	destination := postings[destinationAccountID]
	if customer.direction != PostingDebit || customer.amount != amountMinor || destination.direction != PostingCredit || destination.amount != amountMinor {
		t.Fatalf("internal debit postings should debit customer and credit destination: %+v", rows)
	}
}

func assertSQLInternalTransferRows(t *testing.T, ctx context.Context, db *sqlx.DB, transfer *Transfer, sourceAccountID, destinationAccountID string, amountMinor int64) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected internal transfer to have journal entry: %+v", transfer)
	}
	var journalRows int
	if err := db.GetContext(ctx, &journalRows, `
SELECT COUNT(*)
FROM journal_entries
WHERE institution_id = $1 AND transfer_id = $2 AND id = $3`, DemoInstitutionID, transfer.ID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if journalRows != 1 {
		t.Fatalf("expected one journal for internal transfer %s, got %d", transfer.ID, journalRows)
	}

	rows := []struct {
		AccountID string `db:"account_id"`
		Direction string `db:"direction"`
		Amount    int64  `db:"amount_minor"`
	}{}
	if err := db.SelectContext(ctx, &rows, `
SELECT account_id, direction, amount_minor
FROM postings
WHERE institution_id = $1 AND journal_entry_id = $2
ORDER BY account_id`, DemoInstitutionID, *transfer.JournalEntryID); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected two postings for internal transfer, got %+v", rows)
	}
	postings := map[string]struct {
		direction string
		amount    int64
	}{}
	for _, row := range rows {
		postings[row.AccountID] = struct {
			direction string
			amount    int64
		}{direction: row.Direction, amount: row.Amount}
	}
	source := postings[sourceAccountID]
	destination := postings[destinationAccountID]
	if source.direction != PostingDebit || source.amount != amountMinor || destination.direction != PostingCredit || destination.amount != amountMinor {
		t.Fatalf("internal transfer postings should debit source and credit destination: %+v", rows)
	}
}

func assertSQLHistory(t *testing.T, history []Transaction, inboundID, outboundID, failedPendingOutboundID, succeededPendingOutboundID, pendingID, failedID, reversalID string) {
	t.Helper()
	if len(history) != 7 {
		t.Fatalf("expected seven transaction history rows, got %d: %+v", len(history), history)
	}
	seen := map[string]Transaction{}
	for _, txn := range history {
		seen[txn.TransferID] = txn
	}
	expect := map[string]struct {
		status      string
		signedMinor int64
		hasJournal  bool
	}{
		inboundID:                  {status: TransferStatusSucceeded, signedMinor: 500000, hasJournal: true},
		outboundID:                 {status: TransferStatusSucceeded, signedMinor: -125000, hasJournal: true},
		failedPendingOutboundID:    {status: TransferStatusFailed, signedMinor: 0, hasJournal: false},
		succeededPendingOutboundID: {status: TransferStatusSucceeded, signedMinor: -25000, hasJournal: true},
		pendingID:                  {status: TransferStatusPending, signedMinor: 0, hasJournal: false},
		failedID:                   {status: TransferStatusFailed, signedMinor: 0, hasJournal: false},
		reversalID:                 {status: TransferStatusSucceeded, signedMinor: -500000, hasJournal: true},
	}
	for transferID, want := range expect {
		got, ok := seen[transferID]
		if !ok {
			t.Fatalf("missing history row for transfer %s: %+v", transferID, history)
		}
		if got.Status != want.status || got.SignedMinor != want.signedMinor {
			t.Fatalf("history mismatch for %s: got %+v want status=%s signed=%d", transferID, got, want.status, want.signedMinor)
		}
		if want.hasJournal && got.JournalEntryID == nil {
			t.Fatalf("succeeded history row must be backed by a Lenz journal: %+v", got)
		}
		if !want.hasJournal && got.JournalEntryID != nil {
			t.Fatalf("non-posted history row should not have a journal: %+v", got)
		}
	}
}

func assertAllSQLJournalsBalanced(ctx context.Context, db *sqlx.DB) error {
	var mismatches int
	err := db.GetContext(ctx, &mismatches, `
WITH journal_totals AS (
	SELECT
		je.id,
		je.total_debit_minor,
		je.total_credit_minor,
		COALESCE(SUM(CASE WHEN p.direction = 'debit' THEN p.amount_minor ELSE 0 END), 0) AS posting_debits,
		COALESCE(SUM(CASE WHEN p.direction = 'credit' THEN p.amount_minor ELSE 0 END), 0) AS posting_credits
	FROM journal_entries je
	LEFT JOIN postings p
		ON p.institution_id = je.institution_id
		AND p.journal_entry_id = je.id
	WHERE je.institution_id = $1
	GROUP BY je.id
)
SELECT COUNT(*)
FROM journal_totals
WHERE total_debit_minor <> total_credit_minor
	OR total_debit_minor <> posting_debits
	OR total_credit_minor <> posting_credits`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("found unbalanced SQL journal entries")
	}
	return nil
}

func assertSQLBalancesMatchPostings(ctx context.Context, db *sqlx.DB) error {
	var mismatches int
	err := db.GetContext(ctx, &mismatches, `
WITH posting_balances AS (
	SELECT
		a.institution_id,
		a.id AS account_id,
		COALESCE(SUM(
			CASE
				WHEN p.id IS NULL THEN 0
				WHEN (a.normal_balance = 'credit' AND p.direction = 'credit')
					OR (a.normal_balance = 'debit' AND p.direction = 'debit')
				THEN p.amount_minor
				ELSE -p.amount_minor
			END
		), 0) AS computed_minor
	FROM accounts a
	LEFT JOIN postings p
		ON p.institution_id = a.institution_id
		AND p.account_id = a.id
	WHERE a.institution_id = $1
	GROUP BY a.institution_id, a.id
),
active_holds AS (
	SELECT
		institution_id,
		account_id,
		COALESCE(SUM(amount_minor), 0) AS held_minor
	FROM account_holds
	WHERE institution_id = $1 AND status = 'active'
	GROUP BY institution_id, account_id
)
SELECT COUNT(*)
FROM posting_balances pb
LEFT JOIN account_balances b
	ON b.institution_id = pb.institution_id
	AND b.account_id = pb.account_id
LEFT JOIN active_holds h
	ON h.institution_id = pb.institution_id
	AND h.account_id = pb.account_id
WHERE b.account_id IS NULL
	OR b.ledger_minor <> pb.computed_minor
	OR b.available_minor <> pb.computed_minor - COALESCE(h.held_minor, 0)`, DemoInstitutionID)
	if err != nil {
		return err
	}
	if mismatches != 0 {
		return errors.New("SQL account balances do not reconcile to postings and active holds")
	}
	return nil
}
