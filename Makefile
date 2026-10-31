VENV = /tmp/dql-compiler-venv

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
	go test ./...
	@UV_PROJECT_ENVIRONMENT=$(VENV) uv run --locked --group test pytest -q
	@python3 tools/sync_query_grammar.py --check
