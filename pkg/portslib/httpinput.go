// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.

package portslib

// httpinput.go owns the small, shared HTTP input rules used by the reference
// modules: request bodies contain exactly one JSON value with no unknown
// fields, and pagination query values are explicit non-negative integers.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

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
