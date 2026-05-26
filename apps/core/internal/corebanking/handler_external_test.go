package corebanking

import (
	"context"
	"encoding/json"
	"errors"
	"lenz-core/apps/auth/authn"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

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

func TestMockOutboundRouteConcurrentSameKeyCallsProviderOnce(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	provider := &spyTransferProvider{
		initiateResult: ProviderTransferResult{
			Provider:          ProviderMockNIP,
			ProviderReference: "concurrent-route-out-ref",
			Status:            TransferStatusSucceeded,
			Narration:         "concurrent route outbound",
		},
		initiateDelay: 50 * time.Millisecond,
	}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "concurrent-route-fund",
	})
	journalCountBefore := len(store.journals)

	router := chi.NewRouter()
	NewHandler(svc, WithDemoRoutes(true)).Routes(router)

	const calls = 10
	var wg sync.WaitGroup
	statuses := make([]int, calls)
	transferIDs := make([]string, calls)
	for i := range calls {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			body := `{"account_id":"` + DemoCustomerAccountID + `","amount_minor":10000,"narration":"concurrent generated route"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/transfers/mock/outbound", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "concurrent-route-out")
			req = withTestPrincipal(req, DemoInstitutionID)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			statuses[index] = rec.Code
			if rec.Code == http.StatusOK {
				var transfer Transfer
				if err := json.Unmarshal(rec.Body.Bytes(), &transfer); err == nil {
					transferIDs[index] = transfer.ID
				}
			}
		}(i)
	}
	wg.Wait()

	for index, status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("concurrent mock outbound call %d returned %d, statuses=%v", index, status, statuses)
		}
	}
	firstID := transferIDs[0]
	if firstID == "" {
		t.Fatalf("first concurrent response did not include a transfer id: %+v", transferIDs)
	}
	for index, id := range transferIDs {
		if id != firstID {
			t.Fatalf("concurrent call %d returned transfer %q, want %q; all=%+v", index, id, firstID, transferIDs)
		}
	}
	if provider.initiateCalls != 1 {
		t.Fatalf("expected one provider InitiateTransfer call, got %d", provider.initiateCalls)
	}
	if len(store.journals) != journalCountBefore+1 {
		t.Fatalf("expected one outbound journal effect, before=%d after=%d", journalCountBefore, len(store.journals))
	}
	if countTransfersByIdempotency(store, DemoInstitutionID, "concurrent-route-out") != 1 {
		t.Fatalf("expected one transfer for concurrent idempotency key")
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 40000)
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

func TestExternalNameEnquiryRouteSucceedsAndMatchesSchema(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"` + mockNIPDemoAccountNumber + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected name enquiry to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertResponseMatchesOpenAPISchema(t, req, rec)
	var result ExternalNameEnquiryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Provider != ProviderMockNIP ||
		result.DestinationInstitutionCode != mockNIPDemoBankCode ||
		result.AccountNumber != mockNIPDemoAccountNumber ||
		result.AccountName != mockNIPDemoAccountName ||
		result.Status != NameEnquiryStatusFound ||
		result.Message != "account_found" {
		t.Fatalf("name enquiry response mismatch: %+v", result)
	}
}

func TestExternalNameEnquiryRouteReturnsControlledStatuses(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	tests := []struct {
		name                       string
		destinationInstitutionCode string
		accountNumber              string
		wantStatus                 string
		wantMessage                string
	}{
		{
			name:                       "not found",
			destinationInstitutionCode: mockNIPDemoBankCode,
			accountNumber:              "9990000002",
			wantStatus:                 NameEnquiryStatusNotFound,
			wantMessage:                "account_not_found",
		},
		{
			name:                       "provider unavailable",
			destinationInstitutionCode: mockNIPUnavailableBankCode,
			accountNumber:              mockNIPDemoAccountNumber,
			wantStatus:                 NameEnquiryStatusProviderUnavailable,
			wantMessage:                "provider_unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"destination_institution_code":"` + tt.destinationInstitutionCode + `","account_number":"` + tt.accountNumber + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = withTestPrincipal(req, DemoInstitutionID)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected name enquiry to return 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			var result ExternalNameEnquiryResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != tt.wantStatus || result.Message != tt.wantMessage || result.AccountName != "" {
				t.Fatalf("unexpected controlled name enquiry response: %+v", result)
			}
		})
	}
}

func TestExternalNameEnquiryRouteSanitizesProviderErrors(t *testing.T) {
	store := newMemoryStore()
	provider := &spyTransferProvider{nameEnquiryErr: errors.New("provider password=secret connection failed")}
	svc := NewService(store, provider)
	if _, err := svc.SeedDemo(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"` + mockNIPDemoAccountNumber + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected provider failure to return controlled 200 response, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password=secret") {
		t.Fatalf("raw provider error leaked to client: %s", rec.Body.String())
	}
	var result ExternalNameEnquiryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != NameEnquiryStatusProviderUnavailable || result.Message != "provider_unavailable" {
		t.Fatalf("unexpected provider failure response: %+v", result)
	}
}

func TestExternalNameEnquiryRouteRejectsUnsupportedProvider(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"provider":"unsupported_provider","destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"` + mockNIPDemoAccountNumber + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported provider to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["message"] != "unsupported_provider" {
		t.Fatalf("unexpected unsupported provider response: %+v", response)
	}
}

func TestExternalNameEnquiryRouteRequiresAuth(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	body := `{"destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"` + mockNIPDemoAccountNumber + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalNameEnquiryRouteRejectsMismatchedInstitutionHeader(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"` + mockNIPDemoAccountNumber + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched X-Institution-ID to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalNameEnquiryRouteRejectsInvalidBody(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"destination_institution_code":"` + mockNIPDemoBankCode + `","account_number":"12345"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/name-enquiry", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid body to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalOutboundTransferRouteSucceedsAndMatchesSchema(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-external-outbound-fund",
	})
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"source_account_id":"` + DemoCustomerAccountID + `","destination_institution_code":"` + mockNIPDemoBankCode + `","destination_account_number":"` + mockNIPDemoAccountNumber + `","destination_account_name":"` + mockNIPDemoAccountName + `","amount_minor":12000,"currency_id":"NGN","idempotency_key":"http-external-outbound","narration":"HTTP external outbound","reference":"http-external-outbound-ref","scenario":"success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected external outbound to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertResponseMatchesOpenAPISchema(t, req, rec)
	var result ExternalOutboundTransferResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TransferID == "" ||
		result.SourceAccountID != DemoCustomerAccountID ||
		result.Provider != ProviderMockNIP ||
		result.Status != TransferStatusSucceeded ||
		result.ProviderStatus != TransferStatusSucceeded ||
		result.LedgerStatus != LedgerStatusPosted ||
		result.ReconciliationStatus != ReconciliationStatusMatched ||
		result.JournalEntryID == nil ||
		result.HoldID == nil {
		t.Fatalf("external outbound route response mismatch: %+v", result)
	}
}

func TestExternalOutboundTransferRouteWorksForCreatedAccount(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "HTTPExternal", "Source", "http.external.source@example.com", uniqueAccountNumber("74"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      account.ID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-external-created-source-fund",
	})
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"source_account_id":"` + account.ID + `","destination_institution_code":"` + mockNIPDemoBankCode + `","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":12000,"currency_id":"NGN","idempotency_key":"http-external-created-source","reference":"http-external-created-source-ref","scenario":"success"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected external outbound to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalInboundEventRouteSucceedsReplaysAndMatchesSchema(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"provider_event_id":"http-external-in-event","provider_reference":"http-external-in-ref","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":12000,"currency_id":"NGN","status":"succeeded","sender_name":"HTTP Sender"}`
	first := postExternalInboundEvent(t, router, body, DemoInstitutionID)
	if first.Code != http.StatusOK {
		t.Fatalf("expected external inbound to return 200, got %d body=%s", first.Code, first.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/inbound-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	assertResponseMatchesOpenAPISchema(t, req, first)

	var firstResult ExternalInboundEventResult
	if err := json.Unmarshal(first.Body.Bytes(), &firstResult); err != nil {
		t.Fatal(err)
	}
	if firstResult.TransferID == nil ||
		firstResult.Status != TransferStatusSucceeded ||
		firstResult.ProviderStatus != TransferStatusSucceeded ||
		firstResult.LedgerStatus != LedgerStatusPosted ||
		firstResult.ReconciliationStatus != ReconciliationStatusMatched ||
		firstResult.JournalEntryID == nil {
		t.Fatalf("external inbound route response mismatch: %+v", firstResult)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 12000)

	second := postExternalInboundEvent(t, router, body, DemoInstitutionID)
	if second.Code != http.StatusOK {
		t.Fatalf("expected external inbound replay to return 200, got %d body=%s", second.Code, second.Body.String())
	}
	var secondResult ExternalInboundEventResult
	if err := json.Unmarshal(second.Body.Bytes(), &secondResult); err != nil {
		t.Fatal(err)
	}
	if secondResult.TransferID == nil || *secondResult.TransferID != *firstResult.TransferID {
		t.Fatalf("external inbound replay returned different transfer: first=%+v second=%+v", firstResult, secondResult)
	}
	assertBalance(t, svc, ctx, DemoInstitutionID, DemoCustomerAccountID, 12000)
}

func TestExternalInboundEventRouteConflictReturnsReviewResult(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	firstBody := `{"provider_event_id":"http-external-in-conflict-event","provider_reference":"http-external-in-conflict-ref","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":12000,"status":"succeeded"}`
	first := postExternalInboundEvent(t, router, firstBody, DemoInstitutionID)
	if first.Code != http.StatusOK {
		t.Fatalf("expected external inbound setup to return 200, got %d body=%s", first.Code, first.Body.String())
	}

	conflictBody := `{"provider_event_id":"http-external-in-conflict-event","provider_reference":"http-external-in-conflict-ref","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":13000,"status":"succeeded"}`
	rec := postExternalInboundEvent(t, router, conflictBody, DemoInstitutionID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected external inbound conflict to return 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/inbound-events", strings.NewReader(conflictBody))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	assertResponseMatchesOpenAPISchema(t, req, rec)

	var result ExternalInboundEventResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TransferID == nil ||
		result.Message != "provider_event_payload_conflict" ||
		result.LedgerStatus != LedgerStatusNoPosting ||
		result.ReconciliationStatus != ReconciliationStatusManualReview ||
		result.JournalEntryID != nil {
		t.Fatalf("external inbound conflict response mismatch: %+v", result)
	}
}

func TestExternalInboundEventRouteRequiresAuthAndTenantScope(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	body := `{"provider_event_id":"http-external-in-auth-event","provider_reference":"http-external-in-auth-ref","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":1000,"status":"succeeded"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/inbound-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/inbound-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched tenant to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalInboundEventRouteRejectsInvalidBody(t *testing.T) {
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	body := `{"provider_event_id":"http-external-in-invalid-event","provider_reference":"http-external-in-invalid-ref","destination_account_number":"12345","amount_minor":1000,"status":"succeeded"}`
	rec := postExternalInboundEvent(t, router, body, DemoInstitutionID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid body to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalTransferRequeryRouteOutcomesAndSchema(t *testing.T) {
	tests := []struct {
		name               string
		initialScenario    string
		requeryScenario    string
		wantStatus         string
		wantProviderStatus string
		wantLedgerStatus   string
		wantReconStatus    string
		wantMessage        string
	}{
		{name: "success", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioSuccess, wantStatus: TransferStatusSucceeded, wantProviderStatus: TransferStatusSucceeded, wantLedgerStatus: LedgerStatusPosted, wantReconStatus: ReconciliationStatusMatched, wantMessage: "requery_succeeded"},
		{name: "failed", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioFailed, wantStatus: TransferStatusFailed, wantProviderStatus: TransferStatusFailed, wantLedgerStatus: LedgerStatusNoPosting, wantReconStatus: ReconciliationStatusNoAction, wantMessage: "requery_failed"},
		{name: "pending", initialScenario: MockProviderScenarioPending, requeryScenario: MockProviderScenarioPending, wantStatus: TransferStatusPending, wantProviderStatus: TransferStatusPending, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusPending, wantMessage: "requery_pending"},
		{name: "provider_unknown", initialScenario: MockProviderScenarioProviderUnknown, requeryScenario: MockProviderScenarioTimeout, wantStatus: TransferStatusPending, wantProviderStatus: TransferProviderStatusUnknown, wantLedgerStatus: LedgerStatusPending, wantReconStatus: ReconciliationStatusManualReview, wantMessage: "requery_provider_unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, svc, _ := newTestService(t)
			mustInternalCredit(t, svc, ctx, InternalCreditInput{
				InstitutionID:  DemoInstitutionID,
				AccountID:      DemoCustomerAccountID,
				AmountMinor:    50000,
				CurrencyID:     "NGN",
				IdempotencyKey: "http-requery-" + tt.name + "-fund",
			})
			pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("http-requery-"+tt.name, 10000, tt.initialScenario))
			router := chi.NewRouter()
			NewHandler(svc).Routes(router)

			body := `{"scenario":"` + tt.requeryScenario + `","note":"HTTP requery"}`
			rec := postExternalRequery(t, router, pending.TransferID, body, DemoInstitutionID)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected requery %s to return 200, got %d body=%s", tt.name, rec.Code, rec.Body.String())
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/"+pending.TransferID+"/requery", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = withTestPrincipal(req, DemoInstitutionID)
			assertResponseMatchesOpenAPISchema(t, req, rec)

			var result ExternalTransferRequeryResult
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.TransferID != pending.TransferID ||
				result.Status != tt.wantStatus ||
				result.ProviderStatus != tt.wantProviderStatus ||
				result.LedgerStatus != tt.wantLedgerStatus ||
				result.ReconciliationStatus != tt.wantReconStatus ||
				result.Message != tt.wantMessage {
				t.Fatalf("requery route %s mismatch: %+v", tt.name, result)
			}
		})
	}
}

func TestExternalTransferRequeryRouteNoBodyAlreadyFinalAndInternal(t *testing.T) {
	t.Run("empty body pending requery", func(t *testing.T) {
		ctx, svc, store := newTestService(t)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "http-requery-empty-pending-fund",
		})
		pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("http-requery-empty-pending", 10000, MockProviderScenarioPending))
		journalCountBefore := len(store.journals)
		router := chi.NewRouter()
		NewHandler(svc).Routes(router)

		rec := postExternalRequeryNoBody(t, router, pending.TransferID, DemoInstitutionID)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected empty-body pending requery to return 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var result ExternalTransferRequeryResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Message != "requery_pending" ||
			result.Status != TransferStatusPending ||
			result.ProviderStatus != TransferStatusPending ||
			result.LedgerStatus != LedgerStatusPending ||
			result.ReconciliationStatus != ReconciliationStatusPending {
			t.Fatalf("empty-body pending route mismatch: %+v", result)
		}
		if len(store.journals) != journalCountBefore {
			t.Fatalf("empty-body pending requery changed journal count: before=%d after=%d", journalCountBefore, len(store.journals))
		}
	})

	t.Run("empty body already final no-op", func(t *testing.T) {
		ctx, svc, store := newTestService(t)
		mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    50000,
			CurrencyID:     "NGN",
			IdempotencyKey: "http-requery-final-fund",
		})
		final := externalOutbound(t, svc, ctx, externalOutboundTestInput("http-requery-final", 10000, MockProviderScenarioSuccess))
		journalCountBefore := len(store.journals)
		router := chi.NewRouter()
		NewHandler(svc).Routes(router)

		rec := postExternalRequeryNoBody(t, router, final.TransferID, DemoInstitutionID)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected empty-body final requery to return 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var result ExternalTransferRequeryResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Message != "already_final" || result.Status != TransferStatusSucceeded {
			t.Fatalf("already-final route mismatch: %+v", result)
		}
		if len(store.journals) != journalCountBefore {
			t.Fatalf("already-final requery changed journal count: before=%d after=%d", journalCountBefore, len(store.journals))
		}
	})

	t.Run("internal transfer rejected", func(t *testing.T) {
		ctx, svc, store := newTestService(t)
		internal := mustInternalCredit(t, svc, ctx, InternalCreditInput{
			InstitutionID:  DemoInstitutionID,
			AccountID:      DemoCustomerAccountID,
			AmountMinor:    10000,
			CurrencyID:     "NGN",
			IdempotencyKey: "http-requery-internal",
		})
		journalCountBefore := len(store.journals)
		router := chi.NewRouter()
		NewHandler(svc).Routes(router)

		rec := postExternalRequery(t, router, internal.ID, `{}`, DemoInstitutionID)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected internal transfer requery to return 400, got %d body=%s", rec.Code, rec.Body.String())
		}
		if len(store.journals) != journalCountBefore {
			t.Fatalf("internal transfer requery changed journal count: before=%d after=%d", journalCountBefore, len(store.journals))
		}
	})
}

func TestExternalTransferRequeryRouteRequiresAuthTenantAndValidID(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	ctx, svc, _ := newTestService(t)
	mustInternalCredit(t, svc, ctx, InternalCreditInput{
		InstitutionID:  DemoInstitutionID,
		AccountID:      DemoCustomerAccountID,
		AmountMinor:    50000,
		CurrencyID:     "NGN",
		IdempotencyKey: "http-requery-auth-fund",
	})
	pending := externalOutbound(t, svc, ctx, externalOutboundTestInput("http-requery-auth", 10000, MockProviderScenarioPending))
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/"+pending.TransferID+"/requery", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/"+pending.TransferID+"/requery", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched tenant to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/not-a-uuid/requery", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid transfer id to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExternalOutboundTransferRouteRequiresAuthAndTenantScope(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	body := `{"source_account_id":"` + DemoCustomerAccountID + `","destination_institution_code":"` + mockNIPDemoBankCode + `","destination_account_number":"` + mockNIPDemoAccountNumber + `","amount_minor":1000,"idempotency_key":"http-external-out-auth"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing auth to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/external/transfers/outbound", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched tenant to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
