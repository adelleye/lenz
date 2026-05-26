package server

import (
	"errors"
	"net/http"
	"testing"
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
