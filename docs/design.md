# DQL compiler

Each search backend speaks a query language of its own: PromQL, LogQL, TraceQL, SQL, SPL, PPL. This compiler makes one language query all of them. The language extends DQL, the query language of OpenSearch Dashboards, with pipe stages that compute over what the filter selects. Every DQL query is a query of the language. The compiler reads a query once and compiles it into each backend's native query, so an author learns one language for every source.

The compiler is a Go library. A program embeds it to compile its own queries; a Terraform provider exposes it as functions.

## Versions

The grammar is versioned `major.minor`, apart from the Go module's release tags.

- A minor version adds to the grammar: a query written for `1.2` compiles under every `1.x` from `1.2` on.
- A major version changes what an existing query means. The compiler keeps compiling every major version it has shipped.
- The caller states the version a query is written in; the query text carries none.
- A minor names itself, and a major alone, such as `1`, floats to the newest minor the compiler holds. The parsed query states the minor it was read under.
- A major the compiler does not know, or a minor newer than its own, is refused with the versions it supports.

## Grammar

The grammar is EBNF as Go's `golang.org/x/exp/ebnf` reads it: juxtaposition is sequence, `|` alternation, `[ ]` option, `{ }` repetition, and `.` ends a production. The filter rules follow OpenSearch Dashboards' `kuery.peg`, with `|` added to the special characters. `dqlsyntax/grammar/<major>.<minor>.ebnf` is the spec the parser answers to, one file per version, and what stands between the markers here is the latest, which `make grammar` writes. Edit it there.

<!-- query grammar: written by `make grammar` from dqlsyntax/grammar; edit it there -->

Version `1.1`.

```ebnf
/* The grammar a query is written in. This file is the spec the parser answers to.

   An uppercase production is structure the parser reads. A lowercase one is a
   single token the scanner answers with, and its body says what that token
   looks like: an unquoted literal is any character but a specialCharacter,
   beginning no keyword unless escaped, and a `$` before a name opens a
   variableReference, never a bare `$name`. Keywords and function names read in
   any case. */

Expression = Operand { Operator Operand } .
Operand    = "(" Query ")" | Query | number | Now .
Operator   = "+" | "-" | "*" | "/" .
Now        = "now" "(" ")" .

Query = [ OrQuery ] { "|" Shaping } [ "|" Time ] { "|" ( Statistic | Where ) } { "|" Result } .

/* The filter, as DQL writes it. */

OrQuery              = AndQuery { [ "or" ] AndQuery } .
AndQuery             = NotQuery { "and" NotQuery } .
NotQuery             = [ "not" ] SubQuery .
SubQuery             = "(" OrQuery ")"
                     | NestedQuery
                     | RangeExpression
                     | FieldValueExpression
                     | Value .
NestedQuery          = Field ":" "{" OrQuery "}" .
RangeExpression      = Field RangeOperator literal .
FieldValueExpression = Field ":" ListOfValues .
ListOfValues         = "(" OrValues ")" | Value .
OrValues             = AndValues { "or" AndValues } .
AndValues            = NotValues { "and" NotValues } .
NotValues            = [ "not" ] ListOfValues .
RangeOperator        = "<=" | ">=" | "<" | ">" .
Field                = literal .
Value                = literal .

/* The stages, in the order a query runs them, one production per function. */

Shaping   = Dedup | Eval | Extract | Fields | Rename .
Time      = PerSecond | Rate .
Statistic = Avg | Count | DistinctCount | Max | Min | Quantile | Stdev | Sum .
Result    = Bottomk | Limit | Sort | SortDesc | Topk | Where .

Dedup   = "dedup" "(" Field ")" .
Eval    = "eval" "(" Field "=" Arithmetic ")" .
Extract = "extract" "(" Field "," quotedString ")" .
Fields  = "fields" "(" Field { "," Field } ")" .
Rename  = "rename" "(" Field "," Field ")" .

PerSecond = "per_second" "(" duration ")" [ Grouping ] .
Rate      = "rate" "(" duration ")" .

Avg           = "avg" "(" [ Field ] ")" [ Grouping ] .
Count         = "count" "(" ")" [ Grouping ] .
DistinctCount = "distinct_count" "(" Field ")" [ Grouping ] .
Max           = "max" "(" [ Field ] ")" [ Grouping ] .
Min           = "min" "(" [ Field ] ")" [ Grouping ] .
Quantile      = "quantile" "(" number [ "," Field ] ")" [ Grouping ] .
Stdev         = "stdev" "(" [ Field ] ")" [ Grouping ] .
Sum           = "sum" "(" [ Field ] ")" [ Grouping ] .

Bottomk  = "bottomk" "(" wholeNumber ")" [ Grouping ] .
Limit    = "limit" "(" wholeNumber ")" .
Sort     = "sort" "(" ResultField ")" .
SortDesc = "sort_desc" "(" ResultField ")" .
Topk     = "topk" "(" wholeNumber ")" [ Grouping ] .
Where    = "where" "(" ResultField ComparisonOperator number ")" .

Grouping           = "by" Field { "," Field } .
ResultField        = "value" | Field .
ComparisonOperator = "==" | "!=" | "<=" | ">=" | "<" | ">" .
Arithmetic         = ArithmeticTerm { Operator ArithmeticTerm } .
ArithmeticTerm     = Field | number | "(" Arithmetic ")" .

/* A call written on its own, where a host language takes one: which functions
   exist and what their arguments mean are the host's. */

Call         = unquotedLiteral "(" [ CallArgument { "," CallArgument } ] ")" .
CallArgument = literal | number .

literal           = quotedString | unquotedLiteral .
quotedString      = "\"" { quotedCharacter } "\"" .
unquotedLiteral   = unquotedCharacter { unquotedCharacter } .
quotedCharacter   = "\\" any | any .
unquotedCharacter = "\\" specialCharacter | "\\" keyword | "*" | variableReference | any .
variableReference = "$" "{" variableName "}" .
variableName      = letter { letter | digit | "_" } .
specialCharacter  = "\\" | "(" | ")" | ":" | "<" | ">" | "\"" | "*" | "{" | "}" | "|" .
keyword           = "or" | "and" | "not" .
duration          = wholeNumber durationUnit { wholeNumber durationUnit } .
durationUnit      = "ms" | "s" | "m" | "h" | "d" | "w" | "y" .
number            = [ "-" ] wholeNumber [ "." wholeNumber ] .
wholeNumber       = digit { digit } .
digit             = "0" … "9" .
letter            = "a" … "z" | "A" … "Z" | "_" .
any               = "\u0000" … "\U0010FFFF" .
```

<!-- end query grammar -->

An operand in parentheses is a query when it holds a top-level `|`; any other parentheses group the filter. In an expression with an operator, every query operand is in parentheses.

## Filter

| Form | Matches |
|---|---|
| `term` | the term in any field |
| `"a phrase"` | the phrase in any field |
| `field:value` | the value in the field |
| `field:"a phrase"` | the phrase in the field |
| `field:val*` | a value holding `*`, which stands for any run of characters |
| `field:*` | any item carrying the field |
| `field < value`, `<=`, `>`, `>=` | a range on the field |
| `field:(a or b)` | the field applied to each value of the group |
| `field:{ query }` | the query inside the nested field |
| `and`, `or`, `not`, `( )` | the combination; `and` binds tighter than `or` |

- Terms with no operator between them match any of them.
- A backslash takes the character after it as written.
- `${name}` in a value is a variable reference: a backend passes it through as written, for the tool running the query to substitute. It is the one form a variable takes; a bare `$name` is refused, since text following it would read as part of the name.
- A field is the name the backend's items carry: a record's field path, a series' label, a span's attribute. A series' metric name is its label `__name__`.

## Functions

A stage is a call to one function the language defines. Each backend compiles the functions its items support and refuses the rest.

| Function | Computes |
|---|---|
| `dedup(field)` | the first item for each value of the field |
| `eval(field = arithmetic)` | a field computed from other fields |
| `extract(field, "pattern")` | the fields a regular expression's named groups capture from the field |
| `fields(field, ...)` | the items with only the listed fields |
| `rename(field, new)` | the field under a new name |
| `per_second(window)` | the items per second, in windows of the duration |
| `rate(window)` | a counter's per-second increase, over a window of the duration |
| `avg(field)`, `max(field)`, `min(field)`, `sum(field)` | the statistic over the field |
| `count()` | the number of items or series |
| `distinct_count(field)` | the number of distinct values of the field |
| `quantile(q, field)` | the q-quantile of the field |
| `stdev(field)` | the standard deviation of the field |
| `topk(n)`, `bottomk(n)` | the n largest or smallest results |
| `sort(field)`, `sort_desc(field)` | the results in ascending or descending order of the field |
| `limit(n)` | the first n results |
| `where(comparison)` | the results whose comparison holds |

- A statistical function over series omits the field: it computes over the series' values.
- `by` groups `per_second`, the statistical functions, `topk` and `bottomk`: one result per value of the grouping fields; with no grouping, one result.
- In `sort`, `sort_desc` and `where`, `value` stands for a result's value.
- Stages run in this order: the shaping functions, then at most one of `rate` and `per_second`, then the statistical functions, nesting in order, then the result functions. `where` also stands among the statistical functions, keeping the results it compares before the next one aggregates them. A query out of order is refused.

## Arithmetic

Queries whose results are time series combine with `+`, `-`, `*` and `/`, as do a series and a number. Series pair up on the fields both sides group by.

```
(status:5* | per_second(5m) by job) / (status:* | per_second(5m) by job) * 100
```

Where one side groups by more fields, each of its series pairs with the other side's one series, and the result keeps its fields.

```
(status:* | per_second(5m) by job, status) / (status:* | per_second(5m) by job) * 100
```

From grammar `1.1`, `now()` stands in arithmetic for the time each result is computed at, in seconds since the Unix epoch.

```
(__name__:mock_certificate_expiry_seconds | min() by host) - now()
```

## Result

A query whose stages are only shaping and result functions answers the backend's items: records, spans or series. A query with a time or statistical function answers time series, one per value of its last grouping. The parsed query states which, so a caller knows to show a table or a chart.

A query ending in an ungrouped `count()` over a selection that matched nothing answers `0` while its source reports, and no result while it does not. Whether a source reports is the backend's configuration, never the query's:

| Backend | A source reports while |
|---|---|
| a series backend | the series its reporting filter selects exist |
| a record backend | a search over its records runs |

## Backends

A backend is a compiler from the parsed query to one native language. Each takes a configuration stating what the compiler cannot read from the query: where the items live and what kind each field is in that backend. A query a backend cannot compile is refused with the form it cannot compile and why.

| Backend | Configuration | Filter |
|---|---|---|
| LogQL (Log Query Language, Grafana Loki) | the stream labels | stream selector for stream labels, label filters for the rest, line filters for terms |
| OpenSearch query DSL | the time field, the group limit | a query clause |
| PPL (Piped Processing Language, OpenSearch) | the source, its time field | `where` |
| PromQL (Prometheus Query Language) | the reporting filter, optional | label matchers |
| SPL (Search Processing Language, Splunk) | the index | the search |
| SQL | the dialect, the table, its time column | `WHERE` |
| TraceQL (Trace Query Language, Grafana Tempo) | each field's attribute scope | a spanset filter |

### Support

| Function | LogQL | OpenSearch query DSL | PPL | PromQL | SPL | SQL | TraceQL |
|---|---|---|---|---|---|---|---|
| `dedup` | refused | `collapse` | `dedup` | refused | `dedup` | per dialect | refused |
| `eval` | `label_format` | refused | `eval` | refused | `eval` | a computed column | refused |
| `extract` | `regexp` | refused | `parse` | refused | `rex` | per dialect | refused |
| `fields` | `keep` | `_source` | `fields` | refused | `fields` | `SELECT` | `select` |
| `rename` | `label_format` | refused | `rename` | `label_replace` | `rename` | `AS` | refused |
| `per_second` | `rate` | `date_histogram`, each count over the window's seconds | `stats count() by span(...)`, over the window's seconds | refused | `timechart count`, over the window's seconds | `count()` per time bucket, over the window's seconds | `rate()` |
| `rate` | `rate_counter` | refused | refused | `rate` | `rate` | refused | refused |
| `avg`, `max`, `min`, `sum` | `<function>_over_time` with `unwrap` | the same-named aggregation | the same-named function | the same-named operator | the same-named function | the same-named function | `<function>_over_time()` |
| `count` | `count_over_time` | each bucket's `doc_count` | `count()` | `count` | `count` | `count()` | `count_over_time()` |
| `distinct_count` | `count(count by (...))` | `cardinality` | `distinct_count()` | `count(count by (...))` | `dc` | `count(DISTINCT ...)` | refused |
| `quantile` | `quantile_over_time` with `unwrap` | `percentiles` | `percentile()` | `histogram_quantile` or `quantile` | `perc<X>` | per dialect | `quantile_over_time()` |
| `stdev` | `stddev_over_time` with `unwrap` | `extended_stats` | `stddev_samp()` | `stddev` | `stdev` | per dialect | refused |
| `topk`, `bottomk` | `topk`, `bottomk` | `terms`, ordered, of size n | `sort` and `head` | `topk`, `bottomk` | `sort` and `head` | `ORDER BY` and `LIMIT` | `topk`, `bottomk` |
| `sort`, `sort_desc` | refused | `sort` | `sort` | `sort`, `sort_desc` over values | `sort` | `ORDER BY` | refused |
| `limit` | refused | `size` | `head` | refused | `head` | `LIMIT` | refused |
| `where` | a comparison operator | `bucket_selector` | `where` | a comparison operator | `where` | `HAVING` | refused |

### PromQL

The filter compiles to series selectors.

| DQL | Label matcher |
|---|---|
| `job:a` | `job="a"` |
| `job:"a b"` | `job="a b"` |
| `job:a*` | `job=~"a.*"` |
| `job:${job}` | `job=~"${job}"` |
| `job:*` | `job=~".+"` |
| `not job:a` | `job!="a"` |
| `not job:a*` | `job!~"a.*"` |
| `not job:*` | `job=""` |
| `job:(a or b)` | `job=~"a\|b"` |

- A value is copied as written: `"` and `\` are escaped, and in a wildcard value every regular-expression character but `*` is escaped. A variable reference such as `${job}` is written unescaped, so the dashboard tool substitutes it, and a value holding one compiles to a regular-expression matcher, `job=~"${job}"`, which also matches when the tool substitutes several chosen values as an alternation.
- A filter with `or` across fields is rewritten into a disjunction of conjunctions. Each conjunction is one selector, and the selectors are joined with PromQL's `or`.
- `rate` applies to each selector; statistical and result functions apply to the joined result.
- `quantile` over a classic histogram's bucket series, `__name__:<histogram>_bucket`, compiles to `histogram_quantile` with `le` added to the grouping; over other series, to `quantile`.
- With a reporting filter, a query ending in an ungrouped `count()` compiles to `count(<selection>) or on() (count(<reporting>) * 0)`. The reporting filter is a filter alone, with no stages or arithmetic; without one, the count of nothing is no result.
- `now()` compiles to `time()`.
- Each selector needs a matcher that cannot match the empty string, as PromQL requires; `__name__` is one. A blank filter is refused for the same reason.
- A filter expands into at most 64 selectors.
- A term with no field, a range and a nested query are refused: a series carries flat labels, not text, and a label matcher has no range.

```
__name__:mock_requests_total and (job:mock-api or env:mock) | rate(5m) | sum() by job
```

compiles to

```
sum by (job) (rate(mock_requests_total{job="mock-api"}[5m]) or rate(mock_requests_total{env="mock"}[5m]))
```

Arithmetic between queries pairs the series either side of an operator by what each side groups by.

| The sides group by | PromQL pairs them with |
|---|---|
| the same fields | `on (<fields>)` |
| more fields on the left | `on (<shared fields>) group_left ()` |
| more fields on the right | `on (<shared fields>) group_right ()` |
| a field each that the other lacks | refused: PromQL pairs many series with one |

### OpenSearch query DSL

The filter compiles to a query clause, as OpenSearch Dashboards compiles it.

| DQL | Clause |
|---|---|
| `term` | `multi_match` over every field |
| `"a phrase"` | `multi_match` of the phrase over every field |
| `field:value` | `match` on the field |
| `field:"a phrase"` | `match_phrase` on the field |
| a value holding `*` | `query_string` |
| `field:*` | `exists` |
| `field < value`, `<=`, `>`, `>=` | `range` |
| `field:{ query }` | `nested` |
| `and`, `or`, `not`, `( )` | `bool` |

A blank filter matches every record.

- A query answering items is a search: shaping and result functions set its `collapse`, `_source`, `sort` and `size`.
- A query answering time series is an aggregation over one time or statistic function. `per_second` buckets with `date_histogram` on the configured time field; `by` nests a `terms` aggregation per grouping field, of the configured group limit.
- `topk`, `bottomk` and `where` apply to the buckets of the function before them: `topk` and `bottomk` order and size its innermost `terms`, and `where` is a `bucket_selector` in it.
- Arithmetic is refused: a search request answers one query.

## Calls

A call is written on its own where a host language takes one, such as a dashboard variable's choices, in the form of a stage: the function's name, then its arguments in parentheses, separated by commas. An argument is a field name, a number or a quoted string. The parser reads the form; which functions exist and what their arguments mean are the host's.

```
field_values(job, "__name__:mock_up and env:${env}")
```

## Go API

| Package | Entry point | Answers |
|---|---|---|
| `dqlsyntax` | `ParseQuery(text, grammarVersion string) (ParsedQuery, error)` | the query's syntax tree; its `ResultKind()` is `ItemsResult` or `TimeSeriesResult` |
| `dqlsyntax` | `ParseCall(text, grammarVersion string) (Call, error)` | the call's function, lowercased, and its arguments, each a field, a number or a quoted string |
| `promqlbackend` | `CompileQuery(query ParsedQuery) (string, error)` | the PromQL expression |
| `opensearchbackend` | `CompileQuery(query ParsedQuery, config BackendConfig) (map[string]any, error)` | the search request body |

A backend package that needs configuration declares its own `BackendConfig`. Parsing is shared, so every backend reads one grammar and refuses a malformed query with the same message.

## Errors

An error states what was wrong in the query's own terms:

- `unexpected <token>`
- `the query ends early`
- `a parenthesis is not closed`
- `a quote is not closed`
- `a field needs a value, not <token>`
- `a range on <field> needs a value`
- `a brace is not closed`
- `write a variable as ${<name>}, not $<name>`
- `a variable is written ${name}, its name letters, digits and underscores`
- `unknown function "<name>"`
- `"<function>" cannot follow "<function>"`
- `"<function>" needs <argument>, not <token>`
- `a call needs a function, not <token>`
- `arithmetic combines queries that answer time series`
- `grammar version <version> is not supported; supported: <versions>`

A refusal names the version, the function or the form it cannot compile, and why.

## Open questions

- A grouped count that is zero for a group with nothing. A group whose selection matched nothing is absent in both backends, so a grouped count answers only the groups that matched. Its zero needs the groups that should exist, stated with the source's reporting. Undecided: where those groups are stated, and how each backend compiles them.
