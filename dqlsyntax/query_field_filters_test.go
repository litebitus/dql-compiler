package dqlsyntax

import (
	"reflect"
	"slices"
	"testing"
)

func TestFieldValuesAreTheValuesItsFiltersCompareTheFieldWith(t *testing.T) {
	for text, want := range map[string][]string{
		`job:a`:                          {"a"},
		`job:(a or b) and not job:"c d"`: {"a", "b", "c d"},
		`job:mock-* and env:${env}`:      {"mock-*"},
		`items:{ job:a } or job:*`:       {"a", "*"},
		`env:mock and latency_ms >= 100`: nil,
		`(job:a | count() by env) / (job:b | count() by env) * 100`: {"a", "b"},
		`(__name__:mock_expiry | min() by host) - now()`:            nil,
	} {
		if got := parsedWithNow(t, text).FieldValues("job"); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

func TestFieldRangesAreTheRangesItsFiltersPutOnTheField(t *testing.T) {
	for text, want := range map[string][]FieldRange{
		`latency_ms >= 100`: {{Field: "latency_ms", Operator: ">=", Bound: "100"}},
		`latency_ms > 10 and (job:a or latency_ms <= 500)`: {
			{Field: "latency_ms", Operator: ">", Bound: "10"},
			{Field: "latency_ms", Operator: "<=", Bound: "500"},
		},
		`not latency_ms < 5`:   {{Field: "latency_ms", Operator: "<", Bound: "5"}},
		`job:a and bytes > 10`: nil,
		`(latency_ms > 1 | count() by job) / (latency_ms > 2 | count() by job)`: {
			{Field: "latency_ms", Operator: ">", Bound: "1"},
			{Field: "latency_ms", Operator: ">", Bound: "2"},
		},
	} {
		if got := parsedWithNow(t, text).FieldRanges("latency_ms"); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %#v, want %#v", text, got, want)
		}
	}
}
