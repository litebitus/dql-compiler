package dqlsyntax

// FunctionCategory is where a function stands in a query's stage order.
type FunctionCategory int

const (
	// ShapingCategory functions take items and answer items.
	ShapingCategory FunctionCategory = iota
	// TimeCategory functions turn what the filter selects into time series.
	TimeCategory
	// StatisticCategory functions aggregate.
	StatisticCategory
	// ResultCategory functions select and order results.
	ResultCategory
)

type argumentKind int

const (
	fieldArgument argumentKind = iota
	optionalFieldArgument
	fieldListArgument
	durationArgument
	numberArgument
	wholeNumberArgument
	patternArgument
	assignmentArgument
	sortKeyArgument
	comparisonArgument
)

var argumentDescriptions = map[argumentKind]string{
	fieldArgument:         "a field",
	optionalFieldArgument: "a field",
	fieldListArgument:     "a field",
	durationArgument:      "a duration",
	numberArgument:        "a number",
	wholeNumberArgument:   "a whole number",
	patternArgument:       "a quoted pattern",
	assignmentArgument:    "a field to assign",
	sortKeyArgument:       "value or a field",
	comparisonArgument:    "value or a field to compare",
}

// FunctionSpec is what a function takes and where it stands.
type FunctionSpec struct {
	Category  FunctionCategory
	Grouped   bool
	arguments []argumentKind
}

// Functions is the language's function vocabulary, by name.
var Functions = map[string]FunctionSpec{
	"dedup":          {Category: ShapingCategory, arguments: []argumentKind{fieldArgument}},
	"eval":           {Category: ShapingCategory, arguments: []argumentKind{assignmentArgument}},
	"extract":        {Category: ShapingCategory, arguments: []argumentKind{fieldArgument, patternArgument}},
	"fields":         {Category: ShapingCategory, arguments: []argumentKind{fieldListArgument}},
	"rename":         {Category: ShapingCategory, arguments: []argumentKind{fieldArgument, fieldArgument}},
	"per_second":     {Category: TimeCategory, Grouped: true, arguments: []argumentKind{durationArgument}},
	"rate":           {Category: TimeCategory, arguments: []argumentKind{durationArgument}},
	"avg":            {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{optionalFieldArgument}},
	"count":          {Category: StatisticCategory, Grouped: true},
	"distinct_count": {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{fieldArgument}},
	"max":            {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{optionalFieldArgument}},
	"min":            {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{optionalFieldArgument}},
	"quantile":       {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{numberArgument, optionalFieldArgument}},
	"stdev":          {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{optionalFieldArgument}},
	"sum":            {Category: StatisticCategory, Grouped: true, arguments: []argumentKind{optionalFieldArgument}},
	"bottomk":        {Category: ResultCategory, Grouped: true, arguments: []argumentKind{wholeNumberArgument}},
	"limit":          {Category: ResultCategory, arguments: []argumentKind{wholeNumberArgument}},
	"sort":           {Category: ResultCategory, arguments: []argumentKind{sortKeyArgument}},
	"sort_desc":      {Category: ResultCategory, arguments: []argumentKind{sortKeyArgument}},
	"topk":           {Category: ResultCategory, Grouped: true, arguments: []argumentKind{wholeNumberArgument}},
	"where":          {Category: ResultCategory, arguments: []argumentKind{comparisonArgument}},
}
