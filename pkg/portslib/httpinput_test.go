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

// This is the test whose absence let a real defect ship: pk-client encoded
// entity ids with pathsegment while every handler read the segment raw, so
// every by-id call from the client returned 404. Encoding here through the same
// package the client uses keeps the two ends of the wire pinned together.
func TestEntityIDFromPathRoundTripsTheClientEncoding(t *testing.T) {
	t.Parallel()
	const prefix = "/api/v1/content"

	for _, id := range []string{
		"1784965307450776349-tenant_local-interop-probe",
		"user_operator",
		"id-6162", // an id that merely looks encoded must survive verbatim
		"a/b",     // a slash cannot be allowed to change which route matches
		"100%",
		"kůň",
		" leading and trailing ",
	} {
		segment, ok := portslib.EncodeEntityID(id)
		if !ok {
			t.Fatalf("EncodeEntityID(%q) refused a legitimate id", id)
		}
		if strings.Contains(segment, "/") {
			t.Fatalf("segment %q for id %q contains a separator", segment, id)
		}

		got, verb, err := portslib.EntityIDFromPath(prefix+"/"+segment, prefix)
		if err != nil {
			t.Fatalf("EntityIDFromPath(%q) = %v", segment, err)
		}
		if got != id {
			t.Fatalf("round trip: got %q, want %q", got, id)
		}
		if verb != "" {
			t.Fatalf("verb = %q, want empty", verb)
		}

		got, verb, err = portslib.EntityIDFromPath(prefix+"/"+segment+"/publish", prefix)
		if err != nil || got != id || verb != "publish" {
			t.Fatalf("with verb: id=%q verb=%q err=%v", got, verb, err)
		}
	}
}

func TestEntityIDFromPathDistinguishesCollectionFromMalformed(t *testing.T) {
	t.Parallel()
	const prefix = "/api/v1/content"

	// The collection itself carries no id and is not an error.
	for _, path := range []string{prefix, prefix + "/"} {
		id, verb, err := portslib.EntityIDFromPath(path, prefix)
		if err != nil || id != "" || verb != "" {
			t.Fatalf("collection %q: id=%q verb=%q err=%v", path, id, verb, err)
		}
	}

	// Anything that is not a canonical segment fails closed rather than being
	// guessed at, so a caller cannot reach an entity by an alternate spelling.
	for _, segment := range []string{
		"raw-identifier", // a raw id
		"id-6",           // odd-length hex
		"id-6162AB",      // uppercase hex
		"id-",            // empty payload
		"id-zz",          // not hex
		"id-%36%31",      // percent escapes
		"id-00",          // decodes to a NUL byte
	} {
		if _, _, err := portslib.EntityIDFromPath(prefix+"/"+segment, prefix); err == nil {
			t.Fatalf("EntityIDFromPath(%q) accepted a non-canonical segment", segment)
		}
	}
}
