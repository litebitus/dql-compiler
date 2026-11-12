package dqlsyntax

import "slices"

// resultFields are the fields each series of a result carries, known where every query in it states its grouping.
type resultFields struct {
	answersSeries bool
	known         bool
	fields        []string
}

// QueryResultFields answers the fields each series of the query's result carries: a query's last grouping, and across
// arithmetic the side grouping by every field of the other. They are unknown where a query states no grouping, its
// series keeping the backend's own fields, or where each side groups by a field the other lacks.
func (parsed ParsedQuery) QueryResultFields() ([]string, bool) {
	result := expressionResultFields(parsed.Expression)
	return result.fields, result.known
}

// expressionResultFields pairs an expression's operands as it reads them, products before sums, left to right.
func expressionResultFields(expression Expression) resultFields {
	product := operandResultFields(expression.Operands[0])
	sum, summing := resultFields{}, false
	for index, operator := range expression.Operators {
		operand := operandResultFields(expression.Operands[index+1])
		if operator == "*" || operator == "/" {
			product = pairedResultFields(product, operand)
			continue
		}
		if summing {
			sum = pairedResultFields(sum, product)
		} else {
			sum, summing = product, true
		}
		product = operand
	}
	if summing {
		return pairedResultFields(sum, product)
	}
	return product
}

func operandResultFields(operand Operand) resultFields {
	if operand.Query == nil {
		return resultFields{known: true}
	}
	for index := len(operand.Query.Stages) - 1; index >= 0; index-- {
		if stage := operand.Query.Stages[index]; Functions[stage.Function].Category == StatisticCategory {
			return resultFields{answersSeries: true, known: true, fields: stage.Grouping}
		}
	}
	return resultFields{answersSeries: true}
}

// pairedResultFields are the fields of two sides paired by an operator: the side grouping by every field of the other.
func pairedResultFields(left, right resultFields) resultFields {
	switch {
	case !left.answersSeries:
		return right
	case !right.answersSeries:
		return left
	case !left.known || !right.known:
		return resultFields{answersSeries: true}
	case holdsEveryField(left.fields, right.fields):
		return left
	case holdsEveryField(right.fields, left.fields):
		return right
	}
	return resultFields{answersSeries: true}
}

func holdsEveryField(grouping, fields []string) bool {
	for _, field := range fields {
		if !slices.Contains(grouping, field) {
			return false
		}
	}
	return true
}
