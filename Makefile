VENV = /tmp/dql-compiler-venv

.PHONY: grammar requirements test

grammar:
	@python3 tools/sync_query_grammar.py

requirements:
	@uv lock
	@uv export --format requirements.txt --only-group test --no-emit-project --frozen --no-header \
		-o tools/requirements.txt

test:
	@UV_PROJECT_ENVIRONMENT=$(VENV) uv run --locked --group test pytest -q
