package dqlsyntax

import (
	"slices"
	"testing"
)

func TestAQueryResultCarriesItsLastGroupingOrTheSideGroupingByMore(t *testing.T) {
	for text, want := range map[string][]string{
		`__name__:mock_requests | sum() by job`:                                      {"job"},
		`__name__:mock_requests | rate(5m) | sum() by job, status | max() by status`: {"status"},
		`__name__:mock_requests | sum()`:                                             nil,
		`(__name__:mock_requests | sum() by job, status) / (__name__:mock_requests | sum() by job)`: {
			"job", "status",
		},
		`(__name__:mock_expiry | min() by host) * (__name__:mock_info | max() by host, owner) / 86400 - now() / 86400`: {
			"host", "owner",
		},
		`(__name__:mock_requests | sum() by job) * 100`: {"job"},
		`(__name__:mock_a | sum() by job) + (__name__:mock_b | sum() by job, env) - (__name__:mock_c | sum() by job)`: {
			"job", "env",
		},
	} {
		fields, known := parsedWithNow(t, text).QueryResultFields()
		if !known || !slices.Equal(fields, want) {
			t.Errorf("%q: fields %v known %v, want %v", text, fields, known, want)
		}
	}
}

func TestAQueryResultsFieldsAreUnknownWithoutAGroupingOrAPairing(t *testing.T) {
	for _, text := range []string{
		`__name__:mock_requests`,
		`(__name__:mock_requests | rate(5m)) / (__name__:mock_requests | sum() by job)`,
		`(__name__:mock_requests | sum() by job, status) / (__name__:mock_requests | sum() by job, env)`,
	} {
		if fields, known := parsedWithNow(t, text).QueryResultFields(); known {
			t.Errorf("%q: fields %v known, want unknown", text, fields)
		}
	}
}

func parsedWithNow(t *testing.T, text string) ParsedQuery {
	t.Helper()
	parsed, err := ParseQuery(text, "1.1")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return parsed
}
