VENV = /tmp/dql-compiler-venv
COVERAGE_PROFILE = /tmp/dql-compiler-coverage.out
COVERAGE_MINIMUM = 95

.PHONY: build grammar requirements test

build: test
	go build ./...

grammar:
	@python3 tools/sync_query_grammar.py

requirements:
	@uv lock
	@uv export --format requirements.txt --only-group test --no-emit-project --frozen --no-header \
		-o tools/requirements.txt

test:
	go vet ./...
	@unformatted="$$(gofmt -l .)"; [ -z "$$unformatted" ] || { echo "not gofmt-formatted:"; echo "$$unformatted"; exit 1; }
	go test -count=1 -coverpkg=./... -coverprofile=$(COVERAGE_PROFILE) ./...
	@go tool cover -func=$(COVERAGE_PROFILE) | awk -v minimum=$(COVERAGE_MINIMUM) '/^total:/ { \
		sub("%", "", $$3); \
		if ($$3 + 0 < minimum) { printf "\033[31mcoverage %s%%, below %s%%\033[0m\n", $$3, minimum; exit 1 } \
		printf "coverage %s%%, at least %s%%\n", $$3, minimum }'
	@UV_PROJECT_ENVIRONMENT=$(VENV) uv run --locked --group test pytest -q
	@python3 tools/sync_query_grammar.py --check
