package dqlsyntax

import (
	"strings"
	"testing"
)

func TestAValuesTextRejoinsItsPartsAtTheirWildcards(t *testing.T) {
	for _, value := range []Value{
		{Parts: []string{"mock-api"}},
		{Parts: []string{"mock-", "-api"}},
		{Parts: []string{"", ""}},
	} {
		want := strings.Join(value.Parts, "*")
		if got := value.Text(); got != want {
			t.Errorf("parts %q: text %q, want %q", value.Parts, got, want)
		}
	}
}

func TestEscapingAroundVariablesLeavesEachReferenceAsWritten(t *testing.T) {
	bracketed := func(text string) string { return "[" + text + "]" }
	for text, want := range map[string]string{
		"mock-api":             "[mock-api]",
		"${env}":               "[]${env}[]",
		"mock-${env}-${job}.*": "[mock-]${env}[-]${job}[.*]",
		"$env and ${1bad}":     "[$env and ${1bad}]",
	} {
		if got := EscapeAroundVariables(text, bracketed); got != want {
			t.Errorf("%q: escaped %q, want %q", text, got, want)
		}
	}
}
