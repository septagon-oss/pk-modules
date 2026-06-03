package auth_test

// handler_test.go validates the HTTP error-mapping contract of the auth
// session handler. It exercises the handler the way an OSS caller hits the
// /api/v1/auth/sessions surface, asserting that bad input yields a client
// error (400/401) rather than a 500.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/auth"
)

// postLogin sends a JSON login body to the handler and returns the response
// recorder.
func postLogin(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, auth.APIPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginHTTPMissingTenantReturns400(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	rec := postLogin(t, m.HTTPHandler(), `{"email":"alice@example.test","password":"supersecret"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tenant_id") {
		t.Fatalf("body = %q, want it to mention 'tenant_id'", rec.Body.String())
	}
}

func TestLoginHTTPMissingIdentifierReturns400(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	rec := postLogin(t, m.HTTPHandler(), `{"tenant_id":"t-1","password":"supersecret"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestLoginHTTPMissingPasswordReturns400(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	rec := postLogin(t, m.HTTPHandler(), `{"tenant_id":"t-1","email":"alice@example.test"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestLoginHTTPUnknownEmailReturns401(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	rec := postLogin(t, m.HTTPHandler(), `{"tenant_id":"t-1","email":"ghost@example.test","password":"supersecret"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	// No user enumeration: the body must not reveal the user is unknown.
	if strings.Contains(strings.ToLower(rec.Body.String()), "not found") {
		t.Fatalf("body = %q leaks user existence", rec.Body.String())
	}
}

func TestLoginHTTPWrongPasswordReturns401(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	rec := postLogin(t, m.HTTPHandler(), `{"tenant_id":"t-1","email":"alice@example.test","password":"WRONGPASS"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestLoginHTTPValidCredentialsReturns201(t *testing.T) {
	t.Parallel()
	m, reader, hasher := newModule(t)
	reader.add(makeUserWithPassword(t, hasher, "t-1", "u-1", "alice@example.test", "supersecret", true))

	rec := postLogin(t, m.HTTPHandler(), `{"tenant_id":"t-1","email":"alice@example.test","password":"supersecret"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id"`) {
		t.Fatalf("body = %q, want a session with an id", rec.Body.String())
	}
}

// TestLoginServiceMissingTenantIsInvalidRequest asserts the service-level
// sentinel so callers (and the handler) can branch on it.
func TestLoginServiceMissingTenantIsInvalidRequest(t *testing.T) {
	t.Parallel()
	m, _, _ := newModule(t)

	_, err := m.Service().Login(t.Context(), "", auth.Credentials{
		Email:    "alice@example.test",
		Password: "supersecret",
	})
	if !errors.Is(err, auth.ErrInvalidRequest) {
		t.Fatalf("Login err = %v, want ErrInvalidRequest", err)
	}
}
