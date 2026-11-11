# DQL grammar changelog

Each version is the grammar a caller states a query is written in, and the grammar file of that name. A version not yet released is headed `## <version> — unreleased`; the release workflow stamps it with the compiler release that ships it and the date.

## 1.1 — unreleased

- `now()` in arithmetic, the time each result is computed at.

## 1.0 — compiler v0.1.0, 2026-11-06

- The first grammar: DQL extended with pipe stages and arithmetic between queries.
