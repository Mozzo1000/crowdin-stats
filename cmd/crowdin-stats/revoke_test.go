package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleRevoke(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true, noRateLimit: true}

	token, hash, err := generateRevokeToken()
	if err != nil {
		t.Fatalf("generateRevokeToken: %v", err)
	}
	if err := insertProject(db, "pid-revoke", "12345", []byte("ct"), []byte("n"), time.Now().Unix(), hash); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	// First revoke: succeeds and reports "revoked".
	r := httptest.NewRequest(http.MethodPost, "/revoke/"+token, nil)
	r.SetPathValue("token", token)
	w := httptest.NewRecorder()
	s.handleRevoke(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp revokeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response wasn't valid JSON: %v (%s)", err, w.Body.String())
	}
	if resp.Status != "revoked" {
		t.Fatalf("expected status=revoked, got %s", resp.Status)
	}

	if _, err := getProject(db, "pid-revoke"); err != errProjectNotFound {
		t.Fatalf("expected project embeds to 404 after revoke, got %v", err)
	}

	// Second revoke with the same token: idempotent, reports "already_revoked".
	r = httptest.NewRequest(http.MethodPost, "/revoke/"+token, nil)
	r.SetPathValue("token", token)
	w = httptest.NewRecorder()
	s.handleRevoke(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on second revoke, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response wasn't valid JSON: %v (%s)", err, w.Body.String())
	}
	if resp.Status != "already_revoked" {
		t.Fatalf("expected status=already_revoked, got %s", resp.Status)
	}
}

func TestHandleRevokeUnknownToken(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true, noRateLimit: true}

	r := httptest.NewRequest(http.MethodPost, "/revoke/garbage-token", nil)
	r.SetPathValue("token", "garbage-token")
	w := httptest.NewRecorder()
	s.handleRevoke(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
