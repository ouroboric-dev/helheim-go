package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthzSaysOK(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["status"] != "ok" {
		t.Errorf("body = %s, %v; want {\"status\":\"ok\"}", rec.Body.String(), err)
	}
}

func TestOnlyGetReachesHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want 405", rec.Code)
	}
}

func TestAnUnknownRouteIsA404WithAJSONBody(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Deps{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("GET /nope = %d %q, want a 404 with a JSON body", rec.Code, rec.Header().Get("Content-Type"))
	}
}

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestHealthzTellsWhetherTheDatabaseAnswers(t *testing.T) {
	for name, tc := range map[string]struct {
		deps Deps
		code int
		want string
	}{
		"no database configured":  {Deps{}, http.StatusOK, "ok"},
		"a database that answers": {Deps{DB: fakeDB{}}, http.StatusOK, "ok"},
		"a database that is down": {Deps{DB: fakeDB{err: errors.New("connection refused")}}, http.StatusServiceUnavailable, "database unavailable"},
	} {
		rec := httptest.NewRecorder()
		New(tc.deps).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.code || body["status"] != tc.want {
			t.Errorf("%s: GET /healthz = %d %s, want %d %q", name, rec.Code, rec.Body.String(), tc.code, tc.want)
		}
		if strings.Contains(rec.Body.String(), "refused") {
			t.Errorf("%s: the body leaks the database's error: %s", name, rec.Body.String())
		}
	}
}
