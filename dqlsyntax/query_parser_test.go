package dqlsyntax

import (
	"reflect"
	"testing"
)

func parsedQuery(t *testing.T, text string) ParsedQuery {
	t.Helper()
	parsed, err := ParseQuery(text, "1.0")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return parsed
}

func TestAFilterAndItsStagesParseIntoTheTree(t *testing.T) {
	parsed := parsedQuery(t, `job:(a or b) and not env:c | rate(5m) | sum() by job, env`)
	want := &Query{
		Filter: AllOf{Clauses: []FilterNode{
			AnyOf{Clauses: []FilterNode{
				FieldMatch{Field: "job", Value: Value{Parts: []string{"a"}}},
				FieldMatch{Field: "job", Value: Value{Parts: []string{"b"}}},
			}},
			NoneOf{Clause: FieldMatch{Field: "env", Value: Value{Parts: []string{"c"}}}},
		}},
		Stages: []Stage{
			{Function: "rate", Duration: "5m"},
			{Function: "sum", Grouping: []string{"job", "env"}},
		},
	}
	if got := parsed.Expression.Operands[0].Query; !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %#v, want %#v", got, want)
	}
}

func TestFilterFormsParseIntoTheirNodes(t *testing.T) {
	for text, want := range map[string]FilterNode{
		`mock`:                TermMatch{Value: Value{Parts: []string{"mock"}}},
		`"a phrase"`:          TermMatch{Value: Value{Parts: []string{"a phrase"}, Quoted: true}},
		`job:mock*`:           FieldMatch{Field: "job", Value: Value{Parts: []string{"mock", ""}}},
		`job:mock\*`:          FieldMatch{Field: "job", Value: Value{Parts: []string{"mock*"}}},
		`latency_ms >= 100`:   FieldRange{Field: "latency_ms", Operator: ">=", Bound: "100"},
		`items:{ name:mock }`: NestedMatch{Field: "items", Filter: FieldMatch{Field: "name", Value: Value{Parts: []string{"mock"}}}},
		`a b`: AnyOf{Clauses: []FilterNode{
			TermMatch{Value: Value{Parts: []string{"a"}}},
			TermMatch{Value: Value{Parts: []string{"b"}}},
		}},
	} {
		if got := parsedQuery(t, text).Expression.Operands[0].Query.Filter; !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %#v, want %#v", text, got, want)
		}
	}
}

func TestAPresenceValueIsALoneUnescapedWildcard(t *testing.T) {
	present := parsedQuery(t, `job:*`).Expression.Operands[0].Query.Filter.(FieldMatch).Value
	escaped := parsedQuery(t, `job:\*`).Expression.Operands[0].Query.Filter.(FieldMatch).Value
	if !present.IsPresence() || escaped.IsPresence() || escaped.HasWildcard() {
		t.Errorf("job:* presence = %v; job:\\* presence = %v, wildcard = %v", present.IsPresence(), escaped.IsPresence(), escaped.HasWildcard())
	}
}

func TestAnEscapedKeywordIsAValue(t *testing.T) {
	want := FieldMatch{Field: "message", Value: Value{Parts: []string{"or"}}}
	if got := parsedQuery(t, `message:\or`).Expression.Operands[0].Query.Filter; !reflect.DeepEqual(got, want) {
		t.Errorf("%#v, want %#v", got, want)
	}
}

func TestStageArgumentsParseIntoTheStage(t *testing.T) {
	for text, want := range map[string]Stage{
		`a | extract(message, "id=(?P<id>\\w+)")`: {Function: "extract", Fields: []string{"message"}, Pattern: `id=(?P<id>\w+)`},
		`a | quantile(0.95, latency_ms) by app`:   {Function: "quantile", Number: "0.95", Fields: []string{"latency_ms"}, Grouping: []string{"app"}},
		`a | where(value >= -5)`:                  {Function: "where", Comparison: &Comparison{Subject: ResultValue, Operator: ">=", Number: "-5"}},
		`a | sort_desc(app)`:                      {Function: "sort_desc", SortKey: "app"},
		`a | SUM(bytes)`:                          {Function: "sum", Fields: []string{"bytes"}},
		`a | eval(x = a + b * 2)`: {Function: "eval", Assignment: &Assignment{Field: "x", Arithmetic: ArithmeticTerm{
			Operator: "+",
			Left:     &ArithmeticTerm{Field: "a"},
			Right:    &ArithmeticTerm{Operator: "*", Left: &ArithmeticTerm{Field: "b"}, Right: &ArithmeticTerm{Number: "2"}},
		}}},
	} {
		if got := parsedQuery(t, text).Expression.Operands[0].Query.Stages[0]; !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %#v, want %#v", text, got, want)
		}
	}
}

func TestArithmeticJoinsParenthesizedQueriesAndNumbers(t *testing.T) {
	parsed := parsedQuery(t, `100 * (a | rate(5m) | sum()) / (b | rate(5m) | sum())`)
	if len(parsed.Expression.Operands) != 3 || parsed.Expression.Operands[0].Number != "100" {
		t.Fatalf("operands = %#v", parsed.Expression.Operands)
	}
	if want := []string{"*", "/"}; !reflect.DeepEqual(parsed.Expression.Operators, want) {
		t.Errorf("operators = %v, want %v", parsed.Expression.Operators, want)
	}
}

func TestNowIsAnArithmeticOperandFromGrammarOneOne(t *testing.T) {
	parsed, err := ParseQuery(`(a | min() by host) - now()`, "1.1")
	if err != nil {
		t.Fatal(err)
	}
	if operands := parsed.Expression.Operands; len(operands) != 2 || !operands[1].Now || operands[1].Query != nil {
		t.Errorf("operands = %#v", operands)
	}
	if parsed.ResultKind() != TimeSeriesResult {
		t.Errorf("result kind = %v, want time series", parsed.ResultKind())
	}
}

func TestAParenthesizedFilterGroupIsNoArithmeticOperand(t *testing.T) {
	parsed := parsedQuery(t, `(job:a or job:b) and env:c | count()`)
	if len(parsed.Expression.Operands) != 1 || len(parsed.Expression.Operands[0].Query.Stages) != 1 {
		t.Errorf("expression = %#v", parsed.Expression)
	}
}

func TestTheResultKindFollowsTheStages(t *testing.T) {
	for text, want := range map[string]ResultKind{
		``:                                   ItemsResult,
		`app:mock | fields(app) | limit(10)`: ItemsResult,
		`app:mock | per_second(1m)`:          TimeSeriesResult,
		`app:mock | count() by app`:          TimeSeriesResult,
		`(a | rate(5m) | sum()) * 100`:       TimeSeriesResult,
		`__name__:mock_up`:                   ItemsResult,
	} {
		if got := parsedQuery(t, text).ResultKind(); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}

func TestAGrammarVersionOutsideTheSupportedIsRefused(t *testing.T) {
	for version, want := range map[string]string{
		"1.2":    "grammar version 1.2 is not supported; supported: 1.1",
		"2.0":    "grammar version 2.0 is not supported; supported: 1.1",
		"2":      "grammar version 2 is not supported; supported: 1.1",
		"latest": `a grammar version is written <major> or <major>.<minor>, not "latest"`,
		"1.":     `a grammar version is written <major> or <major>.<minor>, not "1."`,
	} {
		if _, err := ParseQuery("a", version); err == nil || err.Error() != want {
			t.Errorf("%s: err = %v, want %q", version, err, want)
		}
	}
}

func TestAMajorAloneFloatsToItsNewestMinor(t *testing.T) {
	for stated, want := range map[string]string{"1": "1.1", "1.0": "1.0", "1.1": "1.1"} {
		parsed, err := ParseQuery("a", stated)
		if err != nil || parsed.GrammarVersion != want {
			t.Errorf("%s: read under %q (%v), want %q", stated, parsed.GrammarVersion, err, want)
		}
	}
	if _, err := ParseQuery("(a | min()) - now()", "1"); err != nil {
		t.Errorf("a query stating major 1 reads now(), which 1.1 adds: %v", err)
	}
}

func TestTheMatcherRefusesWhatTheGrammarDoesNot(t *testing.T) {
	grammar := readGrammar(t, "grammar/1.0.ebnf")
	stageWithoutPipe := []grammarToken{
		{kind: grammarUnquotedToken, text: "a"},
		{kind: grammarWordToken, text: "count"},
		{kind: grammarSymbolToken, text: "("},
		{kind: grammarSymbolToken, text: ")"},
	}
	if (grammarMatcher{grammar: grammar, start: grammarStart, tokens: stageWithoutPipe}).admits() {
		t.Error("the grammar admits a stage with no | before it")
	}
}
