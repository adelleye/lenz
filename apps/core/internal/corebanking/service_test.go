package corebanking

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateCustomerStoresCustomerInInstitutionBranch(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  " individual ",
		FirstName:     "  Adaeze ",
		LastName:      " Okafor ",
		Email:         " ADAEZE@example.com ",
		Phone:         " +2348012345678 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if customer.ID == "" || customer.InstitutionID != DemoInstitutionID || customer.BranchID != DemoBranchID {
		t.Fatalf("created customer has wrong scope: %+v", customer)
	}
	if customer.CustomerType != CustomerTypeIndividual || customer.FirstName != "Adaeze" || customer.LastName != "Okafor" || customer.Email != "adaeze@example.com" || customer.Phone != "+2348012345678" || customer.Status != "active" {
		t.Fatalf("created customer was not normalized: %+v", customer)
	}
	if customer.KYCTier != CustomerKYCTier1 || customer.BVNStatus != CustomerIdentityStatusNotCollected || customer.NINStatus != CustomerIdentityStatusNotCollected {
		t.Fatalf("created customer was not normalized: %+v", customer)
	}

	got, err := svc.GetCustomer(ctx, DemoInstitutionID, customer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != customer.ID || got.Email != customer.Email {
		t.Fatalf("get customer mismatch: got %+v want %+v", got, customer)
	}
}

func TestCreateBusinessCustomerStoresBusinessNameInMeta(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeBusiness,
		BusinessName:  "Clive Alliance",
	})
	if err != nil {
		t.Fatal(err)
	}
	if customer.CustomerType != CustomerTypeBusiness || customer.BusinessName == nil || *customer.BusinessName != "Clive Alliance" {
		t.Fatalf("business customer did not preserve business metadata: %+v", customer)
	}
	if customer.FirstName != "" || customer.LastName != "" || customer.Email != "" || customer.Phone != "" {
		t.Fatalf("business customer should not require individual/contact fields: %+v", customer)
	}
}

func TestCreateCustomerRejectsInvalidInput(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	tests := []CreateCustomerInput{
		{InstitutionID: "", BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: "Demo"},
		{InstitutionID: DemoInstitutionID, BranchID: "", CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: "Demo"},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: "", FirstName: "Ada", LastName: "Demo"},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: "invalid", FirstName: "Ada", LastName: "Demo"},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "", LastName: "Demo"},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: ""},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: "Demo", Email: "not-email"},
		{InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeBusiness, BusinessName: ""},
	}
	for i, input := range tests {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := svc.CreateCustomer(ctx, input)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected invalid request, got %v", err)
			}
		})
	}
}

func TestCreateCustomerRequiresBranchInInstitution(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	_, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: "99999999-9999-9999-9999-999999999999",
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Ada",
		LastName:      "Demo",
		Email:         "ada@example.com",
		Phone:         "+2348012345678",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-institution branch lookup to fail as not found, got %v", err)
	}
}

func TestCreateStandardAccountCreatesZeroBalance(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    DemoCustomerID,
		AccountNumber: "1234567890",
		Name:          "Ada Main Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if account.ID == "" || account.InstitutionID != DemoInstitutionID || account.CustomerID == nil || *account.CustomerID != DemoCustomerID {
		t.Fatalf("created account has wrong scope: %+v", account)
	}
	if account.Kind != AccountKindCustomer || account.ProductType != AccountProductStandardWallet || account.AllowNegative || account.CurrencyID != "NGN" || account.NormalBalance != NormalBalanceCredit || account.Status != "active" {
		t.Fatalf("created account has wrong defaults: %+v", account)
	}
	balance, err := svc.GetBalance(ctx, DemoInstitutionID, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != 0 || balance.LedgerMinor != 0 || balance.CurrencyID != "NGN" || balance.LastJournalEntryID != nil {
		t.Fatalf("initial balance mismatch: %+v", balance)
	}
}

func TestCreateStandardSavingsAndCurrentAccounts(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	tests := []struct {
		productType   string
		accountNumber string
	}{
		{productType: AccountProductStandardSavings, accountNumber: "1234567891"},
		{productType: AccountProductStandardCurrent, accountNumber: "1234567892"},
	}
	for _, tt := range tests {
		t.Run(tt.productType, func(t *testing.T) {
			account, err := svc.CreateAccount(ctx, CreateAccountInput{
				InstitutionID: DemoInstitutionID,
				CustomerID:    DemoCustomerID,
				AccountNumber: tt.accountNumber,
				Name:          "Ada " + tt.productType,
				ProductType:   tt.productType,
				CurrencyID:    "NGN",
			})
			if err != nil {
				t.Fatal(err)
			}
			if account.ProductType != tt.productType || account.AllowNegative {
				t.Fatalf("created account mismatch: %+v", account)
			}
		})
	}
}

func TestCreateAccountRejectsInvalidInput(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	tests := []struct {
		name  string
		input CreateAccountInput
	}{
		{
			name:  "missing customer",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: "99999999-9999-9999-9999-999999999999", AccountNumber: "1234567890", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
		},
		{
			name:  "cross institution customer",
			input: CreateAccountInput{InstitutionID: "99999999-9999-9999-9999-999999999999", CustomerID: DemoCustomerID, AccountNumber: "1234567890", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
		},
		{
			name:  "negative balance",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "1234567890", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN", AllowNegativeBalance: true},
		},
		{
			name:  "unsupported product",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "1234567890", Name: "Ada Wallet", ProductType: AccountProductInternal, CurrencyID: "NGN"},
		},
		{
			name:  "invalid account number",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "12345", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
		},
		{
			name:  "unsupported currency",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "1234567890", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "USD"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateAccount(ctx, tt.input)
			if !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected validation/not-found error, got %v", err)
			}
		})
	}
}

func TestCreateAccountRejectsDuplicateAccountNumber(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	input := CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    DemoCustomerID,
		AccountNumber: "1234567890",
		Name:          "Ada Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	}
	if _, err := svc.CreateAccount(ctx, input); err != nil {
		t.Fatal(err)
	}
	_, err := svc.CreateAccount(ctx, input)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate account number to return conflict, got %v", err)
	}
}

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
	if len(history) != 1 || history[0].TransferID != transfer.ID || history[0].Status != TransferStatusPending || history[0].SignedMinor != 0 {
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

func TestOverdraftCapableAccountCanBeRepresented(t *testing.T) {
	ctx, svc, store := newTestService(t)
	creditAccountID := "66666666-6666-6666-6666-666666666666"
	customerID := DemoCustomerID
	now := time.Now().UTC()
	store.accounts[creditAccountID] = Account{ID: creditAccountID, InstitutionID: DemoInstitutionID, CustomerID: &customerID, AccountNumber: "9990000002", Name: "Ada Demo Overdraft", Kind: AccountKindCustomer, ProductType: AccountProductOverdraftCredit, AllowNegative: true, CurrencyID: "NGN", NormalBalance: NormalBalanceCredit, Status: "active", CreatedAt: now, UpdatedAt: now}
	store.balances[creditAccountID] = AccountBalance{AccountID: creditAccountID, InstitutionID: DemoInstitutionID, CurrencyID: "NGN", UpdatedAt: now}

	transfer := mockOutbound(t, svc, ctx, TransferRequest{
		AccountID:      creditAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "overdraft-out",
	})

	assertStatus(t, transfer, TransferStatusSucceeded)
	assertBalance(t, svc, ctx, DemoInstitutionID, creditAccountID, -10000)
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

func TestTenantScopingPreventsCrossTenantReads(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mockInbound(t, svc, ctx, TransferRequest{AccountID: DemoCustomerAccountID, AmountMinor: 10000, IdempotencyKey: "tenant-in", ProviderEventID: "evt-tenant-in"})

	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant balance read to fail, got %v", err)
	}
	if _, err := svc.GetTransactions(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID, ListTransactionsOptions{}); err != nil {
		t.Fatalf("empty cross-tenant history should not error: %v", err)
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
	if _, err := svc.ListTransfers(ctx, ""); !errors.Is(err, ErrInvalidRequest) {
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

func TestMockOutboundCallsProviderAdapter(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "provider-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "provider outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "provider-test-fund",
		Provider:          "test_setup",
		ProviderReference: "provider-test-fund-ref",
		ProviderEventID:   "provider-test-fund-event",
		Narration:         "provider adapter test funding",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "provider-adapter-out",
		Narration:      "outbound through provider",
	})
	if err != nil {
		t.Fatal(err)
	}

	if provider.initiateCalls != 1 {
		t.Fatalf("expected one provider InitiateTransfer call, got %d", provider.initiateCalls)
	}
	if provider.lastInitiate.AmountMinor != 10000 || provider.lastInitiate.AccountID != DemoCustomerAccountID {
		t.Fatalf("provider received wrong request: %+v", provider.lastInitiate)
	}
	if transfer.ProviderReference != "provider-out-ref" || transfer.Narration != "provider outbound" {
		t.Fatalf("transfer did not use provider result: %+v", transfer)
	}
}

func TestMockOutboundIdempotencyPreventsDuplicateProviderInitiation(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "idem-provider-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "provider outbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "idem-provider-fund",
		Provider:          "test_setup",
		ProviderReference: "idem-provider-fund-ref",
		ProviderEventID:   "idem-provider-fund-event",
		Narration:         "fund test account",
	}); err != nil {
		t.Fatal(err)
	}

	req := TransferRequest{
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    10000,
		IdempotencyKey: "provider-adapter-idem-out",
		Narration:      "outbound",
	}
	first, err := svc.MockOutbound(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.MockOutbound(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected duplicate idempotency request to return original transfer")
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected one provider InitiateTransfer call, got %d", provider.initiateCalls)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000)
}

func TestMockOutboundRecordsProviderUnknownOnTimeout(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{initiateErr: context.DeadlineExceeded}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordTransfer(ctx, RecordTransferInput{
		InstitutionID:     DemoInstitutionID,
		AccountID:         DemoCustomerAccountID,
		ClearingAccountID: DemoClearingAccountID,
		Direction:         TransferDirectionInbound,
		Status:            TransferStatusSucceeded,
		AmountMinor:       50000,
		CurrencyID:        "NGN",
		IdempotencyKey:    "unknown-provider-fund",
		Provider:          "test_setup",
		ProviderReference: "unknown-provider-fund-ref",
		ProviderEventID:   "unknown-provider-fund-event",
		Narration:         "fund test account",
	}); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockOutbound(ctx, TransferRequest{
		AccountID:         DemoCustomerAccountID,
		AmountMinor:       10000,
		IdempotencyKey:    "unknown-provider-out",
		ProviderReference: "unknown-provider-out-ref",
		Narration:         "outbound",
	})
	if err != nil {
		t.Fatal(err)
	}

	if transfer.Status != TransferStatusPending || transfer.ProviderStatus != TransferProviderStatusUnknown || transfer.ReconciliationStatus != ReconciliationStatusManualReview {
		t.Fatalf("provider unknown transfer mismatch: %+v", transfer)
	}
	if transfer.JournalEntryID != nil {
		t.Fatalf("provider unknown transfer should not post a journal: %+v", transfer)
	}
	if hold := store.holds[transfer.ID]; hold.Status != HoldStatusActive || hold.AmountMinor != 10000 {
		t.Fatalf("provider unknown outbound should keep an active hold: %+v", hold)
	}
	assertBalancePair(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000, 50000)
}

func TestMockInboundCallsProviderAdapter(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		webhookEvent: ProviderWebhookEvent{
			Provider:          ProviderMockNIP,
			InstitutionID:     DemoInstitutionID,
			AccountID:         DemoCustomerAccountID,
			Direction:         TransferDirectionInbound,
			Status:            TransferStatusSucceeded,
			AmountMinor:       15000,
			CurrencyID:        "NGN",
			IdempotencyKey:    "provider-adapter-in",
			ProviderReference: "provider-in-ref",
			ProviderEventID:   "provider-in-event",
			Narration:         "provider inbound",
		},
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}

	transfer, err := svc.MockInbound(ctx, TransferRequest{
		AccountID:       DemoCustomerAccountID,
		AmountMinor:     999,
		IdempotencyKey:  "payload-idem",
		ProviderEventID: "payload-event",
	})
	if err != nil {
		t.Fatal(err)
	}

	if provider.parseCalls != 1 {
		t.Fatalf("expected one provider ParseWebhook call, got %d", provider.parseCalls)
	}
	if provider.lastHeaders["Idempotency-Key"] != "payload-idem" {
		t.Fatalf("provider did not receive idempotency header: %+v", provider.lastHeaders)
	}
	if transfer.AmountMinor != 15000 || transfer.ProviderReference != "provider-in-ref" {
		t.Fatalf("transfer did not use parsed provider webhook event: %+v", transfer)
	}
}

func newTestService(t *testing.T) (context.Context, *Service, *memoryStore) {
	t.Helper()
	store := newMemoryStore()
	svc := NewService(store, NewMockNIPProvider())
	ctx := context.Background()
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, svc, store
}

func mustTransfer(t *testing.T, transfer *Transfer, err error) *Transfer {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return transfer
}

func mockInbound(t *testing.T, svc *Service, ctx context.Context, req TransferRequest) *Transfer {
	t.Helper()
	transfer, err := svc.MockInbound(ctx, req)
	return mustTransfer(t, transfer, err)
}

func mockOutbound(t *testing.T, svc *Service, ctx context.Context, req TransferRequest) *Transfer {
	t.Helper()
	transfer, err := svc.MockOutbound(ctx, req)
	return mustTransfer(t, transfer, err)
}

func reverseTransfer(t *testing.T, svc *Service, ctx context.Context, transferID, idempotencyKey string) *Transfer {
	t.Helper()
	transfer, err := svc.ReverseTransfer(ctx, DemoInstitutionID, transferID, idempotencyKey)
	return mustTransfer(t, transfer, err)
}

func mockProviderEvent(t *testing.T, svc *Service, ctx context.Context, event ProviderWebhookEvent) *Transfer {
	t.Helper()
	if event.Provider == "" {
		event.Provider = ProviderMockNIP
	}
	transfer, err := svc.recordProviderWebhookEvent(ctx, event)
	return mustTransfer(t, transfer, err)
}

func assertStatus(t *testing.T, transfer *Transfer, status string) {
	t.Helper()
	if transfer.Status != status {
		t.Fatalf("expected status %s, got %s", status, transfer.Status)
	}
}

func assertBalance(t *testing.T, svc *Service, ctx context.Context, institutionID, accountID string, want int64) {
	t.Helper()
	assertBalancePair(t, svc, ctx, institutionID, accountID, want, want)
}

func assertBalancePair(t *testing.T, svc *Service, ctx context.Context, institutionID, accountID string, wantAvailable, wantLedger int64) {
	t.Helper()
	balance, err := svc.GetBalance(ctx, institutionID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.AvailableMinor != wantAvailable || balance.LedgerMinor != wantLedger {
		t.Fatalf("balance mismatch: got available=%d ledger=%d want available=%d ledger=%d", balance.AvailableMinor, balance.LedgerMinor, wantAvailable, wantLedger)
	}
}

func assertJournalBalanced(t *testing.T, store *memoryStore, transfer *Transfer) {
	t.Helper()
	if transfer.JournalEntryID == nil {
		t.Fatalf("expected transfer to have journal entry")
	}
	journal, err := store.GetJournal(context.Background(), transfer.InstitutionID, *transfer.JournalEntryID)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.Balanced || len(journal.Postings) != 2 {
		t.Fatalf("journal not balanced: %+v", journal)
	}
}

func assertHistoryNewestFirst(t *testing.T, history []Transaction) {
	t.Helper()
	for i := 1; i < len(history); i++ {
		if history[i].CreatedAt.After(history[i-1].CreatedAt) {
			t.Fatalf("history is not ordered newest first at %d: %s after %s", i, history[i].CreatedAt, history[i-1].CreatedAt)
		}
	}
}

func setTransferCreatedAt(t *testing.T, store *memoryStore, transferID string, createdAt time.Time) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	transfer := store.transfers[transferID]
	transfer.CreatedAt = createdAt
	store.transfers[transferID] = transfer
}

type memoryStore struct {
	mu             sync.Mutex
	institutions   map[string]Institution
	branches       map[string]Branch
	customers      map[string]Customer
	accounts       map[string]Account
	balances       map[string]AccountBalance
	transfers      map[string]Transfer
	journals       map[string]JournalEntry
	postings       map[string][]Posting
	holds          map[string]AccountHold
	idempotency    map[string]string
	providerEvents map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		institutions:   map[string]Institution{},
		branches:       map[string]Branch{},
		customers:      map[string]Customer{},
		accounts:       map[string]Account{},
		balances:       map[string]AccountBalance{},
		transfers:      map[string]Transfer{},
		journals:       map[string]JournalEntry{},
		postings:       map[string][]Posting{},
		holds:          map[string]AccountHold{},
		idempotency:    map[string]string{},
		providerEvents: map[string]string{},
	}
}

func (m *memoryStore) EnsureDemoData(ctx context.Context) (*SeedResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	customerID := DemoCustomerID
	m.institutions[DemoInstitutionID] = Institution{ID: DemoInstitutionID, Name: "Lenz Demo Microfinance Bank", ShortName: "Lenz Demo", Code: "999001", CurrencyID: "NGN", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.branches[DemoBranchID] = Branch{ID: DemoBranchID, InstitutionID: DemoInstitutionID, Code: "HQ", Name: "Demo HQ", Status: "active", CreatedAt: now, UpdatedAt: now}
	m.customers[DemoCustomerID] = Customer{ID: DemoCustomerID, InstitutionID: DemoInstitutionID, BranchID: DemoBranchID, CustomerType: CustomerTypeIndividual, FirstName: "Ada", LastName: "Demo", Email: "ada.demo@example.com", Phone: "+2348000000000", Status: "active", KYCTier: CustomerKYCTier1, BVNStatus: CustomerIdentityStatusNotCollected, NINStatus: CustomerIdentityStatusNotCollected, CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoCustomerAccountID] = Account{ID: DemoCustomerAccountID, InstitutionID: DemoInstitutionID, CustomerID: &customerID, AccountNumber: "9990000001", Name: "Ada Demo Wallet", Kind: AccountKindCustomer, ProductType: AccountProductStandardWallet, AllowNegative: false, CurrencyID: "NGN", NormalBalance: NormalBalanceCredit, Status: "active", CreatedAt: now, UpdatedAt: now}
	m.accounts[DemoClearingAccountID] = Account{ID: DemoClearingAccountID, InstitutionID: DemoInstitutionID, AccountNumber: "9999999999", Name: "Mock NIP Clearing", Kind: AccountKindInternal, ProductType: AccountProductInternal, AllowNegative: true, CurrencyID: "NGN", NormalBalance: NormalBalanceDebit, Status: "active", CreatedAt: now, UpdatedAt: now}
	if _, ok := m.balances[DemoCustomerAccountID]; !ok {
		m.balances[DemoCustomerAccountID] = AccountBalance{AccountID: DemoCustomerAccountID, InstitutionID: DemoInstitutionID, CurrencyID: "NGN", UpdatedAt: now}
	}
	if _, ok := m.balances[DemoClearingAccountID]; !ok {
		m.balances[DemoClearingAccountID] = AccountBalance{AccountID: DemoClearingAccountID, InstitutionID: DemoInstitutionID, CurrencyID: "NGN", UpdatedAt: now}
	}
	return m.seedResultLocked(), nil
}

func (m *memoryStore) seedResultLocked() *SeedResult {
	return &SeedResult{
		Institution: m.institutions[DemoInstitutionID],
		Branch:      m.branches[DemoBranchID],
		Customer:    m.customers[DemoCustomerID],
		Account:     m.accounts[DemoCustomerAccountID],
		Clearing:    m.accounts[DemoClearingAccountID],
	}
}

func (m *memoryStore) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	branch, ok := m.branches[input.BranchID]
	if !ok || branch.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	customer := Customer{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: input.InstitutionID,
		BranchID:      input.BranchID,
		CustomerType:  input.CustomerType,
		FirstName:     input.FirstName,
		LastName:      input.LastName,
		Email:         input.Email,
		Phone:         input.Phone,
		Status:        "active",
		KYCTier:       input.KYCTier,
		BVNStatus:     input.BVNStatus,
		NINStatus:     input.NINStatus,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if input.BusinessName != "" {
		customer.BusinessName = &input.BusinessName
	}
	m.customers[customer.ID] = customer
	return copyOf(customer), nil
}

func (m *memoryStore) GetCustomer(ctx context.Context, institutionID, customerID string) (*Customer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	customer, ok := m.customers[customerID]
	if !ok || customer.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(customer), nil
}

func (m *memoryStore) CreateAccount(ctx context.Context, input CreateAccountInput) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	customer, ok := m.customers[input.CustomerID]
	if !ok || customer.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	for _, account := range m.accounts {
		if account.InstitutionID == input.InstitutionID && account.AccountNumber == input.AccountNumber {
			return nil, ErrConflict
		}
	}
	now := time.Now().UTC()
	account := Account{
		ID:            uuid.Must(uuid.NewRandom()).String(),
		InstitutionID: input.InstitutionID,
		CustomerID:    &input.CustomerID,
		AccountNumber: input.AccountNumber,
		Name:          input.Name,
		Kind:          AccountKindCustomer,
		ProductType:   input.ProductType,
		AllowNegative: false,
		CurrencyID:    input.CurrencyID,
		NormalBalance: NormalBalanceCredit,
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.accounts[account.ID] = account
	m.balances[account.ID] = AccountBalance{
		AccountID:      account.ID,
		InstitutionID:  account.InstitutionID,
		AvailableMinor: 0,
		LedgerMinor:    0,
		CurrencyID:     account.CurrencyID,
		UpdatedAt:      now,
	}
	return copyOf(account), nil
}

func (m *memoryStore) ListAccountsByCustomer(ctx context.Context, institutionID, customerID string) ([]Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var accounts []Account
	for _, account := range m.accounts {
		if account.InstitutionID == institutionID && account.CustomerID != nil && *account.CustomerID == customerID {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (m *memoryStore) GetAccount(ctx context.Context, institutionID, accountID string) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	account, ok := m.accounts[accountID]
	if !ok || account.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(account), nil
}

func (m *memoryStore) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	balance, ok := m.balances[accountID]
	if !ok || balance.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(balance), nil
}

func (m *memoryStore) GetTransfer(ctx context.Context, institutionID, transferID string) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[transferID]
	if !ok || transfer.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) GetTransferByIdempotency(ctx context.Context, institutionID, idempotencyKey string) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.idempotency[strings.TrimSpace(institutionID)+"|"+strings.TrimSpace(idempotencyKey)]
	if id == "" {
		return nil, ErrNotFound
	}
	return copyOf(m.transfers[id]), nil
}

func (m *memoryStore) ListTransfers(ctx context.Context, institutionID string) ([]Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var transfers []Transfer
	for _, transfer := range m.transfers {
		if transfer.InstitutionID == institutionID {
			transfers = append(transfers, transfer)
		}
	}
	sort.Slice(transfers, func(i, j int) bool { return transfers[i].CreatedAt.After(transfers[j].CreatedAt) })
	return transfers, nil
}

func (m *memoryStore) GetJournal(ctx context.Context, institutionID, journalEntryID string) (*JournalWithPostings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	journal, ok := m.journals[journalEntryID]
	if !ok || journal.InstitutionID != institutionID {
		return nil, ErrNotFound
	}
	return &JournalWithPostings{JournalEntry: journal, Postings: append([]Posting(nil), m.postings[journalEntryID]...), Balanced: journal.TotalDebitMinor == journal.TotalCreditMinor}, nil
}

func (m *memoryStore) ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	options = normalizeListTransactionsOptions(options)
	var txns []Transaction
	for _, transfer := range m.transfers {
		if transfer.InstitutionID != institutionID || transfer.AccountID != accountID {
			continue
		}
		if options.BeforeCreatedAt != nil && !transfer.CreatedAt.Before(*options.BeforeCreatedAt) {
			continue
		}
		signed := int64(0)
		if transfer.Status == TransferStatusSucceeded && transfer.JournalEntryID != nil {
			for _, posting := range m.postings[*transfer.JournalEntryID] {
				if posting.AccountID == accountID {
					account := m.accounts[accountID]
					if (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) || (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) {
						signed = posting.AmountMinor
					} else {
						signed = -posting.AmountMinor
					}
				}
			}
		}
		txns = append(txns, Transaction{ID: transfer.ID, TransferID: transfer.ID, JournalEntryID: transfer.JournalEntryID, AccountID: accountID, Direction: transfer.Direction, Status: transfer.Status, AmountMinor: transfer.AmountMinor, SignedMinor: signed, CurrencyID: transfer.CurrencyID, Narration: transfer.Narration, CreatedAt: transfer.CreatedAt})
	}
	sort.Slice(txns, func(i, j int) bool { return txns[i].CreatedAt.After(txns[j].CreatedAt) })
	if len(txns) > options.Limit {
		txns = txns[:options.Limit]
	}
	return txns, nil
}

func (m *memoryStore) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recordTransferLocked(input)
}

func (m *memoryStore) ReverseTransfer(ctx context.Context, input ReverseTransferInput) (*Transfer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	input.InstitutionID = strings.TrimSpace(input.InstitutionID)
	input.TransferID = strings.TrimSpace(input.TransferID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.InstitutionID == "" || input.TransferID == "" || input.IdempotencyKey == "" {
		return nil, ErrInvalidRequest
	}
	original, ok := m.transfers[input.TransferID]
	if !ok || original.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if original.Status != TransferStatusSucceeded || original.JournalEntryID == nil || original.Direction == TransferDirectionReversal {
		return nil, ErrInvalidRequest
	}
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = original.Provider
	}
	providerReference := strings.TrimSpace(input.ProviderReference)
	if providerReference == "" {
		originalReference := strings.TrimSpace(original.ProviderReference)
		if originalReference == "" {
			originalReference = original.ID
		}
		providerReference = "reversal:" + originalReference
	}
	narration := strings.TrimSpace(input.Narration)
	if narration == "" {
		narration = "Reversal of " + original.ID
	}
	direction := TransferDirectionOutbound
	if original.Direction == TransferDirectionOutbound {
		direction = TransferDirectionInbound
	}
	reversal, err := m.recordTransferLocked(RecordTransferInput{InstitutionID: input.InstitutionID, AccountID: original.AccountID, ClearingAccountID: DemoClearingAccountID, Direction: direction, Status: TransferStatusSucceeded, AmountMinor: original.AmountMinor, CurrencyID: original.CurrencyID, IdempotencyKey: input.IdempotencyKey, Provider: provider, ProviderReference: providerReference, ProviderEventID: strings.TrimSpace(input.ProviderEventID), ReversalOfTransferID: original.ID, FailureReason: strings.TrimSpace(input.FailureReason), Narration: narration})
	if err != nil {
		return nil, err
	}
	if reversal.ReversalOfTransferID == nil || *reversal.ReversalOfTransferID != original.ID {
		return nil, ErrConflict
	}
	reversal.Direction = TransferDirectionReversal
	m.transfers[reversal.ID] = *reversal
	return reversal, nil
}

func (m *memoryStore) recordTransferLocked(input RecordTransferInput) (*Transfer, error) {
	if id := m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey]; id != "" {
		return copyOf(m.transfers[id]), nil
	}
	if input.ProviderEventID != "" {
		if id := m.providerEvents[input.InstitutionID+"|"+input.Provider+"|"+input.ProviderEventID]; id != "" {
			return copyOf(m.transfers[id]), nil
		}
	}
	if input.ProviderReference != "" && input.Status != TransferStatusPending {
		for _, transfer := range m.transfers {
			if transfer.InstitutionID == input.InstitutionID && transfer.Provider == input.Provider && transfer.ProviderReference == input.ProviderReference && transfer.Direction == input.Direction && transfer.Status == TransferStatusPending {
				return m.settlePendingTransferLocked(transfer, input)
			}
		}
	}
	account, ok := m.accounts[input.AccountID]
	if !ok || account.InstitutionID != input.InstitutionID {
		return nil, ErrNotFound
	}
	if _, ok = m.accounts[input.ClearingAccountID]; !ok {
		return nil, ErrNotFound
	}
	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	balance := m.balances[input.AccountID]
	if customerInitiatedOutbound(input) && !canUseAvailableBalance(account, balance.AvailableMinor, input.AmountMinor) {
		status = TransferStatusFailed
		failureReason = "insufficient_funds"
	}
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	if status == TransferStatusSucceeded && wouldCreateReversalDeficit(account, balance, input) {
		ledgerStatus = LedgerStatusReversalDeficit
		reconciliationStatus = ReconciliationStatusManualReview
	}
	now := time.Now().UTC()
	transfer := Transfer{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, AccountID: input.AccountID, Direction: input.Direction, Status: status, ProviderStatus: providerStatus, LedgerStatus: ledgerStatus, ReconciliationStatus: reconciliationStatus, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, IdempotencyKey: input.IdempotencyKey, Provider: input.Provider, ProviderReference: input.ProviderReference, Narration: input.Narration, CreatedAt: now, UpdatedAt: now}
	if input.ProviderEventID != "" {
		transfer.ProviderEventID = &input.ProviderEventID
	}
	if input.ReversalOfTransferID != "" {
		transfer.ReversalOfTransferID = &input.ReversalOfTransferID
	}
	if failureReason != "" {
		transfer.FailureReason = &failureReason
	}
	if status == TransferStatusSucceeded {
		journalID := m.postJournalLocked(input, transfer.ID, now, "")
		transfer.JournalEntryID = &journalID
	}
	m.transfers[transfer.ID] = transfer
	if status == TransferStatusPending && input.Direction == TransferDirectionOutbound && input.ReversalOfTransferID == "" {
		m.createHoldLocked(transfer, now)
	}
	m.idempotency[input.InstitutionID+"|"+input.IdempotencyKey] = transfer.ID
	if input.ProviderEventID != "" {
		m.providerEvents[input.InstitutionID+"|"+input.Provider+"|"+input.ProviderEventID] = transfer.ID
	}
	return copyOf(transfer), nil
}

func (m *memoryStore) settlePendingTransferLocked(pending Transfer, input RecordTransferInput) (*Transfer, error) {
	if pending.AccountID != input.AccountID || pending.AmountMinor != input.AmountMinor || pending.CurrencyID != input.CurrencyID {
		return nil, ErrConflict
	}
	account := m.accounts[pending.AccountID]
	balance := m.balances[pending.AccountID]
	providerStatus := strings.ToLower(strings.TrimSpace(input.ProviderStatus))
	if providerStatus == "" {
		providerStatus = input.Status
	}
	status := input.Status
	if providerStatus == TransferProviderStatusUnknown {
		status = TransferStatusPending
	}
	failureReason := input.FailureReason
	ledgerStatus, reconciliationStatus := transferStatuses(status)
	if providerStatus == TransferProviderStatusUnknown {
		reconciliationStatus = ReconciliationStatusManualReview
	}
	now := time.Now().UTC()
	if status == TransferStatusSucceeded && wouldCreateReversalDeficit(account, balance, input) {
		ledgerStatus = LedgerStatusReversalDeficit
		reconciliationStatus = ReconciliationStatusManualReview
	}
	switch status {
	case TransferStatusSucceeded:
		heldAccountID := ""
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			if hold, ok := m.holds[pending.ID]; !ok || hold.Status != HoldStatusActive {
				return nil, ErrConflict
			}
			heldAccountID = pending.AccountID
		}
		journalID := m.postJournalLocked(input, pending.ID, now, heldAccountID)
		pending.JournalEntryID = &journalID
		if heldAccountID != "" {
			m.consumeHoldLocked(pending.ID, now)
		}
	case TransferStatusFailed:
		if pending.Direction == TransferDirectionOutbound && pending.ReversalOfTransferID == nil {
			m.releaseHoldLocked(pending.ID, now)
		}
	default:
		return nil, ErrInvalidRequest
	}
	pending.Status = status
	pending.ProviderStatus = providerStatus
	pending.LedgerStatus = ledgerStatus
	pending.ReconciliationStatus = reconciliationStatus
	pending.UpdatedAt = now
	if input.ProviderEventID != "" {
		pending.ProviderEventID = &input.ProviderEventID
		m.providerEvents[input.InstitutionID+"|"+input.Provider+"|"+input.ProviderEventID] = pending.ID
	}
	if failureReason != "" {
		pending.FailureReason = &failureReason
	}
	if strings.TrimSpace(input.Narration) != "" {
		pending.Narration = input.Narration
	}
	m.transfers[pending.ID] = pending
	return copyOf(pending), nil
}

func (m *memoryStore) postJournalLocked(input RecordTransferInput, transferID string, now time.Time, heldAccountID string) string {
	journalID := uuid.Must(uuid.NewRandom()).String()
	entryType := input.Direction
	if input.ReversalOfTransferID != "" {
		entryType = TransferDirectionReversal
	}
	journal := JournalEntry{ID: journalID, InstitutionID: input.InstitutionID, TransferID: &transferID, EntryType: entryType, CurrencyID: input.CurrencyID, Narration: input.Narration, TotalDebitMinor: input.AmountMinor, TotalCreditMinor: input.AmountMinor, CreatedAt: now}
	m.journals[journalID] = journal
	debitAccountID := input.ClearingAccountID
	creditAccountID := input.AccountID
	if input.Direction == TransferDirectionOutbound {
		debitAccountID = input.AccountID
		creditAccountID = input.ClearingAccountID
	}
	m.postings[journalID] = []Posting{
		{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: debitAccountID, Direction: PostingDebit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
		{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: input.InstitutionID, JournalEntryID: journalID, AccountID: creditAccountID, Direction: PostingCredit, AmountMinor: input.AmountMinor, CurrencyID: input.CurrencyID, CreatedAt: now},
	}
	for _, posting := range m.postings[journalID] {
		availableDeltaOverride := false
		if posting.AccountID == heldAccountID {
			availableDeltaOverride = true
		}
		m.applyPostingLocked(posting, journalID, now, availableDeltaOverride, 0)
	}
	return journalID
}

func (m *memoryStore) applyPostingLocked(posting Posting, journalID string, now time.Time, availableDeltaOverride bool, availableDelta int64) {
	account := m.accounts[posting.AccountID]
	delta := -posting.AmountMinor
	if (account.NormalBalance == NormalBalanceDebit && posting.Direction == PostingDebit) || (account.NormalBalance == NormalBalanceCredit && posting.Direction == PostingCredit) {
		delta = posting.AmountMinor
	}
	if !availableDeltaOverride {
		availableDelta = delta
	}
	balance := m.balances[posting.AccountID]
	balance.AvailableMinor += availableDelta
	balance.LedgerMinor += delta
	balance.LastJournalEntryID = &journalID
	balance.UpdatedAt = now
	m.balances[posting.AccountID] = balance
}

func (m *memoryStore) createHoldLocked(transfer Transfer, now time.Time) {
	m.holds[transfer.ID] = AccountHold{ID: uuid.Must(uuid.NewRandom()).String(), InstitutionID: transfer.InstitutionID, AccountID: transfer.AccountID, TransferID: transfer.ID, AmountMinor: transfer.AmountMinor, CurrencyID: transfer.CurrencyID, Status: HoldStatusActive, Reason: "pending_outbound_transfer", CreatedAt: now, UpdatedAt: now}
	balance := m.balances[transfer.AccountID]
	balance.AvailableMinor -= transfer.AmountMinor
	balance.UpdatedAt = now
	m.balances[transfer.AccountID] = balance
}

func (m *memoryStore) releaseHoldLocked(transferID string, now time.Time) {
	hold, ok := m.holds[transferID]
	if !ok || hold.Status != HoldStatusActive {
		return
	}
	hold.Status = HoldStatusReleased
	hold.UpdatedAt = now
	hold.ReleasedAt = &now
	m.holds[transferID] = hold
	balance := m.balances[hold.AccountID]
	balance.AvailableMinor += hold.AmountMinor
	balance.UpdatedAt = now
	m.balances[hold.AccountID] = balance
}

func (m *memoryStore) consumeHoldLocked(transferID string, now time.Time) {
	hold := m.holds[transferID]
	hold.Status = HoldStatusConsumed
	hold.UpdatedAt = now
	hold.ReleasedAt = &now
	m.holds[transferID] = hold
}

type spyTransferProvider struct {
	initiateCalls int
	parseCalls    int

	lastInitiate ProviderTransferRequest
	lastHeaders  map[string]string

	initiateResult ProviderTransferResult
	initiateErr    error
	webhookEvent   ProviderWebhookEvent
}

func (s *spyTransferProvider) Name() string {
	return ProviderMockNIP
}

func (s *spyTransferProvider) NameEnquiry(ctx context.Context, request NameEnquiryRequest) (*NameEnquiryResult, error) {
	return nil, ErrInvalidRequest
}

func (s *spyTransferProvider) InitiateTransfer(ctx context.Context, request ProviderTransferRequest) (*ProviderTransferResult, error) {
	s.initiateCalls++
	s.lastInitiate = request
	if s.initiateErr != nil {
		return nil, s.initiateErr
	}
	return copyOf(s.initiateResult), nil
}

func (s *spyTransferProvider) RequeryTransfer(ctx context.Context, providerReference string) (*ProviderTransferResult, error) {
	return nil, ErrNotFound
}

func (s *spyTransferProvider) ParseWebhook(ctx context.Context, payload []byte, headers map[string]string) (*ProviderWebhookEvent, error) {
	s.parseCalls++
	s.lastHeaders = map[string]string{}
	for key, value := range headers {
		s.lastHeaders[key] = value
	}
	return copyOf(s.webhookEvent), nil
}

func copyOf[T any](v T) *T {
	return &v
}
