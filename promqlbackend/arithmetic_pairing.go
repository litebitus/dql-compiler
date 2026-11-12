package promqlbackend

import (
	"fmt"
	"slices"
	"strings"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

// arithmeticSide is one side of an operator: whether it answers series, and the fields they carry where known.
type arithmeticSide struct {
	answersSeries bool
	fieldsKnown   bool
	fields        []string
}

// arithmeticSideOf answers the side the operands from first through last make, their fields as the query's own.
func arithmeticSideOf(expression dqlsyntax.Expression, first, last int) arithmeticSide {
	operands := expression.Operands[first : last+1]
	side := dqlsyntax.ParsedQuery{Expression: dqlsyntax.Expression{
		Operands:  operands,
		Operators: expression.Operators[first:last],
	}}
	fields, known := side.QueryResultFields()
	answersSeries := slices.ContainsFunc(operands, func(operand dqlsyntax.Operand) bool { return operand.Query != nil })
	return arithmeticSide{answersSeries: answersSeries, fieldsKnown: known, fields: fields}
}

// pairingClauses answers each operator's matching clause, pairing its sides as PromQL reads them: products before sums, left to right.
func pairingClauses(expression dqlsyntax.Expression) ([]string, error) {
	clauses := make([]string, len(expression.Operators))
	pairSides := func(operatorIndex, leftFirst, rightLast int) error {
		left := arithmeticSideOf(expression, leftFirst, operatorIndex)
		right := arithmeticSideOf(expression, operatorIndex+1, rightLast)
		clause, err := pairingClause(left, right)
		clauses[operatorIndex] = clause
		return err
	}
	productFirst, sumOperatorIndex := 0, -1
	pairSum := func(productLast int) error {
		if sumOperatorIndex < 0 {
			return nil
		}
		return pairSides(sumOperatorIndex, 0, productLast)
	}
	for index, operator := range expression.Operators {
		if operator == "*" || operator == "/" {
			if err := pairSides(index, productFirst, index+1); err != nil {
				return nil, err
			}
			continue
		}
		if err := pairSum(index); err != nil {
			return nil, err
		}
		sumOperatorIndex, productFirst = index, index+1
	}
	if err := pairSum(len(expression.Operands) - 1); err != nil {
		return nil, err
	}
	return clauses, nil
}

// pairingClause answers the clause pairing two sides: on the fields they share, many to one from the side grouping by more.
func pairingClause(left, right arithmeticSide) (string, error) {
	if !left.answersSeries || !right.answersSeries || !left.fieldsKnown || !right.fieldsKnown {
		return "", nil
	}
	var shared []string
	for _, field := range left.fields {
		if slices.Contains(right.fields, field) {
			shared = append(shared, field)
		}
	}
	leftHoldsRight := holdsEveryField(left.fields, right.fields)
	rightHoldsLeft := holdsEveryField(right.fields, left.fields)
	switch {
	case leftHoldsRight && rightHoldsLeft && len(shared) == 0:
		return "", nil
	case leftHoldsRight && rightHoldsLeft:
		return onClause(shared) + " ", nil
	case leftHoldsRight:
		return onClause(shared) + " group_left () ", nil
	case rightHoldsLeft:
		return onClause(shared) + " group_right () ", nil
	}
	return "", fmt.Errorf(
		"arithmetic between queries grouped by (%s) and by (%s) does not compile to PromQL: "+
			"PromQL pairs many series with one, so one side groups by every field of the other",
		strings.Join(left.fields, ", "), strings.Join(right.fields, ", "))
}

func holdsEveryField(grouping, fields []string) bool {
	for _, field := range fields {
		if !slices.Contains(grouping, field) {
			return false
		}
	}
	return true
}

func onClause(fields []string) string {
	return "on (" + strings.Join(fields, ", ") + ")"
}
