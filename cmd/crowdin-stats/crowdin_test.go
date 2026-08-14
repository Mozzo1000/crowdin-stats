package main

import (
	"encoding/json"
	"testing"
)

// flexibleInt exists because Crowdin's report export emits fields like
// user.id/translated/approved as quoted strings rather than bare numbers —
// a plain int/json.Number field hard-fails on that, silently breaking every
// contributors.svg render (see crowdin.go). This asserts the cases it was
// written to handle.
func TestFlexibleIntUnmarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want flexibleInt
	}{
		{"quoted string, the observed Crowdin shape", `"42"`, 42},
		{"bare number", `42`, 42},
		{"quoted zero", `"0"`, 0},
		{"null", `null`, 0},
		{"empty string", `""`, 0},
		{"non-integer numeric string coerces to 0", `"12.5"`, 0},
		{"non-numeric garbage coerces to 0", `"not-a-number"`, 0},
	}
	for _, c := range cases {
		var f flexibleInt
		if err := json.Unmarshal([]byte(c.in), &f); err != nil {
			t.Errorf("%s: json.Unmarshal(%s) returned error %v, want nil (flexibleInt must never hard-fail decode)", c.name, c.in, err)
			continue
		}
		if f != c.want {
			t.Errorf("%s: json.Unmarshal(%s) = %d, want %d", c.name, c.in, f, c.want)
		}
	}
}

// A malformed field on one row must not abort decoding the whole report —
// flexibleInt coerces to 0 instead of erroring precisely so this struct
// field (and any other) keeps decoding the rest of the response.
func TestFlexibleIntUnmarshalJSONWithinStruct(t *testing.T) {
	var row topMembersReportRow
	err := json.Unmarshal([]byte(`{"user":{"id":"123","username":"alice"},"translated":"7","approved":"garbage"}`), &row)
	if err != nil {
		t.Fatalf("unexpected error decoding row: %v", err)
	}
	if row.User.ID != 123 {
		t.Errorf("User.ID = %d, want 123", row.User.ID)
	}
	if row.Translated != 7 {
		t.Errorf("Translated = %d, want 7", row.Translated)
	}
	if row.Approved != 0 {
		t.Errorf("Approved = %d, want 0 (malformed value coerces rather than aborting decode)", row.Approved)
	}
}
