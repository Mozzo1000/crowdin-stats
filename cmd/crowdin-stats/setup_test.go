package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleSetupRejectsNonJSONContentType(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true, noRateLimit: true}

	body := `{"crowdin_project_id":"12345","token":"tok"}`
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		r := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(body))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		w := httptest.NewRecorder()
		s.handleSetup(w, r)

		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: expected 415, got %d: %s", ct, w.Code, w.Body.String())
		}
	}
}

func TestHandleListProjectsRejectsNonJSONContentType(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true, noRateLimit: true}

	r := httptest.NewRequest(http.MethodPost, "/setup/projects", strings.NewReader(`{"token":"tok"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.handleListProjects(w, r)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", w.Code, w.Body.String())
	}
}
