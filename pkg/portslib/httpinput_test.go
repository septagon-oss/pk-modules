// Validates: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.

package portslib_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

func TestDecodeJSONBodyRejectsUnknownAndTrailingValues(t *testing.T) {
	t.Parallel()
	var target struct {
		Name string `json:"name"`
	}
	if err := portslib.DecodeJSONBody(
		strings.NewReader(`{"name":"ok","typo":true}`),
		&target,
	); err == nil || !strings.Contains(err.Error(), `unknown field "typo"`) {
		t.Fatalf("unknown field error = %v", err)
	}
	if err := portslib.DecodeJSONBody(
		strings.NewReader(`{"name":"first"} {"name":"second"}`),
		&target,
	); err == nil || !strings.Contains(err.Error(), "exactly one JSON value") {
		t.Fatalf("trailing value error = %v", err)
	}
	if err := portslib.DecodeJSONBody(
		strings.NewReader("  {\"name\":\"ok\"}\n"),
		&target,
	); err != nil {
		t.Fatalf("valid body: %v", err)
	}
}

func TestParsePaginationRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, query := range []url.Values{
		{"limit": {"0"}},
		{"limit": {"-1"}},
		{"limit": {"many"}},
		{"offset": {"-1"}},
		{"offset": {"later"}},
	} {
		if _, _, err := portslib.ParsePagination(query); err == nil {
			t.Fatalf("ParsePagination(%v) accepted invalid input", query)
		}
	}
	limit, offset, err := portslib.ParsePagination(url.Values{
		"limit":  {"25"},
		"offset": {"10"},
	})
	if err != nil || limit != 25 || offset != 10 {
		t.Fatalf("ParsePagination() = (%d, %d, %v)", limit, offset, err)
	}
}
