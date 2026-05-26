package corebanking

import (
	"context"
	"encoding/json"
	"lenz-core/apps/auth/authn"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

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

func TestHTTPAdminTransferListPaginationAndEmptyArray(t *testing.T) {
	_, svc, store := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	rec := getHTTPAdminTransfers(t, router, "/api/v1/admin/transfers", DemoInstitutionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected empty transfer list to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected empty transfer list to serialize as [], got %q", rec.Body.String())
	}

	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	putMemoryTransferForList(t, store, numberedTestUUID("33333333-3333-3333-3333", 1), DemoInstitutionID, base)
	putMemoryTransferForList(t, store, numberedTestUUID("33333333-3333-3333-3333", 2), DemoInstitutionID, base.Add(time.Minute))
	putMemoryTransferForList(t, store, numberedTestUUID("33333333-3333-3333-3333", 3), DemoInstitutionID, base.Add(time.Minute))
	putMemoryTransferForList(t, store, numberedTestUUID("99999999-9999-9999-9999", 3), "99999999-9999-9999-9999-999999999999", base.Add(10*time.Minute))

	rec = getHTTPAdminTransfers(t, router, "/api/v1/admin/transfers?limit=2", DemoInstitutionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected transfer list page to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	firstPage := decodeHTTPTransfers(t, rec)
	if len(firstPage) != 2 {
		t.Fatalf("expected first page of two transfers, got %+v", firstPage)
	}
	assertTransfersNewestFirst(t, firstPage)
	if firstPage[0].ID != numberedTestUUID("33333333-3333-3333-3333", 3) || firstPage[1].ID != numberedTestUUID("33333333-3333-3333-3333", 2) {
		t.Fatalf("expected HTTP list tie-breaker by transfer id desc, got %+v", firstPage)
	}

	cursor := url.QueryEscape(firstPage[len(firstPage)-1].CreatedAt.Format(time.RFC3339Nano))
	rec = getHTTPAdminTransfers(t, router, "/api/v1/admin/transfers?limit=2&before_created_at="+cursor+"&before_transfer_id="+firstPage[len(firstPage)-1].ID, DemoInstitutionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected second transfer list page to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	secondPage := decodeHTTPTransfers(t, rec)
	assertNoDuplicateTransfers(t, append(firstPage, secondPage...))
	assertTransferListMissing(t, append(firstPage, secondPage...), numberedTestUUID("99999999-9999-9999-9999", 3))

	rec = getHTTPAdminTransfers(t, router, "/api/v1/admin/transfers?before_transfer_id="+firstPage[0].ID, DemoInstitutionID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected cursor without created_at to return 400, got %d body=%s", rec.Code, rec.Body.String())
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

func TestCoreBankingErrorResponsesIncludeRequestID(t *testing.T) {
	newRouter := func(svc *Service) http.Handler {
		router := chi.NewRouter()
		NewHandler(svc).Routes(router)
		return router
	}
	newSeededRouter := func(t *testing.T) http.Handler {
		t.Helper()
		_, svc, _ := newTestService(t)
		return newRouter(svc)
	}

	tests := []struct {
		name        string
		requestID   string
		wantStatus  int
		wantMessage string
		mustNotLeak string
		setup       func(t *testing.T) (http.Handler, *http.Request)
	}{
		{
			name:        "openapi request error",
			requestID:   "req-openapi-request-error",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid_request",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/not-a-uuid/balance", nil)
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "invalid request",
			requestID:   "req-invalid-request",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "invalid_request",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":0,"currency_id":"NGN","idempotency_key":"request-id-invalid"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/credits", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "unauthorized",
			requestID:   "req-unauthorized",
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "unauthorized",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
				return router, req
			},
		},
		{
			name:        "forbidden tenant mismatch",
			requestID:   "req-forbidden-tenant-mismatch",
			wantStatus:  http.StatusForbidden,
			wantMessage: "forbidden",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
				req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "not found",
			requestID:   "req-not-found",
			wantStatus:  http.StatusNotFound,
			wantMessage: "not_found",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/99999999-9999-9999-9999-999999999999/balance", nil)
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "conflict",
			requestID:   "req-conflict",
			wantStatus:  http.StatusConflict,
			wantMessage: "conflict",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				body := `{"customer_id":"` + DemoCustomerID + `","account_number":"9990000001","name":"Duplicate Wallet","product_type":"standard_wallet","currency_id":"NGN"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "insufficient funds",
			requestID:   "req-insufficient-funds",
			wantStatus:  http.StatusUnprocessableEntity,
			wantMessage: "insufficient_funds",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				router := newSeededRouter(t)
				body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":10000,"currency_id":"NGN","idempotency_key":"request-id-insufficient"}`
				req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/debits", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
		{
			name:        "internal error",
			requestID:   "req-internal-error",
			wantStatus:  http.StatusInternalServerError,
			wantMessage: "internal_server_error",
			mustNotLeak: "database password=secret",
			setup: func(t *testing.T) (http.Handler, *http.Request) {
				t.Helper()
				store := &failingBalanceStore{memoryStore: newMemoryStore()}
				svc := NewService(store, NewMockNIPProvider())
				router := newRouter(svc)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/balance", nil)
				req = withTestPrincipal(req, DemoInstitutionID)
				return router, req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, req := tt.setup(t)
			req.Header.Set("X-Request-ID", tt.requestID)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assertHTTPErrorResponse(t, rec, tt.wantStatus, tt.wantMessage, tt.requestID)
			if tt.mustNotLeak != "" && strings.Contains(rec.Body.String(), tt.mustNotLeak) {
				t.Fatalf("raw internal error leaked to client: %s", rec.Body.String())
			}
		})
	}
}
