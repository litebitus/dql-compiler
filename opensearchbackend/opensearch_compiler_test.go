package opensearchbackend

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	return encodedJSON(t, text, request), nil
}

func compiledJSONAt(t *testing.T, text string, compiledAt time.Time) (string, error) {
	t.Helper()
	parsed, err := dqlsyntax.ParseQuery(text, "1.1")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	request, err := compileQueryAt(parsed, mockConfig, compiledAt)
	if err != nil {
		return "", err
	}
	return encodedJSON(t, text, request), nil
}

func encodedJSON(t *testing.T, text string, request map[string]any) string {
	t.Helper()
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(request); err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return strings.TrimSuffix(encoded.String(), "\n")
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
		`app:mock | count() by app | bottomk(3)`:                             `{"aggs":{"by_app":{"terms":{"field":"app","order":{"_count":"asc"},"size":3}}},"query":{"match":{"app":"mock"}},"size":0}`,
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

func TestArithmeticCompilesToOneSearchOverTheBucketsTheSidesShare(t *testing.T) {
	for text, want := range map[string]string{
		`(app:a | count() by job) / (app:b | count() by job) * 100`: `{"aggs":{"by_job":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"filter":{"match":{"app":"b"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>_count","side_1":"side_1>_count"},` +
			`"script":"params.side_0 / params.side_1 * 100.0"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}},` +
			`"size":0}`,
		`(app:a | per_second(5m)) + (app:b | per_second(5m))`: `{"aggs":{"time":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"filter":{"match":{"app":"b"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>_count","side_1":"side_1>_count"},` +
			`"script":"(params.side_0 / 300) + (params.side_1 / 300)"}}},` +
			`"date_histogram":{"field":"mock_time","fixed_interval":"5m"}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}},` +
			`"size":0}`,
		`(app:a | per_second(5m) by job) / (app:b | per_second(300s) by job)`: `{"aggs":{"time":{"aggs":{` +
			`"by_job":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"filter":{"match":{"app":"b"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>_count","side_1":"side_1>_count"},` +
			`"script":"(params.side_0 / 300) / (params.side_1 / 300)"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"date_histogram":{"field":"mock_time","fixed_interval":"5m"}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}},` +
			`"size":0}`,
		`(app:a | avg(latency_ms) by job) * 1000`: `{"aggs":{"by_job":{"aggs":{` +
			`"side_0":{"aggs":{"value":{"avg":{"field":"latency_ms"}}},"filter":{"match":{"app":"a"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>value"},` +
			`"script":"params.side_0 * 1000.0"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}}]}},` +
			`"size":0}`,
		`2 * (app:a | sum(bytes) by job)`: `{"aggs":{"by_job":{"aggs":{` +
			`"side_0":{"aggs":{"value":{"sum":{"field":"bytes"}}},"filter":{"match":{"app":"a"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>value"},` +
			`"script":"2.0 * params.side_0"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}}]}},` +
			`"size":0}`,
		`(app:a | count() by job) + (app:b | count() by job) * (app:c | count() by job) - 1.5`: `{"aggs":{"by_job":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"filter":{"match":{"app":"b"}}},` +
			`"side_2":{"filter":{"match":{"app":"c"}}},` +
			`"value":{"bucket_script":{"buckets_path":` +
			`{"side_0":"side_0>_count","side_1":"side_1>_count","side_2":"side_2>_count"},` +
			`"script":"params.side_0 + params.side_1 * params.side_2 - 1.5"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}},{"match":{"app":"c"}}]}},` +
			`"size":0}`,
		`(app:a | count() by job, env) / (app:b | count() by env, job)`: `{"aggs":{"by_job":{"aggs":{"by_env":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"filter":{"match":{"app":"b"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>_count","side_1":"side_1>_count"},` +
			`"script":"params.side_0 / params.side_1"}}},` +
			`"terms":{"field":"env","size":50}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}},` +
			`"size":0}`,
		`(app:a | quantile(0.95, latency_ms) by job) / (app:b | stdev(latency_ms) by job)`: `{"aggs":{"by_job":{"aggs":{` +
			`"side_0":{"aggs":{"value":{"percentiles":{"field":"latency_ms","percents":[95]}}},` +
			`"filter":{"match":{"app":"a"}}},` +
			`"side_1":{"aggs":{"value":{"extended_stats":{"field":"latency_ms"}}},` +
			`"filter":{"match":{"app":"b"}}},` +
			`"value":{"bucket_script":{"buckets_path":` +
			`{"side_0":"side_0>value[95]","side_1":"side_1>value.std_deviation"},` +
			`"script":"params.side_0 / params.side_1"}}},` +
			`"terms":{"field":"job","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}},{"match":{"app":"b"}}]}},` +
			`"size":0}`,
	} {
		got, err := compiledJSON(t, text, mockConfig)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestNowIsTheBucketsTimeOrTheTimeTheRequestIsCompiled(t *testing.T) {
	compiledAt := time.Unix(1700000000, 0)
	for text, want := range map[string]string{
		`(app:a | max(expiry) by host) - now()`: `{"aggs":{"by_host":{"aggs":{` +
			`"side_0":{"aggs":{"value":{"max":{"field":"expiry"}}},"filter":{"match":{"app":"a"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"side_0":"side_0>value"},` +
			`"script":"params.side_0 - 1700000000.0"}}},` +
			`"terms":{"field":"host","size":50}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}}]}},` +
			`"size":0}`,
		`(app:a | per_second(1m)) * now()`: `{"aggs":{"time":{"aggs":{` +
			`"side_0":{"filter":{"match":{"app":"a"}}},` +
			`"value":{"bucket_script":{"buckets_path":{"now":"_key","side_0":"side_0>_count"},` +
			`"script":"(params.side_0 / 60) * (params.now / 1000)"}}},` +
			`"date_histogram":{"field":"mock_time","fixed_interval":"1m"}}},` +
			`"query":{"bool":{"minimum_should_match":1,` +
			`"should":[{"match":{"app":"a"}}]}},` +
			`"size":0}`,
	} {
		got, err := compiledJSONAt(t, text, compiledAt)
		if err != nil || got != want {
			t.Errorf("%q:\n got  %s (%v)\n want %s", text, got, err, want)
		}
	}
}

func TestArithmeticTheSharedBucketsCannotHoldIsRefused(t *testing.T) {
	groupingRefusal := "a side grouping by more fields does not compile to the OpenSearch query DSL: " +
		"a bucket_script reads only the values in its own bucket"
	timeRefusal := "sides differing in their time function or its window " +
		"do not compile to the OpenSearch query DSL: the sides share one date_histogram"
	bucketsRefusal := "arithmetic compiles to the OpenSearch query DSL over buckets: " +
		"a grouping or a time function"
	for text, want := range map[string]string{
		`(app:a | count() by job, env) / (app:b | count() by job)`:   groupingRefusal,
		`(app:a | count() by job) / (app:b | count() by env)`:        groupingRefusal,
		`(app:a | per_second(5m) by job) / (app:b | count() by job)`: timeRefusal,
		`(app:a | per_second(5m)) / (app:b | per_second(1m))`:        timeRefusal,
		`(app:a | count()) / (app:b | count())`:                      bucketsRefusal,
		`5 * 2`:                                                      bucketsRefusal,
		`(app:a | count() by job | topk(3)) / (app:b | count() by job)`: `"topk" does not compile to the ` +
			`OpenSearch query DSL on a side of arithmetic: the sides share their buckets`,
		`(app:a | per_second(5m) by job) - now()`: "now() does not compile to the OpenSearch query DSL " +
			"beside a time function and a grouping: a bucket_script reads only its own bucket's key",
		`(app:a | sum() by job) * 2`: `over records, "sum" names the field it computes over`,
		`(app:a | dedup(trace_id) | count() by job) * 2`: `"dedup" does not compile to the ` +
			`OpenSearch query DSL beside an aggregation`,
	} {
		if _, err := compiledJSONAt(t, text, time.Unix(1700000000, 0)); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}

func TestFormsTheQueryDSLHasNoFormForAreRefused(t *testing.T) {
	for text, want := range map[string]string{
		`app:mock | rate(5m)`:                          `"rate" does not compile to the OpenSearch query DSL: a record is not a counter`,
		`app:mock | sum()`:                             `over records, "sum" names the field it computes over`,
		`app:mock | count() | sum(bytes)`:              "the OpenSearch query DSL compiles one time or statistic function per query",
		`app:mock | rename(a, b)`:                      `"rename" does not compile to the OpenSearch query DSL over records`,
		`app:mock | sort(value)`:                       `"sort" over records names a field: a record has no value of its own`,
		`app:mock | count() | where(value > 5)`:        `"where" compiles to the OpenSearch query DSL over buckets: a grouping or a time function`,
		`app:mock | where(value > 5) | count() by app`: `"where" compiles to the OpenSearch query DSL after the function whose buckets it selects`,
		`app:mock | count() by app | topk(5) by env`:   `"topk" compiles to the OpenSearch query DSL over the grouping of the statistic before it`,
		`app:mock | stdev(x) by app | topk(5)`:         `"topk" does not compile to the OpenSearch query DSL over a multi-value statistic`,
		`app:mock | dedup(trace_id) | count()`:         `"dedup" does not compile to the OpenSearch query DSL beside an aggregation`,
		`app:mock | count() by app | where(x > 5)`:     `"where" compares a result's value in the OpenSearch query DSL`,
		`app:mock | count() by app | limit(5)`:         `"limit" does not compile to the OpenSearch query DSL over time series`,
		`app:mock | count() by app | sort(value)`:      `"sort" does not compile to the OpenSearch query DSL over time series`,
		`app:mock | per_second(0m)`:                    `a duration of "0m" holds no time`,
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

func TestArithmeticBucketingNeedsItsConfiguration(t *testing.T) {
	for text, want := range map[string]string{
		`(app:mock | per_second(5m)) * 2`: "the OpenSearch backend needs the time field to bucket by time",
		`(app:mock | count() by app) * 2`: "the OpenSearch backend needs a group limit to group by a field",
	} {
		if _, err := compiledJSON(t, text, BackendConfig{}); err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", text, err, want)
		}
	}
}
