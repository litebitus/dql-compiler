package dqlsyntax

import (
	"errors"
	"fmt"
	"strings"
)

// stageOrder is the last stage a query has read, which every following stage may not precede.
type stageOrder struct {
	lastFunction string
}

func (order *stageOrder) admit(function string) error {
	if order.lastFunction != "" {
		last := Functions[order.lastFunction].Category
		next := Functions[function].Category
		if next < last || (next == TimeCategory && last == TimeCategory) {
			return fmt.Errorf("%q cannot follow %q", function, order.lastFunction)
		}
	}
	order.lastFunction = function
	return nil
}

func (parser *queryParser) parseStage(order *stageOrder) (Stage, error) {
	name, err := parser.peekStage()
	if err != nil {
		return Stage{}, err
	}
	if name.kind != wordToken || name.escaped {
		return Stage{}, fmt.Errorf("a stage needs a function, not %s", describeToken(name))
	}
	function := strings.ToLower(name.text)
	spec, known := Functions[function]
	if !known {
		return Stage{}, fmt.Errorf("unknown function %q", name.text)
	}
	if err := order.admit(function); err != nil {
		return Stage{}, err
	}
	parser.take(name, grammarWordToken)
	opening, err := parser.peekStage()
	if err != nil {
		return Stage{}, err
	}
	if !opening.isSymbol("(") {
		return Stage{}, fmt.Errorf("%q needs \"(\", not %s", function, describeToken(opening))
	}
	parser.take(opening, grammarSymbolToken)
	stage := Stage{Function: function}
	for index, argument := range spec.arguments {
		if index > 0 {
			separated, err := parser.takeStageSymbol(",")
			if err != nil {
				return Stage{}, err
			}
			if !separated && argument == optionalFieldArgument {
				break
			}
			if !separated {
				return Stage{}, parser.needs(function, argument)
			}
		}
		if err := parser.parseArgument(&stage, argument); err != nil {
			return Stage{}, err
		}
	}
	closing, err := parser.peekStage()
	if err != nil {
		return Stage{}, err
	}
	if !closing.isSymbol(")") {
		return Stage{}, errors.New("a parenthesis is not closed")
	}
	parser.take(closing, grammarSymbolToken)
	if spec.Grouped {
		if stage.Grouping, err = parser.parseGrouping(); err != nil {
			return Stage{}, err
		}
	}
	return stage, nil
}

func (parser *queryParser) needs(function string, argument argumentKind) error {
	found, err := parser.peekStage()
	if err != nil {
		return err
	}
	return fmt.Errorf("%q needs %s, not %s", function, argumentDescriptions[argument], describeToken(found))
}

func (parser *queryParser) takeStageSymbol(symbol string) (bool, error) {
	following, err := parser.peekStage()
	if err != nil || !following.isSymbol(symbol) {
		return false, err
	}
	parser.take(following, grammarSymbolToken)
	return true, nil
}

func (parser *queryParser) parseArgument(stage *Stage, argument argumentKind) error {
	switch argument {
	case fieldArgument:
		field, err := parser.parseStageField(stage.Function, argument)
		stage.Fields = append(stage.Fields, field)
		return err
	case optionalFieldArgument:
		following, err := parser.peekStage()
		if err != nil || following.isSymbol(")") {
			return err
		}
		field, err := parser.parseStageField(stage.Function, argument)
		stage.Fields = append(stage.Fields, field)
		return err
	case fieldListArgument:
		for {
			field, err := parser.parseStageField(stage.Function, argument)
			if err != nil {
				return err
			}
			stage.Fields = append(stage.Fields, field)
			if separated, err := parser.takeStageSymbol(","); err != nil || !separated {
				return err
			}
		}
	case durationArgument:
		return parser.takeStageToken(stage.Function, argument, durationToken, grammarDurationToken, &stage.Duration)
	case numberArgument:
		number, err := parser.parseNumber(stage.Function, argument)
		stage.Number = number
		return err
	case wholeNumberArgument:
		following, err := parser.peekStage()
		if err != nil {
			return err
		}
		if following.kind != numberToken || strings.Contains(following.text, ".") {
			return parser.needs(stage.Function, argument)
		}
		parser.take(following, grammarNumberToken)
		stage.Number = following.text
		return nil
	case patternArgument:
		return parser.takeStageToken(stage.Function, argument, quotedToken, grammarQuotedToken, &stage.Pattern)
	case assignmentArgument:
		return parser.parseAssignment(stage)
	case sortKeyArgument:
		sortKey, err := parser.parseSortKey(stage.Function, argument)
		stage.SortKey = sortKey
		return err
	case comparisonArgument:
		return parser.parseComparison(stage)
	}
	return fmt.Errorf("%q takes an argument this parser does not read", stage.Function)
}

func (parser *queryParser) takeStageToken(function string, argument argumentKind, kind tokenKind, grammarKind grammarTokenKind, into *string) error {
	following, err := parser.peekStage()
	if err != nil {
		return err
	}
	if following.kind != kind {
		return parser.needs(function, argument)
	}
	parser.take(following, grammarKind)
	*into = following.text
	return nil
}

func (parser *queryParser) parseStageField(function string, argument argumentKind) (string, error) {
	field, err := parser.peekStage()
	if err != nil {
		return "", err
	}
	if !field.isLiteral() {
		return "", parser.needs(function, argument)
	}
	parser.takeLiteral(field)
	return field.text, nil
}

func (parser *queryParser) parseNumber(function string, argument argumentKind) (string, error) {
	sign := ""
	negative, err := parser.takeStageSymbol("-")
	if err != nil {
		return "", err
	}
	if negative {
		sign = "-"
	}
	number, err := parser.peekStage()
	if err != nil {
		return "", err
	}
	if number.kind != numberToken {
		return "", parser.needs(function, argument)
	}
	parser.take(number, grammarNumberToken)
	return sign + number.text, nil
}

func (parser *queryParser) parseSortKey(function string, argument argumentKind) (string, error) {
	following, err := parser.peekStage()
	if err != nil {
		return "", err
	}
	if following.isKeyword(ResultValue) {
		parser.take(following, grammarWordToken)
		return ResultValue, nil
	}
	return parser.parseStageField(function, argument)
}

var comparisonOperators = map[string]bool{"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (parser *queryParser) parseComparison(stage *Stage) error {
	subject, err := parser.parseSortKey(stage.Function, comparisonArgument)
	if err != nil {
		return err
	}
	operator, err := parser.peekStage()
	if err != nil {
		return err
	}
	if operator.kind != symbolToken || !comparisonOperators[operator.text] {
		return fmt.Errorf("%q needs a comparison operator, not %s", stage.Function, describeToken(operator))
	}
	parser.take(operator, grammarSymbolToken)
	number, err := parser.parseNumber(stage.Function, numberArgument)
	stage.Comparison = &Comparison{Subject: subject, Operator: operator.text, Number: number}
	return err
}

func (parser *queryParser) parseAssignment(stage *Stage) error {
	field, err := parser.parseStageField(stage.Function, assignmentArgument)
	if err != nil {
		return err
	}
	assigns, err := parser.takeStageSymbol("=")
	if err != nil {
		return err
	}
	if !assigns {
		return parser.needsText(stage.Function, `"="`)
	}
	arithmetic, err := parser.parseArithmeticSum(stage.Function)
	stage.Assignment = &Assignment{Field: field, Arithmetic: arithmetic}
	return err
}

func (parser *queryParser) needsText(function, needed string) error {
	found, err := parser.peekStage()
	if err != nil {
		return err
	}
	return fmt.Errorf("%q needs %s, not %s", function, needed, describeToken(found))
}

func (parser *queryParser) parseArithmeticSum(function string) (ArithmeticTerm, error) {
	return parser.parseArithmeticLevel(function, "+-", parser.parseArithmeticProduct)
}

func (parser *queryParser) parseArithmeticProduct(function string) (ArithmeticTerm, error) {
	return parser.parseArithmeticLevel(function, "*/", parser.parseArithmeticFactor)
}

func (parser *queryParser) parseArithmeticLevel(function, operators string, parseOperand func(string) (ArithmeticTerm, error)) (ArithmeticTerm, error) {
	left, err := parseOperand(function)
	if err != nil {
		return ArithmeticTerm{}, err
	}
	for {
		operator, err := parser.peekStage()
		if err != nil {
			return ArithmeticTerm{}, err
		}
		if operator.kind != symbolToken || !strings.Contains(operators, operator.text) {
			return left, nil
		}
		parser.take(operator, grammarSymbolToken)
		right, err := parseOperand(function)
		if err != nil {
			return ArithmeticTerm{}, err
		}
		joinedLeft := left
		left = ArithmeticTerm{Operator: operator.text, Left: &joinedLeft, Right: &right}
	}
}

func (parser *queryParser) parseArithmeticFactor(function string) (ArithmeticTerm, error) {
	factor, err := parser.peekStage()
	if err != nil {
		return ArithmeticTerm{}, err
	}
	switch {
	case factor.isSymbol("("):
		parser.take(factor, grammarSymbolToken)
		inner, err := parser.parseArithmeticSum(function)
		if err != nil {
			return ArithmeticTerm{}, err
		}
		closed, err := parser.takeStageSymbol(")")
		if err != nil {
			return ArithmeticTerm{}, err
		}
		if !closed {
			return ArithmeticTerm{}, errors.New("a parenthesis is not closed")
		}
		return inner, nil
	case factor.kind == numberToken:
		parser.take(factor, grammarNumberToken)
		return ArithmeticTerm{Number: factor.text}, nil
	case factor.isLiteral():
		parser.takeLiteral(factor)
		return ArithmeticTerm{Field: factor.text}, nil
	}
	return ArithmeticTerm{}, parser.needsText(function, "a field or a number")
}

func (parser *queryParser) parseGrouping() ([]string, error) {
	following, err := parser.peekStage()
	if err != nil || !following.isKeyword("by") {
		return nil, err
	}
	parser.take(following, grammarWordToken)
	var grouping []string
	for {
		field, err := parser.peekStage()
		if err != nil {
			return nil, err
		}
		if !field.isLiteral() {
			return nil, fmt.Errorf("\"by\" needs a field, not %s", describeToken(field))
		}
		parser.takeLiteral(field)
		grouping = append(grouping, field.text)
		if separated, err := parser.takeStageSymbol(","); err != nil || !separated {
			return grouping, err
		}
	}
}
