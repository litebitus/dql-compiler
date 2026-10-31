from pathlib import Path

import pytest

import sync_query_grammar as sync


def document(between):
    return (
        "# DQL compiler\n\n## Grammar\n\n"
        f"{sync.OPENS}\n\n{between}\n\n{sync.CLOSES}\n\n## Filter\n"
    )


def test_the_grammar_is_written_between_the_markers():
    written = sync.written(document("```ebnf\nOld = \"old\" .\n```"), "1.1", 'New = "new" .\n')
    assert "New = \"new\" ." in written
    assert "Version `1.1`." in written
    assert "Old" not in written
    assert written.startswith("# DQL compiler\n\n## Grammar\n\n")
    assert written.endswith("## Filter\n")
    assert written.count(sync.OPENS) == 1
    assert written.count(sync.CLOSES) == 1


def test_writing_twice_writes_the_same_document():
    once = sync.written(document("```ebnf\nOld = \"old\" .\n```"), "1.0", 'New = "new" .')
    assert sync.written(once, "1.0", 'New = "new" .') == once


def test_a_document_missing_its_markers_is_refused():
    with pytest.raises(SystemExit):
        sync.written("# DQL compiler\n\nno markers here\n", "1.0", 'New = "new" .')


def test_markers_the_wrong_way_round_are_refused():
    inverted = f"# DQL compiler\n\n{sync.CLOSES}\n\n{sync.OPENS}\n"
    with pytest.raises(SystemExit):
        sync.written(inverted, "1.0", 'New = "new" .')


def test_the_latest_version_is_read_by_number():
    grammar_files = [Path("dqlsyntax/grammar/1.9.ebnf"), Path("dqlsyntax/grammar/1.10.ebnf")]
    assert sync.latest_grammar(grammar_files).name == "1.10.ebnf"


def test_a_major_outranks_every_minor():
    grammar_files = [Path("dqlsyntax/grammar/1.12.ebnf"), Path("dqlsyntax/grammar/2.0.ebnf")]
    assert sync.latest_grammar(grammar_files).name == "2.0.ebnf"


def test_a_misnamed_grammar_file_is_refused():
    with pytest.raises(SystemExit):
        sync.latest_grammar([Path("dqlsyntax/grammar/latest.ebnf")])


def test_no_grammar_file_is_refused():
    with pytest.raises(SystemExit):
        sync.latest_grammar([])


@pytest.fixture
def stale_document(tmp_path, monkeypatch):
    grammar_directory = tmp_path / "grammar"
    grammar_directory.mkdir()
    (grammar_directory / "1.0.ebnf").write_text('New = "new" .\n')
    design = tmp_path / "design.md"
    design.write_text(document("```ebnf\nOld = \"old\" .\n```"))
    monkeypatch.setattr(sync, "GRAMMAR_DIRECTORY", grammar_directory)
    monkeypatch.setattr(sync, "DOCUMENT", design)
    return design


def test_a_check_fails_on_a_stale_document_and_leaves_it_alone(stale_document):
    before = stale_document.read_text()
    assert sync.main(["--check"]) == 1
    assert stale_document.read_text() == before


@pytest.mark.usefixtures("stale_document")
def test_a_check_passes_once_the_grammar_is_written():
    assert sync.main([]) == 0
    assert sync.main(["--check"]) == 0


@pytest.mark.usefixtures("stale_document")
def test_an_unknown_argument_is_refused():
    with pytest.raises(SystemExit):
        sync.main(["--write"])
