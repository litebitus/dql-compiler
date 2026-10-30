"""Write the latest query grammar into docs/design.md from where it is kept.

dqlsyntax/grammar/<major>.<minor>.ebnf is the spec the parser answers to, one
file per grammar version. The design document carries the latest version only,
so the block between its markers is this script's to write and nobody's to edit.

Run:
    make grammar
"""

import re
import sys
from pathlib import Path

GRAMMAR_DIRECTORY = Path("dqlsyntax/grammar")
DOCUMENT = Path("docs/design.md")
OPENS = "<!-- query grammar: written by `make grammar` from dqlsyntax/grammar; edit it there -->"
CLOSES = "<!-- end query grammar -->"
GRAMMAR_FILE_NAME = re.compile(r"^(\d+)\.(\d+)\.ebnf$")


def latest_grammar(grammar_files):
    """The grammar file of the highest version, by major then minor."""
    versioned = []
    for grammar_file in grammar_files:
        version = GRAMMAR_FILE_NAME.match(grammar_file.name)
        if version is None:
            raise SystemExit(f"{grammar_file}: a grammar file is named <major>.<minor>.ebnf")
        versioned.append(((int(version[1]), int(version[2])), grammar_file))
    if not versioned:
        raise SystemExit(f"{GRAMMAR_DIRECTORY}: holds no grammar file")
    return max(versioned)[1]


def written(document, grammar_version, grammar):
    """The document with the grammar between its markers.

    The markers have to be there and in that order: a document that lost them is
    a document this script would otherwise append to, or silently leave alone.
    """
    opens, closes = document.find(OPENS), document.find(CLOSES)
    if opens < 0 or closes < 0:
        raise SystemExit(f"{DOCUMENT}: the grammar markers are gone, so there is nowhere to write")
    if closes < opens:
        raise SystemExit(f"{DOCUMENT}: the grammar's markers are the wrong way round")
    block = f"{OPENS}\n\nVersion `{grammar_version}`.\n\n```ebnf\n{grammar.strip()}\n```\n\n"
    return document[:opens] + block + document[closes:]


def main():
    grammar_file = latest_grammar(sorted(GRAMMAR_DIRECTORY.glob("*.ebnf")))
    grammar_version = grammar_file.name.removesuffix(".ebnf")
    document = DOCUMENT.read_text()
    with_grammar = written(document, grammar_version, grammar_file.read_text())
    if with_grammar == document:
        print(f"{DOCUMENT}: the grammar is {grammar_file}")
        return 0
    DOCUMENT.write_text(with_grammar)
    print(f"{DOCUMENT}: written from {grammar_file}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
