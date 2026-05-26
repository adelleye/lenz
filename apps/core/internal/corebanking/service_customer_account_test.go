package corebanking

import (
	"errors"
	"fmt"
	"testing"
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
			name:  "short account number",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "12345", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
		},
		{
			name:  "long account number",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "12345678901", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
		},
		{
			name:  "non-digit account number",
			input: CreateAccountInput{InstitutionID: DemoInstitutionID, CustomerID: DemoCustomerID, AccountNumber: "12345abc90", Name: "Ada Wallet", ProductType: AccountProductStandardWallet, CurrencyID: "NGN"},
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

func TestBalanceEnquiryRejectsMissingCrossTenantAndInvalidAccount(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	if _, err := svc.GetBalance(ctx, DemoInstitutionID, "99999999-9999-9999-9999-999999999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing account balance read to fail as not found, got %v", err)
	}
	if _, err := svc.GetBalance(ctx, "99999999-9999-9999-9999-999999999999", DemoCustomerAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected cross-tenant balance read to fail as not found, got %v", err)
	}
	if _, err := svc.GetBalance(ctx, DemoInstitutionID, "not-a-uuid"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid account id to fail validation, got %v", err)
	}
}

func TestTransferAndJournalReadsRejectInvalidUUID(t *testing.T) {
	ctx, svc, _ := newTestService(t)

	if _, err := svc.GetTransfer(ctx, DemoInstitutionID, "not-a-uuid"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid transfer id to fail validation, got %v", err)
	}
	if _, err := svc.GetJournal(ctx, DemoInstitutionID, "not-a-uuid"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid journal id to fail validation, got %v", err)
	}
	if _, err := svc.ReverseTransfer(ctx, DemoInstitutionID, "not-a-uuid", "reverse-invalid-id"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid reversal transfer id to fail validation, got %v", err)
	}
	_, err := svc.recordProviderWebhookEvent(ctx, ProviderWebhookEvent{
		Provider:             ProviderMockNIP,
		InstitutionID:        DemoInstitutionID,
		Direction:            TransferDirectionReversal,
		Status:               TransferStatusSucceeded,
		IdempotencyKey:       "provider-reversal-invalid-id",
		ProviderEventID:      "provider-reversal-invalid-event",
		ReversalOfTransferID: "not-a-uuid",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid provider reversal id to fail validation, got %v", err)
	}
}
