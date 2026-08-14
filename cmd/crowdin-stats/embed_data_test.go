package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleEmbedDataUnknownProject(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true}

	r := httptest.NewRequest(http.MethodGet, "/embed/does-not-exist/data.json", nil)
	r.SetPathValue("publicID", "does-not-exist")
	w := httptest.NewRecorder()

	s.handleEmbedData(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// A project whose stored ciphertext isn't valid encryptToken output can't be
// decrypted, so handleEmbedData should surface the failure as a JSON error
// body (this endpoint is consumed by fetch(), not <img src>) rather than
// panicking or hanging.
func TestHandleEmbedDataDecryptFailure(t *testing.T) {
	db := newTestDB(t)
	s := &server{db: db, noCache: true}

	if err := insertProject(db, "pid-bad-token", "12345", []byte("not-valid-ciphertext"), []byte("also-not-valid-nonce-000"), time.Now().Unix(), "hash-bad-token"); err != nil {
		t.Fatalf("insertProject: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/embed/pid-bad-token/data.json", nil)
	r.SetPathValue("publicID", "pid-bad-token")
	w := httptest.NewRecorder()

	s.handleEmbedData(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response wasn't valid JSON: %v (%s)", err, w.Body.String())
	}
	if body["error"] == "" {
		t.Fatalf("expected an error message, got %v", body)
	}
}

// The JSON field names must match static/embed-builder.js's expectations
// (its DEMO_LANGUAGES/DEMO_CONTRIBUTORS fixture shape) since the client
// feeds this data straight into the same renderer functions used for the
// demo preview.
func TestEmbedDataResponseJSONShape(t *testing.T) {
	resp := embedDataResponse{
		Languages: []embedDataLanguage{{
			ID: "fr", Name: "French", Percent: 90, ApprovalPercent: 70,
			WordsTotal: 100, WordsTranslated: 90, WordsApproved: 70,
			PhrasesTotal: 50, PhrasesTranslated: 45, PhrasesApproved: 35,
		}},
		Contributors: []embedDataContributor{{
			Username: "amara", FullName: "Amara Okafor", Amount: 4210, Avatar: "https://example.com/a.png",
		}},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	langs := raw["languages"].([]interface{})[0].(map[string]interface{})
	for _, key := range []string{"id", "name", "percent", "approvalPercent", "wordsTotal", "wordsTranslated", "wordsApproved", "phrasesTotal", "phrasesTranslated", "phrasesApproved"} {
		if _, ok := langs[key]; !ok {
			t.Errorf("languages[0] missing key %q in %v", key, langs)
		}
	}

	contribs := raw["contributors"].([]interface{})[0].(map[string]interface{})
	for _, key := range []string{"username", "fullName", "amount", "avatar"} {
		if _, ok := contribs[key]; !ok {
			t.Errorf("contributors[0] missing key %q in %v", key, contribs)
		}
	}
}
