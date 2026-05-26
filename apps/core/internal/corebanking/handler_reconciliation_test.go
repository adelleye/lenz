package corebanking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lenz-core/apps/auth/authn"

	"github.com/go-chi/chi/v5"
)

func TestHTTPReconciliationQueueListFilterDetailAndMarkReviewed(t *testing.T) {
	ctx, svc, store := newTestService(t)
	normal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-recon-normal",
	})
	providerUnknown := createProviderUnknownTransfer(t, store, ctx, "http-recon-provider")
	reversal := createReversalDeficitTransfer(t, svc, ctx, "http-recon-reversal")
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	rec := getHTTPReconciliation(t, router, "/api/v1/admin/reconciliation-items")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected queue list to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var items []ReconciliationItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	assertMissingReconciliationItem(t, items, normal.ID)
	assertReconciliationItem(t, items, providerUnknown.ID, "provider_unknown", ReconciliationActionRequeryProvider)
	assertReconciliationItem(t, items, reversal.ID, "reversal_deficit", ReconciliationActionManualCustomerReceivableReview)

	rec = getHTTPReconciliation(t, router, "/api/v1/admin/reconciliation-items?provider_status=provider_unknown")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected filtered queue to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	items = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TransferID != providerUnknown.ID {
		t.Fatalf("provider_status filter mismatch: %+v", items)
	}

	rec = getHTTPReconciliation(t, router, "/api/v1/admin/reconciliation-items/"+reversal.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected detail to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var item ReconciliationItem
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.TransferID != reversal.ID || item.ReviewReason != "reversal_deficit" {
		t.Fatalf("detail response mismatch: %+v", item)
	}

	rec = postHTTPReconciliation(t, router, "/api/v1/admin/reconciliation-items/"+reversal.ID+"/mark-reviewed", `{"resolution_note":"HTTP ops reviewed","resolution_status":"reviewed"}`, DemoInstitutionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected mark-reviewed to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	item = ReconciliationItem{}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ReviewStatus == nil || *item.ReviewStatus != ReconciliationReviewStatusReviewed || item.ReviewNote == nil || *item.ReviewNote != "HTTP ops reviewed" {
		t.Fatalf("mark-reviewed response missing review metadata: %+v", item)
	}
}

func TestHTTPReconciliationQueueEmptyListIsArray(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	rec := getHTTPReconciliation(t, router, "/api/v1/admin/reconciliation-items")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected empty queue to serialize as [], got %q", rec.Body.String())
	}
}

func TestHTTPReconciliationAuthTenantAndValidation(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	ctx, svc, store := newTestService(t)
	providerUnknown := createProviderUnknownTransfer(t, store, ctx, "http-recon-auth")
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)
	plainRouter := chi.NewRouter()
	NewHandler(svc).Routes(plainRouter)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reconciliation-items", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reconciliation-items", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched institution header to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reconciliation-items/"+providerUnknown.ID, nil)
	req = withTestPrincipal(req, "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	plainRouter.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-tenant detail lookup to return 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reconciliation-items?limit=abc", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid query to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = postHTTPReconciliation(t, plainRouter, "/api/v1/admin/reconciliation-items/"+providerUnknown.ID+"/mark-reviewed", `{"resolution_note":"","resolution_status":"reviewed"}`, DemoInstitutionID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing resolution note to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPReconciliationInternalErrorsAreSanitized(t *testing.T) {
	store := &failingReconciliationListStore{memoryStore: newMemoryStore()}
	svc := NewService(store, NewMockNIPProvider())
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reconciliation-items", nil)
	req.Header.Set("X-Request-ID", "req-recon-500")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password=secret") {
		t.Fatalf("raw reconciliation error leaked to client: %s", rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["message"] != "internal_server_error" || response["request_id"] != "req-recon-500" {
		t.Fatalf("unexpected sanitized error body: %+v", response)
	}
}

func getHTTPReconciliation(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type failingReconciliationListStore struct {
	*memoryStore
}

func (s *failingReconciliationListStore) ListReconciliationItems(ctx context.Context, institutionID string, options ListReconciliationItemsOptions) ([]ReconciliationItem, error) {
	return nil, errors.New("database password=secret connection failed")
}

func postHTTPReconciliation(t *testing.T, router http.Handler, path, body, institutionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, institutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
