package promqlbackend

import (
	"fmt"
	"slices"
	"strings"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

// arithmeticSide is one side of an operator: whether it answers series, and the fields a statistic groups them by.
type arithmeticSide struct {
	answersSeries  bool
	groupingStated bool
	grouping       []string
}

func arithmeticSideOf(operand dqlsyntax.Operand) arithmeticSide {
	if operand.Query == nil {
		return arithmeticSide{}
	}
	grouping, stated := lastGrouping(operand.Query)
	return arithmeticSide{answersSeries: true, groupingStated: stated, grouping: grouping}
}

func lastGrouping(query *dqlsyntax.Query) ([]string, bool) {
	for index := len(query.Stages) - 1; index >= 0; index-- {
		if category := dqlsyntax.Functions[query.Stages[index].Function].Category; category == dqlsyntax.StatisticCategory {
			return query.Stages[index].Grouping, true
		}
	}
	return nil, false
}

// pairingClauses answers each operator's matching clause, pairing its sides as PromQL reads them: products before sums, left to right.
func pairingClauses(expression dqlsyntax.Expression) ([]string, error) {
	clauses := make([]string, len(expression.Operators))
	pairSides := func(operatorIndex int, left, right arithmeticSide) (arithmeticSide, error) {
		clause, paired, err := pairingClause(left, right)
		clauses[operatorIndex] = clause
		return paired, err
	}
	product := arithmeticSideOf(expression.Operands[0])
	var sum arithmeticSide
	sumOperatorIndex := -1
	addProduct := func() (err error) {
		if sumOperatorIndex < 0 {
			sum = product
			return nil
		}
		sum, err = pairSides(sumOperatorIndex, sum, product)
		return err
	}
	for index, operator := range expression.Operators {
		operand := arithmeticSideOf(expression.Operands[index+1])
		if operator == "*" || operator == "/" {
			paired, err := pairSides(index, product, operand)
			if err != nil {
				return nil, err
			}
			product = paired
			continue
		}
		if err := addProduct(); err != nil {
			return nil, err
		}
		sumOperatorIndex, product = index, operand
	}
	if err := addProduct(); err != nil {
		return nil, err
	}
	return clauses, nil
}

// pairingClause answers the clause pairing two sides, and the side their result is: the one grouping by more fields.
func pairingClause(left, right arithmeticSide) (string, arithmeticSide, error) {
	if !left.answersSeries {
		return "", right, nil
	}
	if !right.answersSeries {
		return "", left, nil
	}
	if !left.groupingStated || !right.groupingStated {
		return "", arithmeticSide{answersSeries: true}, nil
	}
	var shared []string
	for _, field := range left.grouping {
		if slices.Contains(right.grouping, field) {
			shared = append(shared, field)
		}
	}
	leftHoldsRight := holdsEveryField(left.grouping, right.grouping)
	rightHoldsLeft := holdsEveryField(right.grouping, left.grouping)
	switch {
	case leftHoldsRight && rightHoldsLeft && len(shared) == 0:
		return "", left, nil
	case leftHoldsRight && rightHoldsLeft:
		return onClause(shared) + " ", left, nil
	case leftHoldsRight:
		return onClause(shared) + " group_left () ", left, nil
	case rightHoldsLeft:
		return onClause(shared) + " group_right () ", right, nil
	}
	return "", arithmeticSide{}, fmt.Errorf(
		"arithmetic between queries grouped by (%s) and by (%s) does not compile to PromQL: "+
			"PromQL pairs many series with one, so one side groups by every field of the other",
		strings.Join(left.grouping, ", "), strings.Join(right.grouping, ", "))
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
