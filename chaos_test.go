package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFaultInjectionReturns500WhenEnabled(t *testing.T) {
	app, _ := newTestApp(t)
	app.Faults.Set(true)
	called := false
	handler := app.faultInjectionMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/validate", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if called {
		t.Fatal("next handler must not run while fault injection is enabled")
	}
}

func TestFaultInjectionPassesThroughWhenDisabled(t *testing.T) {
	app, _ := newTestApp(t)
	called := false
	handler := app.faultInjectionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/validate", nil))

	if !called || recorder.Code != http.StatusOK {
		t.Fatalf("expected passthrough with 200, got called=%v status=%d", called, recorder.Code)
	}
}

func TestChaosHandlerTogglesFault(t *testing.T) {
	app, _ := newTestApp(t)
	cases := []struct {
		method string
		want   bool
		status int
	}{
		{http.MethodPost, true, http.StatusOK},
		{http.MethodGet, true, http.StatusOK},
		{http.MethodDelete, false, http.StatusOK},
		{http.MethodPut, false, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		app.chaosHandler(recorder, httptest.NewRequest(tc.method, "/admin/chaos", nil))
		if recorder.Code != tc.status {
			t.Fatalf("%s: expected status %d, got %d", tc.method, tc.status, recorder.Code)
		}
		if app.Faults.Enabled() != tc.want {
			t.Fatalf("%s: expected fault=%v, got %v", tc.method, tc.want, app.Faults.Enabled())
		}
	}
}

func TestValidateKeyReturns503OnDatabaseError(t *testing.T) {
	app, mock := newTestApp(t)
	key := "tm_key_any"
	mock.ExpectQuery("SELECT id FROM api_keys WHERE key_hash = \\$1 AND is_active = true").
		WithArgs(hashAPIKey(key)).
		WillReturnError(errors.New("connection refused"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/validate", nil)
	request.Header.Set("Authorization", "Bearer "+key)

	app.validateKeyHandler(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", recorder.Code)
	}
	assertSQLExpectations(t, mock)
}
