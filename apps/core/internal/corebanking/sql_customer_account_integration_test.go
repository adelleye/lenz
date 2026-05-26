//go:build integration

package corebanking

import (
	"context"
	"errors"
	"testing"
)

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
