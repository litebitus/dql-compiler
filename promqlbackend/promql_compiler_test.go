package promqlbackend

import (
	"testing"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

func compiled(t *testing.T, text string) (string, error) {
	t.Helper()
	parsed, err := dqlsyntax.ParseQuery(text, "1.0")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return CompileQuery(parsed)
}

func TestQueriesCompileToPromQL(t *testing.T) {
	for text, want := range map[string]string{
		`__name__:mock_up`:                                                                      `mock_up`,
		`__name__:mock_up and job:mock-api`:                                                     `mock_up{job="mock-api"}`,
		`__name__:mock_up and job:"mock api"`:                                                   `mock_up{job="mock api"}`,
		`__name__:mock_up and job:mock*`:                                                        `mock_up{job=~"mock.*"}`,
		`__name__:mock_up and job:*`:                                                            `mock_up{job=~".+"}`,
		`__name__:mock_up and not job:mock-api`:                                                 `mock_up{job!="mock-api"}`,
		`__name__:mock_up and not job:mock*`:                                                    `mock_up{job!~"mock.*"}`,
		`__name__:mock_up and not job:*`:                                                        `mock_up{job=""}`,
		`__name__:mock_up and job:(a.b or c)`:                                                   `mock_up{job=~"a\\.b|c"}`,
		`__name__:mock_up and job:$job`:                                                         `mock_up{job="$job"}`,
		`__name__:mock_up and "log.level":warn`:                                                 `mock_up{"log.level"="warn"}`,
		`__name__:mock_* and job:mock-api`:                                                      `{__name__=~"mock_.*",job="mock-api"}`,
		`__name__:mock_up and not job:(a or b)`:                                                 `mock_up{job!="a",job!="b"}`,
		`__name__:mock_up | rate(5m)`:                                                           `rate(mock_up[5m])`,
		`__name__:mock_up | rate(5m) | sum() by job`:                                            `sum by (job) (rate(mock_up[5m]))`,
		`__name__:mock_up | count()`:                                                            `count(mock_up)`,
		`__name__:mock_up | stdev() by job`:                                                     `stddev by (job) (mock_up)`,
		`__name__:mock_up | distinct_count(job) by env`:                                         `count by (env) (count by (env, job) (mock_up))`,
		`__name__:mock_up | quantile(0.9) by job`:                                               `quantile by (job) (0.9, mock_up)`,
		`__name__:mock_latency_bucket | rate(5m) | quantile(0.95) by job`:                       `histogram_quantile(0.95, sum by (le, job) (rate(mock_latency_bucket[5m])))`,
		`__name__:mock_up | topk(5) by job`:                                                     `topk by (job) (5, mock_up)`,
		`__name__:mock_up | bottomk(3)`:                                                         `bottomk(3, mock_up)`,
		`__name__:mock_up | sort(value)`:                                                        `sort(mock_up)`,
		`__name__:mock_up | sort_desc(value)`:                                                   `sort_desc(mock_up)`,
		`__name__:mock_up | where(value > 5)`:                                                   `mock_up > 5`,
		`__name__:mock_up | rename(job, service)`:                                               `label_replace(mock_up, "service", "$1", "job", "(.*)")`,
		`__name__:mock_up | rate(5m) | sum() | max()`:                                           `max(sum(rate(mock_up[5m])))`,
		`__name__:mock_requests_total and (job:mock-api or env:mock) | rate(5m) | sum() by job`: `sum by (job) (rate(mock_requests_total{job="mock-api"}[5m]) or rate(mock_requests_total{env="mock"}[5m]))`,
		`(__name__:mock_up and job:a) or (__name__:mock_up and job:b) | where(value == 1)`:      `(mock_up{job="a"} or mock_up{job="b"}) == 1`,
		`(__name__:mock_errors | rate(5m) | sum() by job) / (__name__:mock_requests | rate(5m) | sum() by job) * 100`: `(sum by (job) (rate(mock_errors[5m]))) / on (job) (sum by (job) (rate(mock_requests[5m]))) * 100`,
	} {
		got, err := compiled(t, text)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestFormsPromQLHasNoFormForAreRefused(t *testing.T) {
	for text, want := range map[string]string{
		`mock`:                              "a term with no field does not compile to PromQL: a series carries labels, not text",
		`__name__:mock_up and value > 5`:    "a range does not compile to PromQL: a label matcher has no range",
		`items:{ name:mock }`:               "a nested field does not compile to PromQL: a series' labels are flat",
		`not job:mock-api`:                  "a selector needs a field that cannot be empty, such as __name__",
		`| count()`:                         "a PromQL query needs a filter naming its series",
		`__name__:mock_up | per_second(1m)`: `"per_second" does not compile to PromQL: a series is not a stream of items; rate is a counter's increase`,
		`__name__:mock_up | dedup(job)`:     `"dedup" does not compile to PromQL: a series is not a record`,
		`__name__:mock_up | limit(5)`:       `"limit" does not compile to PromQL: PromQL's limitk is experimental`,
		`__name__:mock_up | sum(bytes)`:     `over series, "sum" takes no field: it computes over the series' values`,
		`__name__:mock_up | sort(job)`:      `"sort" does not compile to PromQL by a field: PromQL sorts by value`,
		`__name__:mock_up | where(job > 5)`: `"where" does not compile to PromQL on a field: PromQL compares a series' value`,
	} {
		if _, err := compiled(t, text); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}

func TestAFilterExpandingPastTheSelectorLimitIsRefused(t *testing.T) {
	text := `__name__:mock_up and (a:1 or b:1) and (a:2 or b:2) and (a:3 or b:3) and (a:4 or b:4) and (a:5 or b:5) and (a:6 or b:6) and (a:7 or b:7)`
	if _, err := compiled(t, text); err == nil || err.Error() != "the filter expands to 128 selectors, more than 64" {
		t.Errorf("err = %v", err)
	}
}
