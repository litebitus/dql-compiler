# DQL Compiler

One query language for every search backend: DQL, the query language of OpenSearch Dashboards, extended with pipe stages, compiled into each backend's native query.

## Features

- A filter is DQL as OpenSearch Dashboards writes it; every DQL query is a query of the language.
- Pipe stages compute over what the filter selects: shaping, time, statistic and result functions from one standardized vocabulary.
- Arithmetic combines queries that answer time series.
- A versioned grammar: the caller states the version a query is written in, and the compiler keeps compiling every major it has shipped.
- Backends: PromQL and the OpenSearch query DSL. A form a backend has no equivalent for is refused with the reason.

The language, its grammar and each backend's rules are in [docs/design.md](docs/design.md).

## Usage

```
go get github.com/litebitus/dql-compiler
```

```go
parsed, err := dqlsyntax.ParseQuery(`__name__:mock_requests_total and env:mock | rate(5m) | sum() by job`, "1.0")
if err != nil {
	return err
}
expression, err := promqlbackend.CompileQuery(parsed, promqlbackend.BackendConfig{})
// sum by (job) (rate(mock_requests_total{env="mock"}[5m]))
```

Terraform configurations call the compiler through [terraform-provider-dql](https://github.com/litebitus/terraform-provider-dql).

## Development

Go and [uv](https://docs.astral.sh/uv/) build and test the repo.

| Target | Does |
|---|---|
| `make build` | runs the tests, then builds every package |
| `make test` | `go vet`, a `gofmt` check, the Go tests, the Python tests, and a check that the design doc carries the latest grammar |
| `make grammar` | writes the latest grammar into the design doc |
| `make requirements` | locks the Python dependencies and exports `tools/requirements.txt` |

The grammar is `dqlsyntax/grammar/<major>.<minor>.ebnf`, one file per version; `dqlsyntax/testdata/queries-<major>.<minor>.yml` holds the queries it admits and refuses.

### Releases

The `Release` workflow, dispatched by hand, runs the tests, computes the next version, stamps `CHANGELOG.md`, tags it and publishes the GitHub release.
