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
)

func postExternalInboundEvent(t *testing.T, router http.Handler, body, institutionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/inbound-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, institutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postExternalRequery(t *testing.T, router http.Handler, transferID, body, institutionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/"+transferID+"/requery", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, institutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postExternalRequeryNoBody(t *testing.T, router http.Handler, transferID, institutionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/"+transferID+"/requery", nil)
	req = withTestPrincipal(req, institutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func countTransfersByIdempotency(store *memoryStore, institutionID, idempotencyKey string) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for _, transfer := range store.transfers {
		if transfer.InstitutionID == institutionID && transfer.IdempotencyKey == idempotencyKey {
			count++
		}
	}
	return count
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

func getHTTPBalance(t *testing.T, router http.Handler, accountID string) AccountBalance {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+accountID+"/balance", nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected balance read to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var balance AccountBalance
	if err := json.Unmarshal(rec.Body.Bytes(), &balance); err != nil {
		t.Fatal(err)
	}
	return balance
}

func getHTTPTransactions(t *testing.T, router http.Handler, accountID string) []Transaction {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+accountID+"/transactions", nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected transaction history to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var history []Transaction
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	return history
}

func getHTTPAdminTransfers(t *testing.T, router http.Handler, path, institutionID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = withTestPrincipal(req, institutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeHTTPTransfers(t *testing.T, rec *httptest.ResponseRecorder) []Transfer {
	t.Helper()
	var transfers []Transfer
	if err := json.Unmarshal(rec.Body.Bytes(), &transfers); err != nil {
		t.Fatal(err)
	}
	return transfers
}

func assertHTTPErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantMessage, wantRequestID string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("expected status %d, got %d body=%s", wantStatus, rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON error response content type, got %q", contentType)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != wantMessage {
		t.Fatalf("expected error message %q, got body=%+v", wantMessage, body)
	}
	if body["request_id"] != wantRequestID {
		t.Fatalf("expected request_id %q, got body=%+v", wantRequestID, body)
	}
	if len(body) != 2 {
		t.Fatalf("expected only message and request_id in error body, got %+v", body)
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

type failingRecordTransferStore struct {
	*memoryStore
}

func (s *failingRecordTransferStore) RecordTransfer(ctx context.Context, input RecordTransferInput) (*Transfer, error) {
	return nil, errors.New("database password=secret connection failed")
}
