package corebanking

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"lenz-core/apps/auth/authn"

	"github.com/go-chi/chi/v5"
)

func TestHTTPAccountControlsEndpoints(t *testing.T) {
	ctx, svc, _ := newTestService(t)
	account := createMemoryCustomerAccount(t, svc, ctx, "HTTP", "Controls", "http.controls@example.com", uniqueAccountNumber("85"))
	counterparty := createMemoryCustomerAccount(t, svc, ctx, "HTTP", "ControlCounterparty", "http.control.counterparty@example.com", uniqueAccountNumber("86"))
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: account.ID, AmountMinor: 20000, CurrencyID: "NGN", IdempotencyKey: "http-controls-funding"})
	mustInternalCredit(t, svc, ctx, InternalCreditInput{InstitutionID: DemoInstitutionID, AccountID: counterparty.ID, AmountMinor: 10000, CurrencyID: "NGN", IdempotencyKey: "http-controls-counterparty-funding"})
	router := chi.NewRouter()
	NewHandler(svc).Routes(router)

	frozen := postHTTPAccount(t, router, account.ID, "/freeze", `{"reference":"freeze-http","reason":"ops freeze"}`)
	if frozen.Status != AccountStatusFrozen {
		t.Fatalf("expected frozen account, got %+v", frozen)
	}
	rec := postHTTPInternalCredit(t, router, account.ID, "http-frozen-credit")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected frozen credit to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPInternalTransfer(t, router, account.ID, counterparty.ID, "http-frozen-transfer-out")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected frozen transfer out to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPInternalTransfer(t, router, counterparty.ID, account.ID, "http-frozen-transfer-in")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected frozen transfer in to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	active := postHTTPAccount(t, router, account.ID, "/unfreeze", `{"reference":"unfreeze-http","reason":"ops clear"}`)
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active account, got %+v", active)
	}

	pnd := postHTTPAccount(t, router, account.ID, "/post-no-debit", `{"reference":"pnd-http","reason":"ops pnd"}`)
	if pnd.Status != AccountStatusPostNoDebit {
		t.Fatalf("expected PND account, got %+v", pnd)
	}
	rec = postHTTPInternalDebit(t, router, account.ID, "http-pnd-debit")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected PND debit to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPInternalCredit(t, router, account.ID, "http-pnd-credit")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected PND credit to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPInternalTransfer(t, router, account.ID, counterparty.ID, "http-pnd-transfer-out")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected PND transfer out to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPInternalTransfer(t, router, counterparty.ID, account.ID, "http-pnd-transfer-in")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected PND transfer in to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	active = deleteHTTPAccount(t, router, account.ID, "/post-no-debit", `{"reference":"pnd-off-http","reason":"ops clear"}`)
	if active.Status != AccountStatusActive {
		t.Fatalf("expected active account after PND removal, got %+v", active)
	}

	lien := postHTTPAccountLien(t, router, account.ID, `{"amount_minor":5000,"currency_id":"NGN","reference":"lien-http","reason":"ops lien"}`)
	if lien.Status != HoldStatusActive || lien.TransferID != nil {
		t.Fatalf("expected active operational lien, got %+v", lien)
	}
	balance := getHTTPBalance(t, router, account.ID)
	if balance.AvailableMinor != 17000 || balance.LedgerMinor != 22000 {
		t.Fatalf("expected lien to reduce available only, got %+v", balance)
	}
	replayedLien := postHTTPAccountLien(t, router, account.ID, `{"amount_minor":5000,"currency_id":"NGN","reference":"lien-http","reason":"ops lien replay"}`)
	if replayedLien.ID != lien.ID {
		t.Fatalf("expected HTTP lien replay to return lien %s, got %s", lien.ID, replayedLien.ID)
	}
	balance = getHTTPBalance(t, router, account.ID)
	if balance.AvailableMinor != 17000 || balance.LedgerMinor != 22000 {
		t.Fatalf("expected same-payload lien replay to leave balance unchanged, got %+v", balance)
	}
	rec = postHTTPAccountLienRequest(t, router, account.ID, `{"amount_minor":6000,"currency_id":"NGN","reference":"lien-http","reason":"changed amount"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected changed-amount lien replay to return 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postHTTPAccountLienRequest(t, router, account.ID, `{"amount_minor":5000,"currency_id":"USD","reference":"lien-http","reason":"changed currency"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected changed-currency lien replay to return 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	balance = getHTTPBalance(t, router, account.ID)
	if balance.AvailableMinor != 17000 || balance.LedgerMinor != 22000 {
		t.Fatalf("expected conflicting lien replays to leave balance unchanged, got %+v", balance)
	}
	released := deleteHTTPAccountLien(t, router, account.ID, lien.ID, `{"reference":"lien-release-http"}`)
	if released.Status != HoldStatusReleased {
		t.Fatalf("expected released lien, got %+v", released)
	}
	balance = getHTTPBalance(t, router, account.ID)
	if balance.AvailableMinor != 22000 || balance.LedgerMinor != 22000 {
		t.Fatalf("expected released lien to restore available, got %+v", balance)
	}
}

func TestHTTPAccountControlsAuthAndValidation(t *testing.T) {
	t.Setenv("LENZ_DEV_AUTH_TOKEN", "test-token")
	t.Setenv("LENZ_DEV_INSTITUTION_ID", DemoInstitutionID)
	_, svc, _ := newTestService(t)
	router := chi.NewRouter()
	router.Use(authn.Authentication(authn.AuthRequiredScope))
	NewHandler(svc).Routes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+DemoCustomerAccountID+"/freeze", bytes.NewBufferString(`{"reference":"freeze","reason":"ops"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing bearer token to return 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+DemoCustomerAccountID+"/freeze", bytes.NewBufferString(`{"reference":"freeze","reason":"ops"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Institution-ID", "99999999-9999-9999-9999-999999999999")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected institution mismatch to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+DemoCustomerAccountID+"/liens", bytes.NewBufferString(`{"amount_minor":0,"currency_id":"NGN","reference":"bad","reason":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid lien request to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func postHTTPAccount(t *testing.T, router http.Handler, accountID, suffix, body string) Account {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID+suffix, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected account control %s to return 200, got %d body=%s", suffix, rec.Code, rec.Body.String())
	}
	var account Account
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	return account
}

func deleteHTTPAccount(t *testing.T, router http.Handler, accountID, suffix, body string) Account {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/"+accountID+suffix, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected account control %s to return 200, got %d body=%s", suffix, rec.Code, rec.Body.String())
	}
	var account Account
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	return account
}

func postHTTPAccountLien(t *testing.T, router http.Handler, accountID, body string) AccountHold {
	t.Helper()
	rec := postHTTPAccountLienRequest(t, router, accountID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected lien placement to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var hold AccountHold
	if err := json.Unmarshal(rec.Body.Bytes(), &hold); err != nil {
		t.Fatal(err)
	}
	return hold
}

func postHTTPAccountLienRequest(t *testing.T, router http.Handler, accountID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID+"/liens", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func deleteHTTPAccountLien(t *testing.T, router http.Handler, accountID, lienID, body string) AccountHold {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/accounts/"+accountID+"/liens/"+lienID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected lien release to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var hold AccountHold
	if err := json.Unmarshal(rec.Body.Bytes(), &hold); err != nil {
		t.Fatal(err)
	}
	return hold
}

func postHTTPInternalCredit(t *testing.T, router http.Handler, accountID, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"account_id":"` + accountID + `","amount_minor":1000,"currency_id":"NGN","idempotency_key":"` + idempotencyKey + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/credits", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postHTTPInternalDebit(t *testing.T, router http.Handler, accountID, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"account_id":"` + accountID + `","amount_minor":1000,"currency_id":"NGN","idempotency_key":"` + idempotencyKey + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/debits", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postHTTPInternalTransfer(t *testing.T, router http.Handler, sourceAccountID, destinationAccountID, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"source_account_id":"` + sourceAccountID + `","destination_account_id":"` + destinationAccountID + `","amount_minor":1000,"currency_id":"NGN","idempotency_key":"` + idempotencyKey + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/internal/transfers", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withTestPrincipal(req, DemoInstitutionID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
