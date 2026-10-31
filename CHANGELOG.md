# Changelog

## Unreleased

- Grammar
  - Grammar 1.0: DQL extended with pipe stages and arithmetic between queries.
- Library
  - `dqlsyntax`: parses a query under a grammar version into its syntax tree and result kind.
  - `promqlbackend`: compiles a query into a PromQL expression.
  - `opensearchbackend`: compiles a query into an OpenSearch search request body.
- Development
  - `make build`, which tests first, and `make test`; CI runs the tests on every push and pull request.
  - A dispatched release workflow stamps the changelog, tags the version and publishes the GitHub release.
