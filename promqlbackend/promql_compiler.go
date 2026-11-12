// Package promqlbackend compiles a parsed query into a PromQL expression.
package promqlbackend

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

// SelectorLimit bounds how many selectors a filter's `or` may expand into.
const SelectorLimit = 64

var refusals = map[string]string{
	"dedup":      "a series is not a record",
	"eval":       "a series carries labels, not fields to compute",
	"extract":    "a series carries labels, not text to extract from",
	"fields":     "a series keeps its labels",
	"per_second": "a series is not a stream of items; rate is a counter's increase",
	"limit":      "PromQL's limitk is experimental",
}

// BackendConfig is what the compiler cannot read from a query.
type BackendConfig struct {
	// ReportingFilter is a DQL filter selecting the series a source emits while it reports; optional.
	ReportingFilter string
}

// CompileQuery answers the PromQL expression a parsed query compiles to.
func CompileQuery(query dqlsyntax.ParsedQuery, config BackendConfig) (string, error) {
	reporting, err := reportingSelector(config.ReportingFilter, query.GrammarVersion)
	if err != nil {
		return "", err
	}
	expression := query.Expression
	if len(expression.Operands) == 1 {
		return compileCountingQuery(expression.Operands[0].Query, reporting)
	}
	clauses, err := pairingClauses(expression)
	if err != nil {
		return "", err
	}
	var compiled strings.Builder
	for index, operand := range expression.Operands {
		if index > 0 {
			compiled.WriteString(" ")
			compiled.WriteString(expression.Operators[index-1])
			compiled.WriteString(" ")
			compiled.WriteString(clauses[index-1])
		}
		if operand.Now {
			compiled.WriteString("time()")
			continue
		}
		if operand.Query == nil {
			compiled.WriteString(operand.Number)
			continue
		}
		compiledQuery, err := compileCountingQuery(operand.Query, reporting)
		if err != nil {
			return "", err
		}
		compiled.WriteString("(")
		compiled.WriteString(compiledQuery)
		compiled.WriteString(")")
	}
	return compiled.String(), nil
}

// reportingSelector is the reporting filter compiled to the series it selects, or "" where there is none.
func reportingSelector(filter, grammarVersion string) (string, error) {
	if filter == "" {
		return "", nil
	}
	parsed, err := dqlsyntax.ParseQuery(filter, grammarVersion)
	if err != nil {
		return "", fmt.Errorf("the reporting filter: %w", err)
	}
	operands := parsed.Expression.Operands
	if len(operands) != 1 || operands[0].Query == nil || len(operands[0].Query.Stages) > 0 {
		return "", errors.New("the reporting filter is a filter alone, with no stages or arithmetic")
	}
	selector, err := compileQuery(operands[0].Query)
	if err != nil {
		return "", fmt.Errorf("the reporting filter: %w", err)
	}
	return selector, nil
}

// compileCountingQuery compiles a query; one ending in an ungrouped count answers 0 while the reporting series exist.
func compileCountingQuery(query *dqlsyntax.Query, reporting string) (string, error) {
	compiled, err := compileQuery(query)
	if err != nil || reporting == "" || !endsInUngroupedCount(query) {
		return compiled, err
	}
	return compiled + " or on() (count(" + reporting + ") * 0)", nil
}

func endsInUngroupedCount(query *dqlsyntax.Query) bool {
	if len(query.Stages) == 0 {
		return false
	}
	last := query.Stages[len(query.Stages)-1]
	return last.Function == "count" && len(last.Grouping) == 0
}

// compiledExpression is PromQL text and whether it is a union, which binds looser than a comparison.
type compiledExpression struct {
	text    string
	isUnion bool
}

func compileQuery(query *dqlsyntax.Query) (string, error) {
	if query.Filter == nil {
		return "", fmt.Errorf("a PromQL query needs a filter naming its series")
	}
	conjunctions, err := conjunctionsOf(query.Filter, false)
	if err != nil {
		return "", err
	}
	if len(conjunctions) > SelectorLimit {
		return "", fmt.Errorf("the filter expands to %d selectors, more than %d", len(conjunctions), SelectorLimit)
	}
	var perSelector, overUnion []dqlsyntax.Stage
	for _, stage := range query.Stages {
		if reason, refused := refusals[stage.Function]; refused {
			return "", fmt.Errorf("%q does not compile to PromQL: %s", stage.Function, reason)
		}
		switch dqlsyntax.Functions[stage.Function].Category {
		case dqlsyntax.ShapingCategory, dqlsyntax.TimeCategory:
			perSelector = append(perSelector, stage)
		default:
			overUnion = append(overUnion, stage)
		}
	}
	var selected []string
	for _, conjunction := range conjunctions {
		selector, err := selectorOf(conjunction)
		if err != nil {
			return "", err
		}
		selected = append(selected, applySelectorStages(selector, perSelector))
	}
	compiled := compiledExpression{text: strings.Join(selected, " or "), isUnion: len(selected) > 1}
	histogramBuckets := allHistogramBuckets(conjunctions)
	for _, stage := range overUnion {
		if compiled, err = applyStage(compiled, stage, histogramBuckets); err != nil {
			return "", err
		}
		histogramBuckets = false
	}
	return compiled.text, nil
}

func applySelectorStages(selector string, stages []dqlsyntax.Stage) string {
	compiled := selector
	for _, stage := range stages {
		if stage.Function == "rate" {
			compiled = "rate(" + compiled + "[" + stage.Duration + "])"
		}
	}
	for _, stage := range stages {
		if stage.Function == "rename" {
			compiled = fmt.Sprintf(`label_replace(%s, %s, "$1", %s, "(.*)")`,
				compiled, strconv.Quote(stage.Fields[1]), strconv.Quote(stage.Fields[0]))
		}
	}
	return compiled
}

var aggregationOperators = map[string]string{
	"avg":   "avg",
	"count": "count",
	"max":   "max",
	"min":   "min",
	"stdev": "stddev",
	"sum":   "sum",
}

func applyStage(inner compiledExpression, stage dqlsyntax.Stage, histogramBuckets bool) (compiledExpression, error) {
	function := stage.Function
	switch function {
	case "avg", "count", "max", "min", "stdev", "sum":
		if len(stage.Fields) > 0 {
			return compiledExpression{}, overSeriesTakesNoField(function)
		}
		return aggregated(aggregationOperators[function], stage.Grouping, "", inner.text), nil
	case "distinct_count":
		innerGrouping := append(slices.Clone(stage.Grouping), stage.Fields[0])
		return aggregated("count", stage.Grouping, "", aggregated("count", innerGrouping, "", inner.text).text), nil
	case "quantile":
		if len(stage.Fields) > 0 {
			return compiledExpression{}, overSeriesTakesNoField(function)
		}
		if histogramBuckets {
			buckets := aggregated("sum", append([]string{"le"}, stage.Grouping...), "", inner.text)
			return compiledExpression{text: "histogram_quantile(" + stage.Number + ", " + buckets.text + ")"}, nil
		}
		return aggregated("quantile", stage.Grouping, stage.Number+", ", inner.text), nil
	case "topk", "bottomk":
		return aggregated(function, stage.Grouping, stage.Number+", ", inner.text), nil
	case "sort", "sort_desc":
		if stage.SortKey != dqlsyntax.ResultValue {
			return compiledExpression{}, fmt.Errorf("%q does not compile to PromQL by a field: PromQL sorts by value", function)
		}
		return compiledExpression{text: function + "(" + inner.text + ")"}, nil
	case "where":
		if stage.Comparison.Subject != dqlsyntax.ResultValue {
			return compiledExpression{}, fmt.Errorf("%q does not compile to PromQL on a field: PromQL compares a series' value", function)
		}
		text := inner.text
		if inner.isUnion {
			text = "(" + text + ")"
		}
		return compiledExpression{text: text + " " + stage.Comparison.Operator + " " + stage.Comparison.Number}, nil
	}
	return compiledExpression{}, fmt.Errorf("%q does not compile to PromQL", function)
}

func overSeriesTakesNoField(function string) error {
	return fmt.Errorf("over series, %q takes no field: it computes over the series' values", function)
}

func aggregated(operator string, grouping []string, parameter, inner string) compiledExpression {
	if len(grouping) == 0 {
		return compiledExpression{text: operator + "(" + parameter + inner + ")"}
	}
	return compiledExpression{text: operator + " by (" + strings.Join(grouping, ", ") + ") (" + parameter + inner + ")"}
}

func allHistogramBuckets(conjunctions [][]labelMatcher) bool {
	for _, conjunction := range conjunctions {
		bucket := slices.ContainsFunc(conjunction, func(matcher labelMatcher) bool {
			return matcher.label == "__name__" && matcher.operator == "=" && strings.HasSuffix(matcher.value, "_bucket")
		})
		if !bucket {
			return false
		}
	}
	return true
}

type labelMatcher struct {
	label    string
	operator string
	value    string
}

// conjunctionsOf rewrites a filter into a disjunction of conjunctions, each one selector.
func conjunctionsOf(node dqlsyntax.FilterNode, negated bool) ([][]labelMatcher, error) {
	switch filter := node.(type) {
	case dqlsyntax.AnyOf:
		if matcher, joins := sameLabelAlternation(filter.Clauses); joins && !negated {
			return [][]labelMatcher{{matcher}}, nil
		}
		if negated {
			return productOf(filter.Clauses, true)
		}
		return unionOf(filter.Clauses, false)
	case dqlsyntax.AllOf:
		if negated {
			return unionOf(filter.Clauses, true)
		}
		return productOf(filter.Clauses, false)
	case dqlsyntax.NoneOf:
		return conjunctionsOf(filter.Clause, !negated)
	case dqlsyntax.FieldMatch:
		return [][]labelMatcher{{matcherOf(filter.Field, filter.Value, negated)}}, nil
	case dqlsyntax.TermMatch:
		return nil, fmt.Errorf("a term with no field does not compile to PromQL: a series carries labels, not text")
	case dqlsyntax.FieldRange:
		return nil, fmt.Errorf("a range does not compile to PromQL: a label matcher has no range")
	case dqlsyntax.NestedMatch:
		return nil, fmt.Errorf("a nested field does not compile to PromQL: a series' labels are flat")
	}
	return nil, fmt.Errorf("a %T does not compile to PromQL", node)
}

func unionOf(clauses []dqlsyntax.FilterNode, negated bool) ([][]labelMatcher, error) {
	var union [][]labelMatcher
	for _, clause := range clauses {
		conjunctions, err := conjunctionsOf(clause, negated)
		if err != nil {
			return nil, err
		}
		union = append(union, conjunctions...)
		if len(union) > SelectorLimit {
			return union, nil
		}
	}
	return union, nil
}

func productOf(clauses []dqlsyntax.FilterNode, negated bool) ([][]labelMatcher, error) {
	product := [][]labelMatcher{nil}
	for _, clause := range clauses {
		conjunctions, err := conjunctionsOf(clause, negated)
		if err != nil {
			return nil, err
		}
		var next [][]labelMatcher
		for _, prefix := range product {
			for _, conjunction := range conjunctions {
				next = append(next, append(slices.Clone(prefix), conjunction...))
			}
		}
		if product = next; len(product) > SelectorLimit {
			return product, nil
		}
	}
	return product, nil
}

// sameLabelAlternation joins values of one field into one regular-expression matcher.
func sameLabelAlternation(clauses []dqlsyntax.FilterNode) (labelMatcher, bool) {
	var field string
	var alternatives []string
	for _, clause := range clauses {
		match, isMatch := clause.(dqlsyntax.FieldMatch)
		if !isMatch || (field != "" && match.Field != field) {
			return labelMatcher{}, false
		}
		field = match.Field
		alternatives = append(alternatives, valueExpression(match.Value))
	}
	return labelMatcher{label: field, operator: "=~", value: strings.Join(alternatives, "|")}, true
}

func matcherOf(field string, value dqlsyntax.Value, negated bool) labelMatcher {
	switch {
	case value.IsPresence() && negated:
		return labelMatcher{label: field, operator: "=", value: ""}
	case value.IsPresence():
		return labelMatcher{label: field, operator: "=~", value: ".+"}
	case (value.HasWildcard() || holdsVariable(value)) && negated:
		return labelMatcher{label: field, operator: "!~", value: valueExpression(value)}
	case value.HasWildcard() || holdsVariable(value):
		return labelMatcher{label: field, operator: "=~", value: valueExpression(value)}
	case negated:
		return labelMatcher{label: field, operator: "!=", value: value.Text()}
	}
	return labelMatcher{label: field, operator: "=", value: value.Text()}
}

// holdsVariable reports whether an unquoted value references a variable, which a dashboard tool substitutes as a regular expression when several values are chosen.
func holdsVariable(value dqlsyntax.Value) bool {
	return !value.Quoted && dqlsyntax.VariableReferencePattern.MatchString(value.Text())
}

func valueExpression(value dqlsyntax.Value) string {
	if value.IsPresence() {
		return ".+"
	}
	quoted := make([]string, len(value.Parts))
	for index, part := range value.Parts {
		quoted[index] = dqlsyntax.EscapeAroundVariables(part, regexp.QuoteMeta)
	}
	return strings.Join(quoted, ".*")
}

func (matcher labelMatcher) matchesEmpty() bool {
	switch matcher.operator {
	case "=":
		return matcher.value == ""
	case "!=":
		return matcher.value != ""
	}
	matchesEmpty, _ := regexp.MatchString("^(?:"+matcher.value+")$", "")
	return matchesEmpty == (matcher.operator == "=~")
}

var (
	metricNamePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNamePattern  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func selectorOf(conjunction []labelMatcher) (string, error) {
	if !slices.ContainsFunc(conjunction, func(matcher labelMatcher) bool { return !matcher.matchesEmpty() }) {
		return "", fmt.Errorf("a selector needs a field that cannot be empty, such as __name__")
	}
	metricName := ""
	var rendered []string
	for _, matcher := range conjunction {
		if metricName == "" && matcher.label == "__name__" && matcher.operator == "=" && metricNamePattern.MatchString(matcher.value) {
			metricName = matcher.value
			continue
		}
		label := matcher.label
		if !labelNamePattern.MatchString(label) {
			label = strconv.Quote(label)
		}
		rendered = append(rendered, label+matcher.operator+strconv.Quote(matcher.value))
	}
	if len(rendered) == 0 {
		return metricName, nil
	}
	return metricName + "{" + strings.Join(rendered, ",") + "}", nil
}
