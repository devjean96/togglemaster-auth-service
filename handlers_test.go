package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newTestApp(t *testing.T) (*App, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("creating mock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &App{DB: db, MasterKey: "master-secret"}, mock
}

func assertSQLExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectation: %v", err)
	}
}

func TestHealthHandler(t *testing.T) {
	app := &App{}
	recorder := httptest.NewRecorder()

	app.healthHandler(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if body := recorder.Body.String(); body != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestValidateKeyRequiresAuthorization(t *testing.T) {
	app, _ := newTestApp(t)
	recorder := httptest.NewRecorder()

	app.validateKeyHandler(recorder, httptest.NewRequest(http.MethodGet, "/validate", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}
}

func TestValidateKeyAcceptsActiveKey(t *testing.T) {
	app, mock := newTestApp(t)
	key := "tm_key_valid"
	mock.ExpectQuery("SELECT id FROM api_keys WHERE key_hash = \\$1 AND is_active = true").
		WithArgs(hashAPIKey(key)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/validate", nil)
	request.Header.Set("Authorization", "Bearer "+key)

	app.validateKeyHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	assertSQLExpectations(t, mock)
}

func TestValidateKeyRejectsUnknownKey(t *testing.T) {
	app, mock := newTestApp(t)
	key := "unknown"
	mock.ExpectQuery("SELECT id FROM api_keys WHERE key_hash = \\$1 AND is_active = true").
		WithArgs(hashAPIKey(key)).
		WillReturnError(sql.ErrNoRows)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/validate", nil)
	request.Header.Set("Authorization", "Bearer "+key)

	app.validateKeyHandler(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}
	assertSQLExpectations(t, mock)
}

func TestCreateKeyRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
		status int
	}{
		{name: "method", method: http.MethodGet, status: http.StatusMethodNotAllowed},
		{name: "malformed JSON", method: http.MethodPost, body: "{", status: http.StatusBadRequest},
		{name: "missing name", method: http.MethodPost, body: `{}`, status: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app, _ := newTestApp(t)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, "/admin/keys", strings.NewReader(test.body))

			app.createKeyHandler(recorder, request)

			if recorder.Code != test.status {
				t.Fatalf("expected status %d, got %d", test.status, recorder.Code)
			}
		})
	}
}

func TestCreateKey(t *testing.T) {
	app, mock := newTestApp(t)
	mock.ExpectQuery("INSERT INTO api_keys \\(name, key_hash\\) VALUES \\(\\$1, \\$2\\) RETURNING id").
		WithArgs("evaluation-service", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/admin/keys",
		strings.NewReader(`{"name":"evaluation-service"}`),
	)

	app.createKeyHandler(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var response CreateKeyResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if response.Name != "evaluation-service" || !strings.HasPrefix(response.Key, "tm_key_") {
		t.Fatalf("unexpected response: %+v", response)
	}
	assertSQLExpectations(t, mock)
}

func TestCreateKeyHandlesDatabaseError(t *testing.T) {
	app, mock := newTestApp(t)
	mock.ExpectQuery("INSERT INTO api_keys").WillReturnError(errors.New("database unavailable"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(`{"name":"api"}`))

	app.createKeyHandler(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	assertSQLExpectations(t, mock)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestCreateKeyHandlesRandomGeneratorError(t *testing.T) {
	previousReader := rand.Reader
	rand.Reader = failingReader{}
	t.Cleanup(func() { rand.Reader = previousReader })
	app, _ := newTestApp(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(`{"name":"api"}`))

	app.createKeyHandler(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
}

func TestMasterKeyMiddleware(t *testing.T) {
	app := &App{MasterKey: "master-secret"}
	nextCalled := false
	handler := app.masterKeyAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/admin/keys", nil))
	if unauthorized.Code != http.StatusForbidden || nextCalled {
		t.Fatal("request without the master key should be rejected")
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/admin/keys", nil)
	request.Header.Set("Authorization", "Bearer master-secret")
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusNoContent || !nextCalled {
		t.Fatal("request with the master key should reach the next handler")
	}
}
