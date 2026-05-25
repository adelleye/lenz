package corebanking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"lenz-core/apps/auth/authn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/go-chi/chi/v5"
)

func TestDemoRoutesAreDisabledByDefault(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/seed", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected disabled demo route to return 404, got %d", rec.Code)
	}
}

func TestMockRoutesRequireAuthenticatedPrincipal(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "missing-institution")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing principal to return 401, got %d", rec.Code)
	}
}

func TestMockRoutesRejectBodyInstitutionMismatch(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"institution_id":"99999999-9999-9999-9999-999999999999","account_id":"` + DemoCustomerAccountID + `","amount_minor":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "institution-mismatch")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected institution mismatch to return 403, got %d", rec.Code)
	}
}

func TestCoreRoutesDeriveInstitutionFromAuthenticatedPrincipal(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected principal-scoped request without X-Institution-ID to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var balance AccountBalance
	if err := json.Unmarshal(rec.Body.Bytes(), &balance); err != nil {
		t.Fatal(err)
	}
	if balance.InstitutionID != DemoInstitutionID {
		t.Fatalf("handler did not use principal institution scope: %+v", balance)
	}
}

func TestMismatchedInstitutionHeaderIsRejected(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched X-Institution-ID to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPrincipalCannotMutateAnotherInstitution(t *testing.T) {
	store := newMemoryStore()
	provider := &spyTransferProvider{}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"institution_id":"99999999-9999-9999-9999-999999999999","account_id":"` + DemoCustomerAccountID + `","amount_minor":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "cross-tenant-mutate")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant mutation attempt to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if provider.initiateCalls != 0 {
		t.Fatalf("cross-tenant request should be rejected before provider call, got %d calls", provider.initiateCalls)
	}
}

func TestInternalErrorsAreSanitized(t *testing.T) {
	store := &failingBalanceStore{memoryStore: newMemoryStore()}
	svc := NewService(store, NewMockNIPProvider())
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
	req.Header.Set("X-Request-ID", "req-test-500")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "database password=secret") {
		t.Fatalf("raw internal error leaked to client: %s", rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "internal_server_error" || body["request_id"] != "req-test-500" {
		t.Fatalf("unexpected sanitized error body: %+v", body)
	}
}

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

func TestGetAccountBalanceRouteRequiresAuth(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateAccountRouteRequiresAuth(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	body := `{"customer_id":"` + DemoCustomerID + `","account_number":"1234567890","name":"Ada Main Wallet"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateAccountRouteRejectsMismatchedInstitutionHeader(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"customer_id":"` + DemoCustomerID + `","account_number":"1234567890","name":"Ada Main Wallet"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched X-Institution-ID to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateAccountRouteRejectsInvalidRequestBody(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"customer_id":"` + DemoCustomerID + `","account_number":"12345","name":"Ada Main Wallet"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid account request to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCustomerCreateInternalErrorsAreSanitized(t *testing.T) {
	store := &failingCreateCustomerStore{memoryStore: newMemoryStore()}
	svc := NewService(store, NewMockNIPProvider())
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"branch_id":"` + DemoBranchID + `","customer_type":"individual","first_name":"Adaeze","last_name":"Okafor","email":"adaeze@example.com","phone":"+2348012345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/customers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-customer-500")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "database password=secret") {
		t.Fatalf("raw internal error leaked to client: %s", rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["message"] != "internal_server_error" || response["request_id"] != "req-customer-500" {
		t.Fatalf("unexpected sanitized error body: %+v", response)
	}
}

func TestGeneratedMockOutboundRouteCallsService(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "generated-route-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "generated route outbound",
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
		IdempotencyKey:    "generated-route-fund",
		Provider:          "test_setup",
		ProviderReference: "generated-route-fund-ref",
		ProviderEventID:   "generated-route-fund-event",
		Narration:         "generated route funding",
	}); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":10000,"narration":"through generated route"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "generated-route-out")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected generated route to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected generated route to call provider-backed service once, got %d", provider.initiateCalls)
	}
	var transfer Transfer
	if err := json.Unmarshal(rec.Body.Bytes(), &transfer); err != nil {
		t.Fatal(err)
	}
	if transfer.ProviderReference != "generated-route-out-ref" || transfer.AmountMinor != 10000 {
		t.Fatalf("generated route returned wrong transfer: %+v", transfer)
	}
}

func TestInvalidMockOutboundRequestRejectsBeforeServiceExecution(t *testing.T) {
	store := newMemoryStore()
	provider := &spyTransferProvider{}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "invalid-generated-route")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid request to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if provider.initiateCalls != 0 {
		t.Fatalf("invalid request should be rejected before service/provider execution, got %d provider calls", provider.initiateCalls)
	}
}

func withTestPrincipal(req *http.Request, institutionID string) *http.Request {
	return authn.RequestWithPrincipal(req, authn.Principal{
		InstitutionID: institutionID,
		Roles:         []string{"test"},
		Scopes:        []string{"corebanking:read", "corebanking:write"},
	})
}

func assertResponseMatchesOpenAPISchema(t *testing.T, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()

	swagger, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	swagger.Servers = nil

	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		t.Fatal(err)
	}
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatal(err)
	}
	result := rec.Result()
	defer result.Body.Close()

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: rec.Code,
		Header: result.Header,
		Body:   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
	}
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Fatalf("response does not match OpenAPI schema: %v", err)
	}
}

type failingBalanceStore struct {
	*memoryStore
}

func (s *failingBalanceStore) GetBalance(ctx context.Context, institutionID, accountID string) (*AccountBalance, error) {
	return nil, errors.New("database password=secret connection failed")
}

type failingCreateCustomerStore struct {
	*memoryStore
}

func (s *failingCreateCustomerStore) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	return nil, errors.New("database password=secret connection failed")
}
