package corebanking

import (
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

func TestHTTPTransactionHistoryEmptyListIsArray(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "HTTP", "Empty", "http.empty@example.com", uniqueAccountNumber("72"))
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+account.ID+"/transactions", nil)
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected empty history to serialize as [], got %q", rec.Body.String())
	}
}

func TestHTTPTransactionHistoryDirections(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)
	destination := createMemoryCustomerAccount(t, svc, ctx, "HTTP", "Receiver", "http.receiver@example.com", uniqueAccountNumber("73"))

	credit := mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    30000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-history-credit",
		Reference:      "http-history-credit-ref",
	})
	debit := mustInternalDebit(t, svc, ctx, InternalDebitInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    5000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-history-debit",
		Reference:      "http-history-debit-ref",
	})
	transfer := mustInternalTransfer(t, svc, ctx, InternalTransferInput{
		InstitutionID:        DemoInstitutionID,
		SourceAccountID:      DemoCustomerAccountID,
		DestinationAccountID: destination.ID,
		AmountMinor:          4000,
		CurrencyID:           "NGN",
		IdempotencyKey:       "http-history-transfer",
		Reference:            "http-history-transfer-ref",
	})

	sourceHistory := getHTTPTransactions(t, router, DemoCustomerAccountID)
	assertHistoryRow(t, sourceHistory, credit.ID, TransactionDirectionCredit, 30000, credit.JournalEntryID, ProviderLedgerInternal, "http-history-credit-ref", nil)
	assertHistoryRow(t, sourceHistory, debit.ID, TransactionDirectionDebit, -5000, debit.JournalEntryID, ProviderLedgerInternal, "http-history-debit-ref", nil)
	assertHistoryRow(t, sourceHistory, transfer.ID, TransactionDirectionDebit, -4000, transfer.JournalEntryID, ProviderLedgerInternal, "http-history-transfer-ref", &destination.ID)

	destinationHistory := getHTTPTransactions(t, router, destination.ID)
	sourceAccountID := DemoCustomerAccountID
	assertHistoryRow(t, destinationHistory, transfer.ID, TransactionDirectionCredit, 4000, transfer.JournalEntryID, ProviderLedgerInternal, "http-history-transfer-ref", &sourceAccountID)
}

func TestHTTPTransactionHistoryInvalidAndUnauthorizedRequests(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/transactions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing bearer token to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/transactions?limit=abc", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid limit to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/transactions", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched institution header to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPTransactionHistoryCrossTenantAccountNotFound(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/transactions", nil)
	req = withTestPrincipal(req, "99999999-9999-9999-9999-999999999999")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-tenant account history to return 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "not_found" {
		t.Fatalf("expected controlled not_found response, got %+v", body)
	}
}

func TestHTTPTransactionHistoryInternalErrorsAreSanitized(t *testing.T) {
	store := &failingTransactionHistoryStore{memoryStore: newMemoryStore()}
	svc := NewService(store, NewMockNIPProvider())
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+DemoCustomerAccountID+"/transactions", nil)
	req.Header.Set("X-Request-ID", "req-history-500")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password=secret") {
		t.Fatalf("history response leaked internal error: %s", rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["message"] != "internal_server_error" || body["request_id"] != "req-history-500" {
		t.Fatalf("expected sanitized internal error with request id, got %+v", body)
	}
}

type failingTransactionHistoryStore struct {
	*memoryStore
}

func (s *failingTransactionHistoryStore) ListTransactions(ctx context.Context, institutionID, accountID string, options ListTransactionsOptions) ([]Transaction, error) {
	return nil, errors.New("database password=secret connection failed")
}
