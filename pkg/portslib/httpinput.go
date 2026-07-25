// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.

package portslib

// httpinput.go owns the small, shared HTTP input rules used by the reference
// modules: request bodies contain exactly one JSON value with no unknown
// fields, pagination query values are explicit non-negative integers, and an
// entity ID in a path is one canonical opaque segment.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/septagon-oss/pk-shared/pkg/pathsegment"
)

// ErrMalformedEntityID reports a path segment that is not a canonical opaque
// entity ID. It is a client mistake, so handlers answer 400 rather than 404:
// "not found" would imply the identifier was well-formed and simply absent.
//
// The message names the expected form because this is the error a caller sees
// when they pass a raw identifier, and the fix should be recoverable from the
// response alone rather than only from the changelog.
var ErrMalformedEntityID = errors.New(
	`entity id must be one canonical opaque path segment: expected "id-<lowercase hex of the id's bytes>" ` +
		"(encode with pk-shared/pkg/pathsegment.EncodeOpaqueID, or use pk-client)")

// EntityIDFromPath resolves the entity addressed by urlPath beneath prefix.
//
// An empty id means the request addressed the collection itself; verb carries
// anything after the id, such as a lifecycle action. Identifiers travel as the
// canonical segment produced by pathsegment.EncodeOpaqueID, which is what
// pk-client sends, so an ID containing a slash, a percent escape, or a control
// character cannot silently change which entity a route resolves to.
//
// Raw identifiers fail closed. Sharing this rule keeps every module's notion of
// "the id in the path" identical to the client's notion of "the id on the wire".
func EntityIDFromPath(urlPath, prefix string) (id, verb string, err error) {
	rest := strings.TrimPrefix(strings.TrimPrefix(urlPath, prefix), "/")
	if rest == "" {
		return "", "", nil
	}

	segment, verb, _ := strings.Cut(rest, "/")
	decoded, ok := pathsegment.DecodeOpaqueID(segment)
	if !ok {
		return "", "", ErrMalformedEntityID
	}
	return decoded, verb, nil
}

// EncodeEntityID renders an entity ID as its canonical path segment. Callers
// that build a URL — links, tests, clients — must use this so both ends of the
// wire agree.
func EncodeEntityID(entityID string) (string, bool) {
	return pathsegment.EncodeOpaqueID(entityID)
}

// DecodeJSONBody decodes exactly one JSON value and rejects fields the target
// request type does not declare. Keeping this rule shared makes typo handling
// consistent across every built-in mutation endpoint.
func DecodeJSONBody(body io.Reader, target any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

// ParsePagination parses optional limit/offset query values. Missing values
// remain zero so each store can apply its documented default and maximum;
// malformed or negative inputs fail loudly instead of silently changing the
// requested page.
func ParsePagination(values url.Values) (limit, offset int, err error) {
	if raw := values.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return 0, 0, errors.New("limit must be a positive integer")
		}
	}
	if raw := values.Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, errors.New("offset must be a non-negative integer")
		}
	}
	return limit, offset, nil
}
