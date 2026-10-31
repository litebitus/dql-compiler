package dqlsyntax

import (
	"errors"
	"fmt"
	"strings"
)

// ParseQuery reads text as a query written in grammarVersion.
func ParseQuery(text, grammarVersion string) (ParsedQuery, error) {
	parsed, _, err := parseRecordingTokens(text, grammarVersion)
	return parsed, err
}

type grammarTokenKind int

const (
	grammarWordToken grammarTokenKind = iota
	grammarSymbolToken
	grammarUnquotedToken
	grammarQuotedToken
	grammarNumberToken
	grammarDurationToken
)

// grammarToken is one token as the grammar names it, recorded as the parser takes it.
type grammarToken struct {
	kind grammarTokenKind
	text string
}

type queryParser struct {
	reader   queryReader
	at       int
	recorded []grammarToken
}

func parseRecordingTokens(text, grammarVersion string) (ParsedQuery, []grammarToken, error) {
	if err := checkGrammarVersion(grammarVersion); err != nil {
		return ParsedQuery{}, nil, err
	}
	parser := &queryParser{reader: queryReader{runes: []rune(text)}}
	expression, err := parser.parseExpression()
	if err != nil {
		return ParsedQuery{}, parser.recorded, err
	}
	return ParsedQuery{GrammarVersion: grammarVersion, Expression: expression}, parser.recorded, nil
}

func (parser *queryParser) take(token queryToken, kind grammarTokenKind) {
	text := token.text
	if kind == grammarWordToken {
		text = strings.ToLower(text)
	}
	parser.recorded = append(parser.recorded, grammarToken{kind: kind, text: text})
	parser.at = token.end
}

func (parser *queryParser) takeLiteral(token queryToken) {
	if token.kind == quotedToken {
		parser.take(token, grammarQuotedToken)
		return
	}
	parser.take(token, grammarUnquotedToken)
}

func (parser *queryParser) peekFilter() (queryToken, error) {
	return parser.reader.filterTokenAt(parser.at)
}

func (parser *queryParser) peekStage() (queryToken, error) {
	return parser.reader.stageTokenAt(parser.at)
}

func (parser *queryParser) nextFilter() (queryToken, error) {
	token, err := parser.peekFilter()
	if err == nil && token.kind == endToken {
		return token, errors.New("the query ends early")
	}
	return token, err
}

func (parser *queryParser) parseExpression() (Expression, error) {
	operand, standsAlone, err := parser.parseOperand(true)
	if err != nil {
		return Expression{}, err
	}
	expression := Expression{Operands: []Operand{operand}}
	for {
		following, err := parser.peekStage()
		if err != nil {
			return Expression{}, err
		}
		if following.kind == endToken {
			break
		}
		if !standsAlone || following.kind != symbolToken || !strings.Contains("+-*/", following.text) {
			return Expression{}, parser.unexpected()
		}
		parser.take(following, grammarSymbolToken)
		if operand, standsAlone, err = parser.parseOperand(false); err != nil {
			return Expression{}, err
		}
		expression.Operators = append(expression.Operators, following.text)
		expression.Operands = append(expression.Operands, operand)
	}
	if len(expression.Operands) > 1 {
		for _, operand := range expression.Operands {
			if operand.Query != nil && !operand.Query.answersTimeSeries() {
				return Expression{}, errors.New("arithmetic combines queries that answer time series")
			}
		}
	}
	return expression, nil
}

func (parser *queryParser) unexpected() error {
	token, err := parser.peekFilter()
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected %s", describeToken(token))
}

// parseOperand reads an operand; standsAlone is whether an arithmetic operator may follow it.
func (parser *queryParser) parseOperand(first bool) (Operand, bool, error) {
	start := parser.reader.spacesEnd(parser.at)
	opensOperand := start < len(parser.reader.runes) && parser.reader.runes[start] == '('
	if opensOperand && (!first || parser.reader.holdsTopLevelPipe(start)) {
		opening, _ := parser.peekStage()
		parser.take(opening, grammarSymbolToken)
		query, err := parser.parseQuery()
		if err != nil {
			return Operand{}, false, err
		}
		closing, err := parser.peekStage()
		if err != nil {
			return Operand{}, false, err
		}
		if !closing.isSymbol(")") {
			return Operand{}, false, errors.New("a parenthesis is not closed")
		}
		parser.take(closing, grammarSymbolToken)
		return Operand{Query: query}, true, nil
	}
	if !first || parser.reader.numberOperandAt(start) {
		number, err := parser.peekStage()
		if err != nil {
			return Operand{}, false, err
		}
		if number.kind != numberToken {
			return Operand{}, false, fmt.Errorf("arithmetic needs a query in parentheses or a number, not %s", describeToken(number))
		}
		parser.take(number, grammarNumberToken)
		return Operand{Number: number.text}, true, nil
	}
	query, err := parser.parseQuery()
	return Operand{Query: query}, false, err
}

func (parser *queryParser) parseQuery() (*Query, error) {
	query := &Query{}
	following, err := parser.peekFilter()
	if err != nil {
		return nil, err
	}
	if !endsFilter(following) {
		if query.Filter, err = parser.parseDisjunction(); err != nil {
			return nil, err
		}
	}
	stageOrder := stageOrder{}
	for {
		following, err := parser.peekStage()
		if err != nil {
			return nil, err
		}
		if !following.isSymbol("|") {
			return query, nil
		}
		parser.take(following, grammarSymbolToken)
		stage, err := parser.parseStage(&stageOrder)
		if err != nil {
			return nil, err
		}
		query.Stages = append(query.Stages, stage)
		after, err := parser.peekStage()
		if err != nil {
			return nil, err
		}
		if after.kind != endToken && !after.isSymbol("|") && !after.isSymbol(")") {
			return nil, fmt.Errorf("unexpected %s", describeToken(after))
		}
	}
}

func endsFilter(token queryToken) bool {
	return token.kind == endToken || token.isSymbol(")") || token.isSymbol("|") || token.isSymbol("}")
}

// parseDisjunction reads clauses joined by `or`, or by nothing, as DQL reads terms with no operator between them.
func (parser *queryParser) parseDisjunction() (FilterNode, error) {
	var clauses []FilterNode
	for {
		conjunction, err := parser.parseConjunction()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, conjunction)
		following, err := parser.peekFilter()
		if err != nil {
			return nil, err
		}
		if endsFilter(following) {
			return anyOf(clauses), nil
		}
		if following.isKeyword("or") {
			parser.take(following, grammarWordToken)
		}
	}
}

func (parser *queryParser) parseConjunction() (FilterNode, error) {
	var clauses []FilterNode
	for {
		negation, err := parser.parseNegation()
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, negation)
		taken, err := parser.takeKeyword("and")
		if err != nil {
			return nil, err
		}
		if !taken {
			return allOf(clauses), nil
		}
	}
}

func (parser *queryParser) parseNegation() (FilterNode, error) {
	taken, err := parser.takeKeyword("not")
	if err != nil {
		return nil, err
	}
	if !taken {
		return parser.parsePrimary()
	}
	negated, err := parser.parseNegation()
	if err != nil {
		return nil, err
	}
	return NoneOf{Clause: negated}, nil
}

func (parser *queryParser) takeKeyword(keyword string) (bool, error) {
	following, err := parser.peekFilter()
	if err != nil || !following.isKeyword(keyword) {
		return false, err
	}
	parser.take(following, grammarWordToken)
	return true, nil
}

func (parser *queryParser) parsePrimary() (FilterNode, error) {
	primary, err := parser.nextFilter()
	if err != nil {
		return nil, err
	}
	if primary.isSymbol("(") {
		parser.take(primary, grammarSymbolToken)
		inner, err := parser.parseDisjunction()
		if err != nil {
			return nil, err
		}
		return inner, parser.expectFilterClose(")", "a parenthesis is not closed")
	}
	if !primary.isLiteral() || primary.isAnyFilterKeyword() {
		return nil, fmt.Errorf("unexpected %s", describeToken(primary))
	}
	parser.takeLiteral(primary)
	following, err := parser.peekFilter()
	if err != nil {
		return nil, err
	}
	switch {
	case following.isSymbol(":"):
		parser.take(following, grammarSymbolToken)
		return parser.parseFieldValue(primary.text)
	case following.kind == symbolToken && rangeOperators[following.text]:
		parser.take(following, grammarSymbolToken)
		return parser.parseRange(primary.text, following.text)
	}
	return TermMatch{Value: valueOf(primary)}, nil
}

var rangeOperators = map[string]bool{"<": true, "<=": true, ">": true, ">=": true}

func (parser *queryParser) expectFilterClose(symbol, refusal string) error {
	closing, err := parser.peekFilter()
	if err != nil {
		return err
	}
	if !closing.isSymbol(symbol) {
		return errors.New(refusal)
	}
	parser.take(closing, grammarSymbolToken)
	return nil
}

func (parser *queryParser) parseFieldValue(field string) (FilterNode, error) {
	opening, err := parser.peekFilter()
	if err != nil {
		return nil, err
	}
	if opening.isSymbol("{") {
		parser.take(opening, grammarSymbolToken)
		nested, err := parser.parseDisjunction()
		if err != nil {
			return nil, err
		}
		return NestedMatch{Field: field, Filter: nested}, parser.expectFilterClose("}", "a brace is not closed")
	}
	return parser.parseValueList(field)
}

// parseValueList reads a field's value, or its parenthesized values joined by `or`, `and` and `not`.
func (parser *queryParser) parseValueList(field string) (FilterNode, error) {
	opening, err := parser.peekFilter()
	if err != nil {
		return nil, err
	}
	if !opening.isSymbol("(") {
		return parser.parseValue(field)
	}
	parser.take(opening, grammarSymbolToken)
	var clauses []FilterNode
	for {
		conjunction, err := parser.parseValueConjunction(field)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, conjunction)
		taken, err := parser.takeKeyword("or")
		if err != nil {
			return nil, err
		}
		if !taken {
			return anyOf(clauses), parser.expectFilterClose(")", "a parenthesis is not closed")
		}
	}
}

func (parser *queryParser) parseValueConjunction(field string) (FilterNode, error) {
	var clauses []FilterNode
	for {
		negation, err := parser.parseValueNegation(field)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, negation)
		taken, err := parser.takeKeyword("and")
		if err != nil {
			return nil, err
		}
		if !taken {
			return allOf(clauses), nil
		}
	}
}

func (parser *queryParser) parseValueNegation(field string) (FilterNode, error) {
	taken, err := parser.takeKeyword("not")
	if err != nil {
		return nil, err
	}
	if !taken {
		return parser.parseValueList(field)
	}
	negated, err := parser.parseValueNegation(field)
	if err != nil {
		return nil, err
	}
	return NoneOf{Clause: negated}, nil
}

func (parser *queryParser) parseValue(field string) (FilterNode, error) {
	value, err := parser.nextFilter()
	if err != nil {
		return nil, err
	}
	if !value.isLiteral() {
		return nil, fmt.Errorf("a field needs a value, not %s", describeToken(value))
	}
	parser.takeLiteral(value)
	return FieldMatch{Field: field, Value: valueOf(value)}, nil
}

func (parser *queryParser) parseRange(field, operator string) (FilterNode, error) {
	bound, err := parser.peekFilter()
	if err != nil || !bound.isLiteral() {
		return nil, fmt.Errorf("a range on %s needs a value", field)
	}
	parser.takeLiteral(bound)
	return FieldRange{Field: field, Operator: operator, Bound: bound.text}, nil
}

func valueOf(token queryToken) Value {
	return Value{Parts: token.parts, Quoted: token.kind == quotedToken}
}

func anyOf(clauses []FilterNode) FilterNode {
	if len(clauses) == 1 {
		return clauses[0]
	}
	return AnyOf{Clauses: clauses}
}

func allOf(clauses []FilterNode) FilterNode {
	if len(clauses) == 1 {
		return clauses[0]
	}
	return AllOf{Clauses: clauses}
}
