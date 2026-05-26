package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestListenAndServeErrorHandlingTreatsServerClosedAsNormal(t *testing.T) {
	if shouldLogListenAndServeError(nil) {
		t.Fatal("expected nil error to be ignored")
	}
	if shouldLogListenAndServeError(http.ErrServerClosed) {
		t.Fatal("expected http.ErrServerClosed to be ignored")
	}
	if !shouldLogListenAndServeError(errors.New("bind failed")) {
		t.Fatal("expected unexpected server error to be logged")
	}
}

func TestWriteTimeoutExceedsTimeoutMiddlewareDuration(t *testing.T) {
	if writeTimeout <= timeoutMiddlewareDuration {
		t.Fatalf("writeTimeout (%s) must exceed timeoutMiddlewareDuration (%s)", writeTimeout, timeoutMiddlewareDuration)
	}
}

func TestReadyzReportsReadyWhenDatabasePings(t *testing.T) {
	router := chi.NewRouter()
	registerHealthRoutes(router, fakePinger{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected readyz 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"status": "ready"}` {
		t.Fatalf("unexpected readyz body: %s", rec.Body.String())
	}
}

func TestReadyzReportsUnavailableWhenDatabasePingFails(t *testing.T) {
	router := chi.NewRouter()
	registerHealthRoutes(router, fakePinger{err: errors.New("db unavailable")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected readyz 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"status": "not_ready"}` {
		t.Fatalf("unexpected readyz body: %s", rec.Body.String())
	}
}

type fakePinger struct {
	err error
}

func (p fakePinger) PingContext(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("missing readiness timeout")
	}
	return p.err
}
