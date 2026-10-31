// Package opensearchbackend compiles a parsed query into an OpenSearch search request body.
package opensearchbackend

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

// BackendConfig is what the compiler cannot read from a query.
type BackendConfig struct {
	// TimeField is the field a time function buckets by.
	TimeField string
	// GroupLimit is how many groups each grouping field answers.
	GroupLimit int
}

const valueAggregation = "value"

// CompileQuery answers the search request body a parsed query compiles to.
func CompileQuery(query dqlsyntax.ParsedQuery, config BackendConfig) (map[string]any, error) {
	if len(query.Expression.Operands) > 1 {
		return nil, errors.New("arithmetic does not compile to the OpenSearch query DSL: a search request answers one query")
	}
	parsed := query.Expression.Operands[0].Query
	queryClause := map[string]any{"match_all": map[string]any{}}
	if parsed.Filter != nil {
		clause, err := filterClause(parsed.Filter, "")
		if err != nil {
			return nil, err
		}
		queryClause = clause
	}
	if query.ResultKind() == dqlsyntax.ItemsResult {
		return itemsRequest(queryClause, parsed.Stages)
	}
	return seriesRequest(queryClause, parsed.Stages, config)
}

func itemsRequest(queryClause map[string]any, stages []dqlsyntax.Stage) (map[string]any, error) {
	request := map[string]any{"query": queryClause}
	var sorting []any
	for _, stage := range stages {
		switch stage.Function {
		case "dedup":
			request["collapse"] = map[string]any{"field": stage.Fields[0]}
		case "fields":
			request["_source"] = stringsAsAny(stage.Fields)
		case "limit":
			size, _ := strconv.Atoi(stage.Number)
			request["size"] = size
		case "sort", "sort_desc":
			if stage.SortKey == dqlsyntax.ResultValue {
				return nil, fmt.Errorf("%q over records names a field: a record has no value of its own", stage.Function)
			}
			order := "asc"
			if stage.Function == "sort_desc" {
				order = "desc"
			}
			sorting = append(sorting, map[string]any{stage.SortKey: map[string]any{"order": order}})
		default:
			return nil, fmt.Errorf("%q does not compile to the OpenSearch query DSL over records", stage.Function)
		}
	}
	if len(sorting) > 0 {
		request["sort"] = sorting
	}
	return request, nil
}

// bucketedMetric is the aggregation tree built so far and the path a pipeline aggregation reads the metric by.
type bucketedMetric struct {
	leaf       map[string]any
	metricPath string
	singleton  bool
}

func seriesRequest(queryClause map[string]any, stages []dqlsyntax.Stage, config BackendConfig) (map[string]any, error) {
	var computing *dqlsyntax.Stage
	var selecting []dqlsyntax.Stage
	for index, stage := range stages {
		switch dqlsyntax.Functions[stage.Function].Category {
		case dqlsyntax.ShapingCategory:
			return nil, fmt.Errorf("%q does not compile to the OpenSearch query DSL beside an aggregation", stage.Function)
		case dqlsyntax.ResultCategory:
			if computing == nil {
				return nil, fmt.Errorf("%q compiles to the OpenSearch query DSL after the function whose buckets it selects", stage.Function)
			}
			selecting = append(selecting, stage)
		default:
			if computing != nil {
				return nil, errors.New("the OpenSearch query DSL compiles one time or statistic function per query")
			}
			computing = &stages[index]
		}
	}
	grouping := computing.Grouping
	if len(grouping) > 0 && config.GroupLimit <= 0 {
		return nil, errors.New("the OpenSearch backend needs a group limit to group by a field")
	}
	metric, err := metricOf(*computing, config, len(grouping) > 0)
	if err != nil {
		return nil, err
	}
	innermost := map[string]any{}
	if len(metric.leaf) > 0 {
		innermost["aggs"] = metric.leaf
	}
	bucketed := len(grouping) > 0 || computing.Function == "per_second"
	for _, stage := range selecting {
		if !bucketed {
			return nil, fmt.Errorf("%q compiles to the OpenSearch query DSL over buckets: a grouping or a time function", stage.Function)
		}
		if err := applySelection(innermost, stage, metric, grouping); err != nil {
			return nil, err
		}
	}
	aggregations := metric.leaf
	if len(grouping) > 0 {
		aggregations = groupedBy(grouping, config.GroupLimit, innermost)
	}
	if computing.Function == "per_second" {
		histogram := map[string]any{"date_histogram": map[string]any{
			"field":          config.TimeField,
			"fixed_interval": fixedInterval(computing.Duration),
		}}
		if len(aggregations) > 0 {
			histogram["aggs"] = aggregations
		}
		aggregations = map[string]any{"time": histogram}
	}
	return map[string]any{"size": 0, "query": queryClause, "aggs": aggregations}, nil
}

var metricAggregations = map[string]string{
	"avg":            "avg",
	"distinct_count": "cardinality",
	"max":            "max",
	"min":            "min",
	"sum":            "sum",
}

func metricOf(stage dqlsyntax.Stage, config BackendConfig, grouped bool) (bucketedMetric, error) {
	switch stage.Function {
	case "per_second":
		if config.TimeField == "" {
			return bucketedMetric{}, errors.New("the OpenSearch backend needs the time field to bucket by time")
		}
		seconds, err := durationSeconds(stage.Duration)
		if err != nil {
			return bucketedMetric{}, err
		}
		return bucketedMetric{leaf: map[string]any{valueAggregation: map[string]any{"bucket_script": map[string]any{
			"buckets_path": map[string]any{"count": "_count"},
			"script":       "params.count / " + strconv.FormatFloat(seconds, 'f', -1, 64),
		}}}, metricPath: valueAggregation, singleton: true}, nil
	case "count":
		if grouped {
			return bucketedMetric{metricPath: "_count", singleton: true}, nil
		}
		return bucketedMetric{leaf: map[string]any{valueAggregation: map[string]any{"filter": map[string]any{"match_all": map[string]any{}}}}}, nil
	case "rate":
		return bucketedMetric{}, errors.New(`"rate" does not compile to the OpenSearch query DSL: a record is not a counter`)
	}
	if len(stage.Fields) == 0 {
		return bucketedMetric{}, fmt.Errorf("over records, %q names the field it computes over", stage.Function)
	}
	field := map[string]any{"field": stage.Fields[0]}
	switch stage.Function {
	case "quantile":
		percent, err := strconv.ParseFloat(stage.Number, 64)
		if err != nil {
			return bucketedMetric{}, err
		}
		field["percents"] = []any{math.Round(percent*100*1e9) / 1e9}
		return bucketedMetric{leaf: map[string]any{valueAggregation: map[string]any{"percentiles": field}}}, nil
	case "stdev":
		return bucketedMetric{leaf: map[string]any{valueAggregation: map[string]any{"extended_stats": field}}}, nil
	}
	return bucketedMetric{
		leaf:       map[string]any{valueAggregation: map[string]any{metricAggregations[stage.Function]: field}},
		metricPath: valueAggregation,
		singleton:  true,
	}, nil
}

func applySelection(innermost map[string]any, stage dqlsyntax.Stage, metric bucketedMetric, grouping []string) error {
	if !metric.singleton {
		return fmt.Errorf("%q does not compile to the OpenSearch query DSL over a multi-value statistic", stage.Function)
	}
	switch stage.Function {
	case "topk", "bottomk":
		if len(grouping) == 0 || len(stage.Grouping) > 0 {
			return fmt.Errorf("%q compiles to the OpenSearch query DSL over the grouping of the statistic before it", stage.Function)
		}
		size, _ := strconv.Atoi(stage.Number)
		order := "desc"
		if stage.Function == "bottomk" {
			order = "asc"
		}
		innermost["size"] = size
		innermost["order"] = map[string]any{metric.metricPath: order}
		return nil
	case "where":
		if stage.Comparison.Subject != dqlsyntax.ResultValue {
			return fmt.Errorf("%q compares a result's value in the OpenSearch query DSL", stage.Function)
		}
		aggregations, _ := innermost["aggs"].(map[string]any)
		if aggregations == nil {
			aggregations = map[string]any{}
			innermost["aggs"] = aggregations
		}
		aggregations["where"] = map[string]any{"bucket_selector": map[string]any{
			"buckets_path": map[string]any{"value": metric.metricPath},
			"script":       "params.value " + stage.Comparison.Operator + " " + stage.Comparison.Number,
		}}
		return nil
	}
	return fmt.Errorf("%q does not compile to the OpenSearch query DSL over time series", stage.Function)
}

// groupedBy nests one terms aggregation per grouping field, the innermost carrying the metric.
func groupedBy(grouping []string, groupLimit int, innermost map[string]any) map[string]any {
	var aggregations map[string]any
	for index := len(grouping) - 1; index >= 0; index-- {
		terms := map[string]any{"field": grouping[index], "size": groupLimit}
		bucket := map[string]any{"terms": terms}
		if index == len(grouping)-1 {
			if size, ordered := innermost["size"]; ordered {
				terms["size"], terms["order"] = size, innermost["order"]
			}
			if leaf, holds := innermost["aggs"]; holds {
				bucket["aggs"] = leaf
			}
		} else {
			bucket["aggs"] = aggregations
		}
		aggregations = map[string]any{"by_" + grouping[index]: bucket}
	}
	return aggregations
}

var durationPartPattern = regexp.MustCompile(`(\d+)(ms|s|m|h|d|w|y)`)

var durationUnitSeconds = map[string]float64{
	"ms": 0.001,
	"s":  1,
	"m":  60,
	"h":  3600,
	"d":  86400,
	"w":  604800,
	"y":  31536000,
}

func durationSeconds(duration string) (float64, error) {
	total := 0.0
	for _, part := range durationPartPattern.FindAllStringSubmatch(duration, -1) {
		count, err := strconv.ParseFloat(part[1], 64)
		if err != nil {
			return 0, err
		}
		total += count * durationUnitSeconds[part[2]]
	}
	if total == 0 {
		return 0, fmt.Errorf("a duration of %q holds no time", duration)
	}
	return total, nil
}

var fixedIntervalPattern = regexp.MustCompile(`^\d+(ms|s|m|h|d)$`)

// fixedInterval writes a duration as date_histogram's fixed_interval reads one: a single unit up to days.
func fixedInterval(duration string) string {
	if fixedIntervalPattern.MatchString(duration) {
		return duration
	}
	seconds, _ := durationSeconds(duration)
	return strconv.FormatInt(int64(math.Round(seconds*1000)), 10) + "ms"
}

// queryStringSpecials are the characters query_string reads as syntax.
const queryStringSpecials = `+-=&|><!(){}[]^"~*?:\/ `

func filterClause(node dqlsyntax.FilterNode, nestedPath string) (map[string]any, error) {
	switch filter := node.(type) {
	case dqlsyntax.AnyOf:
		clauses, err := filterClauses(filter.Clauses, nestedPath)
		return map[string]any{"bool": map[string]any{"should": clauses, "minimum_should_match": 1}}, err
	case dqlsyntax.AllOf:
		clauses, err := filterClauses(filter.Clauses, nestedPath)
		return map[string]any{"bool": map[string]any{"filter": clauses}}, err
	case dqlsyntax.NoneOf:
		clause, err := filterClause(filter.Clause, nestedPath)
		return map[string]any{"bool": map[string]any{"must_not": []any{clause}}}, err
	case dqlsyntax.TermMatch:
		return termClause(filter.Value), nil
	case dqlsyntax.FieldMatch:
		return valueClause(nestedPath+filter.Field, filter.Value), nil
	case dqlsyntax.FieldRange:
		return map[string]any{"range": map[string]any{
			nestedPath + filter.Field: map[string]any{rangeOperatorNames[filter.Operator]: filter.Bound},
		}}, nil
	case dqlsyntax.NestedMatch:
		path := nestedPath + filter.Field
		inner, err := filterClause(filter.Filter, path+".")
		return map[string]any{"nested": map[string]any{"path": path, "query": inner}}, err
	}
	return nil, fmt.Errorf("a %T does not compile to the OpenSearch query DSL", node)
}

var rangeOperatorNames = map[string]string{"<": "lt", "<=": "lte", ">": "gt", ">=": "gte"}

func filterClauses(nodes []dqlsyntax.FilterNode, nestedPath string) ([]any, error) {
	clauses := make([]any, 0, len(nodes))
	for _, node := range nodes {
		clause, err := filterClause(node, nestedPath)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, clause)
	}
	return clauses, nil
}

func termClause(value dqlsyntax.Value) map[string]any {
	switch {
	case value.Quoted:
		return map[string]any{"multi_match": map[string]any{"query": value.Text(), "type": "phrase", "lenient": true}}
	case value.HasWildcard():
		return map[string]any{"query_string": map[string]any{"query": wildcardPattern(value)}}
	}
	return map[string]any{"multi_match": map[string]any{"query": value.Text(), "type": "best_fields", "lenient": true}}
}

func valueClause(field string, value dqlsyntax.Value) map[string]any {
	switch {
	case value.IsPresence():
		return map[string]any{"exists": map[string]any{"field": field}}
	case value.Quoted:
		return map[string]any{"match_phrase": map[string]any{field: value.Text()}}
	case value.HasWildcard():
		return map[string]any{"query_string": map[string]any{"fields": []any{field}, "query": wildcardPattern(value)}}
	}
	return map[string]any{"match": map[string]any{field: value.Text()}}
}

func wildcardPattern(value dqlsyntax.Value) string {
	escaped := make([]string, len(value.Parts))
	for index, part := range value.Parts {
		var builder strings.Builder
		for _, character := range part {
			if strings.ContainsRune(queryStringSpecials, character) {
				builder.WriteRune('\\')
			}
			builder.WriteRune(character)
		}
		escaped[index] = builder.String()
	}
	return strings.Join(escaped, "*")
}

func stringsAsAny(values []string) []any {
	converted := make([]any, len(values))
	for index, value := range values {
		converted[index] = value
	}
	return converted
}
