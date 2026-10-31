package opensearchbackend

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

var mockConfig = BackendConfig{TimeField: "mock_time", GroupLimit: 50}

func compiledJSON(t *testing.T, text string, config BackendConfig) (string, error) {
	t.Helper()
	parsed, err := dqlsyntax.ParseQuery(text, "1.0")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	request, err := CompileQuery(parsed, config)
	if err != nil {
		return "", err
	}
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return strings.TrimSuffix(encoded.String(), "\n"), nil
}

func TestFiltersCompileAsOpenSearchDashboardsCompilesThem(t *testing.T) {
	for text, want := range map[string]string{
		``:                    `{"query":{"match_all":{}}}`,
		`mock`:                `{"query":{"multi_match":{"lenient":true,"query":"mock","type":"best_fields"}}}`,
		`"a phrase"`:          `{"query":{"multi_match":{"lenient":true,"query":"a phrase","type":"phrase"}}}`,
		`mock*`:               `{"query":{"query_string":{"query":"mock*"}}}`,
		`app:mock`:            `{"query":{"match":{"app":"mock"}}}`,
		`app:"mock api"`:      `{"query":{"match_phrase":{"app":"mock api"}}}`,
		`path:a/b*`:           `{"query":{"query_string":{"fields":["path"],"query":"a\\/b*"}}}`,
		`path:a/${env}*`:      `{"query":{"query_string":{"fields":["path"],"query":"a\\/${env}*"}}}`,
		`app:${app}`:          `{"query":{"match":{"app":"${app}"}}}`,
		`app:*`:               `{"query":{"exists":{"field":"app"}}}`,
		`latency_ms >= 100`:   `{"query":{"range":{"latency_ms":{"gte":"100"}}}}`,
		`items:{ name:mock }`: `{"query":{"nested":{"path":"items","query":{"match":{"items.name":"mock"}}}}}`,
		`a and not b`:         `{"query":{"bool":{"filter":[{"multi_match":{"lenient":true,"query":"a","type":"best_fields"}},{"bool":{"must_not":[{"multi_match":{"lenient":true,"query":"b","type":"best_fields"}}]}}]}}}`,
		`app:(a or b)`:        `{"query":{"bool":{"minimum_should_match":1,"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}}}`,
	} {
		got, err := compiledJSON(t, text, mockConfig)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestStagesCompileToTheSearchRequest(t *testing.T) {
	for text, want := range map[string]string{
		`app:mock | fields(app, message) | sort_desc(mock_time) | limit(10)`: `{"_source":["app","message"],"query":{"match":{"app":"mock"}},"size":10,"sort":[{"mock_time":{"order":"desc"}}]}`,
		`app:mock | dedup(trace_id)`:                                         `{"collapse":{"field":"trace_id"},"query":{"match":{"app":"mock"}}}`,
		`app:mock | per_second(5m)`:                                          `{"aggs":{"time":{"aggs":{"value":{"bucket_script":{"buckets_path":{"count":"_count"},"script":"params.count / 300"}}},"date_histogram":{"field":"mock_time","fixed_interval":"5m"}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | per_second(1h30m) by app`:                                `{"aggs":{"time":{"aggs":{"by_app":{"aggs":{"value":{"bucket_script":{"buckets_path":{"count":"_count"},"script":"params.count / 5400"}}},"terms":{"field":"app","size":50}}},"date_histogram":{"field":"mock_time","fixed_interval":"5400000ms"}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | count()`:                                                 `{"aggs":{"value":{"filter":{"match_all":{}}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | count() by app, env`:                                     `{"aggs":{"by_app":{"aggs":{"by_env":{"terms":{"field":"env","size":50}}},"terms":{"field":"app","size":50}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | avg(latency_ms) by app | topk(5)`:                        `{"aggs":{"by_app":{"aggs":{"value":{"avg":{"field":"latency_ms"}}},"terms":{"field":"app","order":{"value":"desc"},"size":5}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | count() by app | where(value > 100)`:                     `{"aggs":{"by_app":{"aggs":{"where":{"bucket_selector":{"buckets_path":{"value":"_count"},"script":"params.value > 100"}}},"terms":{"field":"app","size":50}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | distinct_count(user)`:                                    `{"aggs":{"value":{"cardinality":{"field":"user"}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | quantile(0.95, latency_ms)`:                              `{"aggs":{"value":{"percentiles":{"field":"latency_ms","percents":[95]}}},"query":{"match":{"app":"mock"}},"size":0}`,
		`app:mock | stdev(latency_ms)`:                                       `{"aggs":{"value":{"extended_stats":{"field":"latency_ms"}}},"query":{"match":{"app":"mock"}},"size":0}`,
	} {
		got, err := compiledJSON(t, text, mockConfig)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestFormsTheQueryDSLHasNoFormForAreRefused(t *testing.T) {
	for text, want := range map[string]string{
		`(a | count() by app) / (b | count() by app)`:  "arithmetic does not compile to the OpenSearch query DSL: a search request answers one query",
		`app:mock | rate(5m)`:                          `"rate" does not compile to the OpenSearch query DSL: a record is not a counter`,
		`app:mock | sum()`:                             `over records, "sum" names the field it computes over`,
		`app:mock | count() | sum(bytes)`:              "the OpenSearch query DSL compiles one time or statistic function per query",
		`app:mock | rename(a, b)`:                      `"rename" does not compile to the OpenSearch query DSL over records`,
		`app:mock | sort(value)`:                       `"sort" over records names a field: a record has no value of its own`,
		`app:mock | count() | where(value > 5)`:        `"where" compiles to the OpenSearch query DSL over buckets: a grouping or a time function`,
		`app:mock | where(value > 5) | count() by app`: `"where" compiles to the OpenSearch query DSL after the function whose buckets it selects`,
		`app:mock | count() by app | topk(5) by env`:   `"topk" compiles to the OpenSearch query DSL over the grouping of the statistic before it`,
		`app:mock | stdev(x) by app | topk(5)`:         `"topk" does not compile to the OpenSearch query DSL over a multi-value statistic`,
	} {
		if _, err := compiledJSON(t, text, mockConfig); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}

func TestBucketingNeedsItsConfiguration(t *testing.T) {
	for text, want := range map[string]string{
		`app:mock | per_second(5m)`: "the OpenSearch backend needs the time field to bucket by time",
		`app:mock | count() by app`: "the OpenSearch backend needs a group limit to group by a field",
	} {
		if _, err := compiledJSON(t, text, BackendConfig{}); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}
