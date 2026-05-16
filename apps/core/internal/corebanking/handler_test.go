package corebanking

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestMockRoutesRequireInstitutionHeader(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "missing-institution")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing institution scope to return 400, got %d", rec.Code)
	}
}

func TestMockRoutesRejectInstitutionMismatch(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	body := `{"institution_id":"99999999-9999-9999-9999-999999999999","account_id":"` + DemoCustomerAccountID + `","amount_minor":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Institution-ID", DemoInstitutionID)
	req.Header.Set("Idempotency-Key", "institution-mismatch")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected institution mismatch to return 400, got %d", rec.Code)
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
	req.Header.Set("X-Institution-ID", DemoInstitutionID)
	req.Header.Set("Idempotency-Key", "generated-route-out")
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
	req.Header.Set("X-Institution-ID", DemoInstitutionID)
	req.Header.Set("Idempotency-Key", "invalid-generated-route")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid request to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if provider.initiateCalls != 0 {
		t.Fatalf("invalid request should be rejected before service/provider execution, got %d provider calls", provider.initiateCalls)
	}
}
