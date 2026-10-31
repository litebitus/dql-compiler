// Package dqlsyntax parses a query into its syntax tree, shared by every backend.
package dqlsyntax

import "strings"

// ParsedQuery is a query read under one grammar version.
type ParsedQuery struct {
	GrammarVersion string
	Expression     Expression
}

// ResultKind says what a query answers.
type ResultKind int

const (
	// ItemsResult is the backend's own items: records, spans or series.
	ItemsResult ResultKind = iota
	// TimeSeriesResult is time series, one per value of the last grouping.
	TimeSeriesResult
)

// ResultKind answers what the query returns, so a caller knows to show a table or a chart.
func (parsed ParsedQuery) ResultKind() ResultKind {
	if len(parsed.Expression.Operands) > 1 || parsed.Expression.Operands[0].Query.answersTimeSeries() {
		return TimeSeriesResult
	}
	return ItemsResult
}

// Expression is operands joined by arithmetic operators, evaluated with `*` and `/` before `+` and `-`.
type Expression struct {
	Operands  []Operand
	Operators []string
}

// Operand is a query or a number; Query is nil for a number.
type Operand struct {
	Query  *Query
	Number string
}

// Query is a filter and the stages that compute over what it selects; Filter is nil for a blank filter.
type Query struct {
	Filter FilterNode
	Stages []Stage
}

func (query *Query) answersTimeSeries() bool {
	if query == nil {
		return false
	}
	for _, stage := range query.Stages {
		if category := Functions[stage.Function].Category; category == TimeCategory || category == StatisticCategory {
			return true
		}
	}
	return false
}

// FilterNode is one node of a filter's tree.
type FilterNode interface {
	filterNode()
}

// AnyOf matches what any of its clauses matches.
type AnyOf struct{ Clauses []FilterNode }

// AllOf matches what every one of its clauses matches.
type AllOf struct{ Clauses []FilterNode }

// NoneOf matches what its clause does not.
type NoneOf struct{ Clause FilterNode }

// TermMatch matches a value in any field.
type TermMatch struct{ Value Value }

// FieldMatch matches a value in one field.
type FieldMatch struct {
	Field string
	Value Value
}

// FieldRange matches a field's value against a bound.
type FieldRange struct {
	Field    string
	Operator string
	Bound    string
}

// NestedMatch matches the filter inside a nested field.
type NestedMatch struct {
	Field  string
	Filter FilterNode
}

func (AnyOf) filterNode()       {}
func (AllOf) filterNode()       {}
func (NoneOf) filterNode()      {}
func (TermMatch) filterNode()   {}
func (FieldMatch) filterNode()  {}
func (FieldRange) filterNode()  {}
func (NestedMatch) filterNode() {}

// Value is a value as written, split at each unescaped `*`.
type Value struct {
	Parts  []string
	Quoted bool
}

// HasWildcard reports whether the value holds an unescaped `*`.
func (value Value) HasWildcard() bool { return len(value.Parts) > 1 }

// IsPresence reports whether the value is a lone unescaped `*`, matching any item carrying the field.
func (value Value) IsPresence() bool {
	return !value.Quoted && len(value.Parts) == 2 && value.Parts[0] == "" && value.Parts[1] == ""
}

// Text is the value with each unescaped `*` written back.
func (value Value) Text() string { return strings.Join(value.Parts, "*") }

// Stage is one function call after a `|`.
type Stage struct {
	Function   string
	Fields     []string
	Duration   string
	Number     string
	Pattern    string
	SortKey    string
	Assignment *Assignment
	Comparison *Comparison
	Grouping   []string
}

// ResultValue is the sort key and comparison subject standing for a result's value.
const ResultValue = "value"

// Assignment is `eval`'s computed field.
type Assignment struct {
	Field      string
	Arithmetic ArithmeticTerm
}

// ArithmeticTerm is a field, a number, or two terms joined by an operator.
type ArithmeticTerm struct {
	Field    string
	Number   string
	Operator string
	Left     *ArithmeticTerm
	Right    *ArithmeticTerm
}

// Comparison is `where`'s condition; Subject is `value` or a field.
type Comparison struct {
	Subject  string
	Operator string
	Number   string
}
