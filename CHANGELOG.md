# Changelog

## Unreleased

## v0.1.3 — 2026-11-12

- Library
  - `dqlsyntax`: a parsed query's `QueryResultFields()` answers the fields each series of its result carries, where known.

## v0.1.2 — 2026-11-12

- Library
  - `promqlbackend`: arithmetic with a side grouping by more fields pairs each of its series with the other side's one, and the result keeps its fields.
- Development
  - `examples/`: queries and what each compiles to, compiled by the tests.

## v0.1.1 — 2026-11-11

- Grammar
  - Grammar 1.1: `now()` in arithmetic, the time each result is computed at.
- Library
  - `dqlsyntax`: a grammar version stated as a major alone floats to that major's newest minor.
  - `promqlbackend`: compiles `now()` to `time()`.
  - `promqlbackend`: `CompileQuery` takes a `BackendConfig`, whose `ReportingFilter` makes an ungrouped count of nothing answer `0` while the source reports.

## v0.1.0 — 2026-11-06

- Grammar
  - Grammar 1.0: DQL extended with pipe stages and arithmetic between queries.
- Library
  - `dqlsyntax`: parses a query under a grammar version into its syntax tree and result kind.
  - `dqlsyntax`: `ParseCall` parses a call written on its own, in a stage's form, for a host language's own functions.
  - `promqlbackend`: compiles a query into a PromQL expression.
  - `opensearchbackend`: compiles a query into an OpenSearch search request body.
- Development
  - `make build`, which tests first, and `make test`; CI runs the tests on every push and pull request.
  - A dispatched release workflow stamps the changelog, tags the version and publishes the GitHub release.
