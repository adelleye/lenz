package corebanking

import (
	"encoding/json"
	"lenz-core/apps/auth/authn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCreateCustomerRouteCreatesAndGetsCustomer(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"branch_id":"` + DemoBranchID + `","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze@example.com","phone":"+2348012345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected customer create to return 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.InstitutionID != DemoInstitutionID || created.BranchID != DemoBranchID || created.CustomerType != CustomerTypeIndividual || created.Email != "adaeze@example.com" {
		t.Fatalf("created customer response has wrong scope/data: %+v", created)
	}
	if created.KYCTier != CustomerKYCTier1 || created.BVNStatus != CustomerIdentityStatusNotCollected || created.NINStatus != CustomerIdentityStatusNotCollected {
		t.Fatalf("created customer response has wrong identity defaults: %+v", created)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+created.ID, nil)
	getReq = withTestPrincipal(getReq, DemoInstitutionID)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get customer to return 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got Customer
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.InstitutionID != DemoInstitutionID {
		t.Fatalf("get customer response mismatch: got %+v created %+v", got, created)
	}
}

func TestCreateCustomerRouteRejectsInvalidInput(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"branch_id":"` + DemoBranchID + `","customer_type":"individual","first_name":"","last_name":"Okafor","email":"adaeze@example.com","phone":"+2348012345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid customer request to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateCustomerRouteRequiresAuth(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	body := `{"branch_id":"` + DemoBranchID + `","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze@example.com","phone":"+2348012345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateCustomerRouteRejectsMismatchedInstitutionHeader(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"branch_id":"` + DemoBranchID + `","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze@example.com","phone":"+2348012345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched X-Institution-ID to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetCustomerRouteDeniesCrossInstitutionRead(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "Adaeze",
		LastName:      "Okafor",
		Email:         "adaeze@example.com",
		Phone:         "+2348012345678",
	})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+customer.ID, nil)
	req = withTestPrincipal(req, "99999999-9999-9999-9999-999999999999")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-institution read to return 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateAccountRouteCreatesGetsAndListsAccount(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"customer_id":"` + DemoCustomerID + `","account_number":"1234567890","name":"Ada Main Wallet","product_type":"standard_wallet","currency_id":"NGN"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected account create to return 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created Account
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.InstitutionID != DemoInstitutionID || created.CustomerID == nil || *created.CustomerID != DemoCustomerID || created.AccountNumber != "1234567890" {
		t.Fatalf("created account response has wrong scope/data: %+v", created)
	}
	if created.Kind != AccountKindCustomer || created.ProductType != AccountProductStandardWallet || created.AllowNegative || created.CurrencyID != "NGN" || created.NormalBalance != NormalBalanceCredit || created.Status != "active" {
		t.Fatalf("created account response has wrong defaults: %+v", created)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+created.ID, nil)
	getReq = withTestPrincipal(getReq, DemoInstitutionID)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get account to return 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got Account
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.AccountNumber != created.AccountNumber {
		t.Fatalf("get account response mismatch: got %+v created %+v", got, created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+DemoCustomerID+"/accounts", nil)
	listReq = withTestPrincipal(listReq, DemoInstitutionID)
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected customer account list to return 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var accounts []Account
	if err := json.Unmarshal(listRec.Body.Bytes(), &accounts); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, account := range accounts {
		if account.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("customer account list did not include created account: %+v", accounts)
	}
}

func TestCustomerAccountsRouteReturnsEmptyList(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	customer, err := svc.CreateCustomer(ctx, CreateCustomerInput{
		InstitutionID: DemoInstitutionID,
		BranchID:      DemoBranchID,
		CustomerType:  CustomerTypeIndividual,
		FirstName:     "No",
		LastName:      "Accounts",
		Email:         "no.accounts@example.com",
		Phone:         "+2348012345000",
	})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/customers/"+customer.ID+"/accounts", nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected account list to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected empty list to encode as [], got %s", rec.Body.String())
	}
}

func TestGetAccountBalanceRouteReturnsNewAccountZeroBalanceAndMatchesSchema(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account, err := svc.CreateAccount(ctx, CreateAccountInput{
		InstitutionID: DemoInstitutionID,
		CustomerID:    DemoCustomerID,
		AccountNumber: "1234567894",
		Name:          "Ada Balance Wallet",
		ProductType:   AccountProductStandardWallet,
		CurrencyID:    "NGN",
	})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+account.ID+"/balance", nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected balance read to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response content type, got %q", contentType)
	}
	assertResponseMatchesOpenAPISchema(t, req, rec)

	var balance AccountBalance
	if err := json.Unmarshal(rec.Body.Bytes(), &balance); err != nil {
		t.Fatal(err)
	}
	if balance.AccountID != account.ID || balance.InstitutionID != DemoInstitutionID || balance.AvailableMinor != 0 || balance.LedgerMinor != 0 || balance.CurrencyID != "NGN" || balance.LastJournalEntryID != nil {
		t.Fatalf("new account balance response mismatch: %+v", balance)
	}
}

func TestGetAccountBalanceRouteDeniesCrossInstitutionRead(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
	req = withTestPrincipal(req, "99999999-9999-9999-9999-999999999999")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-institution balance read to return 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
