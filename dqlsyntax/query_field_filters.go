package dqlsyntax

// FieldValues answers the values the query's filters compare the field with, in the order written.
func (parsed ParsedQuery) FieldValues(field string) []string {
	var values []string
	for _, node := range parsed.filterNodes() {
		if match, matches := node.(FieldMatch); matches && match.Field == field {
			values = append(values, match.Value.Text())
		}
	}
	return values
}

// FieldRanges answers the ranges the query's filters put on the field, in the order written.
func (parsed ParsedQuery) FieldRanges(field string) []FieldRange {
	var ranges []FieldRange
	for _, node := range parsed.filterNodes() {
		if fieldRange, matches := node.(FieldRange); matches && fieldRange.Field == field {
			ranges = append(ranges, fieldRange)
		}
	}
	return ranges
}

func (parsed ParsedQuery) filterNodes() []FilterNode {
	var nodes []FilterNode
	for _, operand := range parsed.Expression.Operands {
		if operand.Query != nil && operand.Query.Filter != nil {
			nodes = appendFilterNodes(nodes, operand.Query.Filter)
		}
	}
	return nodes
}

func appendFilterNodes(nodes []FilterNode, node FilterNode) []FilterNode {
	nodes = append(nodes, node)
	switch filter := node.(type) {
	case AnyOf:
		for _, clause := range filter.Clauses {
			nodes = appendFilterNodes(nodes, clause)
		}
	case AllOf:
		for _, clause := range filter.Clauses {
			nodes = appendFilterNodes(nodes, clause)
		}
	case NoneOf:
		nodes = appendFilterNodes(nodes, filter.Clause)
	case NestedMatch:
		nodes = appendFilterNodes(nodes, filter.Filter)
	}
	return nodes
}
