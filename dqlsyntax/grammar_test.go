package dqlsyntax

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/exp/ebnf"
	"gopkg.in/yaml.v3"
)

const grammarStart = "Expression"

type refusedQuery struct {
	Written string `yaml:"written"`
	Says    string `yaml:"says"`
}

type queryCorpus struct {
	Admitted map[string][]string `yaml:"admitted"`
	Refused  []refusedQuery      `yaml:"refused"`
}

type grammarVersionFixture struct {
	version string
	grammar ebnf.Grammar
	corpus  queryCorpus
}

// grammarVersionFixtures reads every grammar file and the corpus written for it.
func grammarVersionFixtures(t *testing.T) []grammarVersionFixture {
	t.Helper()
	grammarFiles, err := filepath.Glob("grammar/*.ebnf")
	if err != nil || len(grammarFiles) == 0 {
		t.Fatalf("grammar/ holds no grammar file: %v", err)
	}
	var fixtures []grammarVersionFixture
	for _, grammarFile := range grammarFiles {
		version := strings.TrimSuffix(filepath.Base(grammarFile), ".ebnf")
		fixtures = append(fixtures, grammarVersionFixture{
			version: version,
			grammar: readGrammar(t, grammarFile),
			corpus:  readCorpus(t, filepath.Join("testdata", "queries-"+version+".yml")),
		})
	}
	return fixtures
}

func readGrammar(t *testing.T, grammarFile string) ebnf.Grammar {
	t.Helper()
	file, err := os.Open(grammarFile)
	if err != nil {
		t.Fatalf("open %s: %v", grammarFile, err)
	}
	defer file.Close()
	grammar, err := ebnf.Parse(grammarFile, file)
	if err != nil {
		t.Fatalf("%s is not a grammar: %v", grammarFile, err)
	}
	if err := ebnf.Verify(grammar, grammarStart); err != nil {
		t.Fatalf("%s does not verify from %s: %v", grammarFile, grammarStart, err)
	}
	return grammar
}

func readCorpus(t *testing.T, corpusFile string) queryCorpus {
	t.Helper()
	raw, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatalf("open %s: %v", corpusFile, err)
	}
	var corpus queryCorpus
	if err := yaml.Unmarshal(raw, &corpus); err != nil {
		t.Fatalf("%s: %v", corpusFile, err)
	}
	if len(corpus.Admitted) == 0 || len(corpus.Refused) == 0 {
		t.Fatalf("%s holds nothing to read", corpusFile)
	}
	return corpus
}

// grammarMatcher answers whether a grammar admits the tokens the parser took, carrying every position an expression can reach.
type grammarMatcher struct {
	grammar ebnf.Grammar
	tokens  []grammarToken
}

var lexicalTokenKinds = map[string][]grammarTokenKind{
	"literal":         {grammarUnquotedToken, grammarQuotedToken},
	"quotedString":    {grammarQuotedToken},
	"unquotedLiteral": {grammarUnquotedToken},
	"number":          {grammarNumberToken},
	"wholeNumber":     {grammarNumberToken},
	"duration":        {grammarDurationToken},
}

func (matcher grammarMatcher) admits() bool {
	return slices.Contains(matcher.reaches(matcher.grammar[grammarStart].Expr, 0), len(matcher.tokens))
}

func (matcher grammarMatcher) reaches(expression ebnf.Expression, at int) []int {
	switch node := expression.(type) {
	case nil:
		return []int{at}
	case ebnf.Alternative:
		var reached []int
		for _, alternative := range node {
			reached = append(reached, matcher.reaches(alternative, at)...)
		}
		return sortedPositions(reached)
	case ebnf.Sequence:
		positions := []int{at}
		for _, element := range node {
			var next []int
			for _, position := range positions {
				next = append(next, matcher.reaches(element, position)...)
			}
			if positions = sortedPositions(next); len(positions) == 0 {
				return nil
			}
		}
		return positions
	case *ebnf.Group:
		return matcher.reaches(node.Body, at)
	case *ebnf.Option:
		return sortedPositions(append([]int{at}, matcher.reaches(node.Body, at)...))
	case *ebnf.Repetition:
		return matcher.repeats(node.Body, at)
	case *ebnf.Name:
		return matcher.named(node.String, at)
	case *ebnf.Token:
		return matcher.token(node.String, at)
	}
	panic(fmt.Sprintf("%T is a form this matcher does not read", expression))
}

func (matcher grammarMatcher) repeats(body ebnf.Expression, at int) []int {
	reached, frontier := map[int]bool{at: true}, []int{at}
	for len(frontier) > 0 {
		var next []int
		for _, position := range frontier {
			for _, reachedPosition := range matcher.reaches(body, position) {
				if !reached[reachedPosition] {
					reached[reachedPosition], next = true, append(next, reachedPosition)
				}
			}
		}
		frontier = next
	}
	return sortedPositions(slices.Collect(maps.Keys(reached)))
}

func (matcher grammarMatcher) named(name string, at int) []int {
	if isLexical(name) {
		if at >= len(matcher.tokens) || !slices.Contains(lexicalTokenKinds[name], matcher.tokens[at].kind) {
			return nil
		}
		if name == "wholeNumber" && strings.Contains(matcher.tokens[at].text, ".") {
			return nil
		}
		return []int{at + 1}
	}
	production, declared := matcher.grammar[name]
	if !declared {
		panic(fmt.Sprintf("%s is named and never declared", name))
	}
	return matcher.reaches(production.Expr, at)
}

func (matcher grammarMatcher) token(text string, at int) []int {
	if at >= len(matcher.tokens) {
		return nil
	}
	taken := matcher.tokens[at]
	if (taken.kind == grammarWordToken || taken.kind == grammarSymbolToken) && taken.text == text {
		return []int{at + 1}
	}
	return nil
}

func isLexical(name string) bool { return unicode.IsLower(rune(name[0])) }

func sortedPositions(positions []int) []int {
	slices.Sort(positions)
	return slices.Compact(positions)
}

func TestEveryAdmittedQueryIsASentenceOfTheGrammarTheParserReads(t *testing.T) {
	for _, fixture := range grammarVersionFixtures(t) {
		for group, queries := range fixture.corpus.Admitted {
			for _, query := range queries {
				_, tokens, err := parseRecordingTokens(query, fixture.version)
				if err != nil {
					t.Errorf("%s %s: %q: %v", fixture.version, group, query, err)
					continue
				}
				if !(grammarMatcher{grammar: fixture.grammar, tokens: tokens}).admits() {
					t.Errorf("%s %s: %q is no sentence of grammar %s", fixture.version, group, query, fixture.version)
				}
			}
		}
	}
}

func TestEveryRefusedQueryIsRefusedInItsOwnWords(t *testing.T) {
	for _, fixture := range grammarVersionFixtures(t) {
		for _, refused := range fixture.corpus.Refused {
			_, err := ParseQuery(refused.Written, fixture.version)
			if err == nil || err.Error() != refused.Says {
				t.Errorf("%s: %q: err = %v, want %q", fixture.version, refused.Written, err, refused.Says)
			}
		}
	}
}

// A form the grammar declares and no admitted query writes is a form nothing proves.
func TestTheGrammarHasNoFormNoAdmittedQueryWrites(t *testing.T) {
	for _, fixture := range grammarVersionFixtures(t) {
		written := map[string]bool{}
		kinds := map[grammarTokenKind]bool{}
		for _, queries := range fixture.corpus.Admitted {
			for _, query := range queries {
				_, tokens, _ := parseRecordingTokens(query, fixture.version)
				for _, token := range tokens {
					written[token.text], kinds[token.kind] = true, true
				}
			}
		}
		for _, name := range slices.Sorted(maps.Keys(fixture.grammar)) {
			if lexicalKinds, answers := lexicalTokenKinds[name]; answers {
				if !slices.ContainsFunc(lexicalKinds, func(kind grammarTokenKind) bool { return kinds[kind] }) {
					t.Errorf("%s: no admitted query writes a %s", fixture.version, name)
				}
				continue
			}
			if isLexical(name) {
				continue
			}
			for _, text := range tokensIn(fixture.grammar[name].Expr) {
				if !written[text] {
					t.Errorf("%s: no admitted query writes %q, which %s declares", fixture.version, text, name)
				}
			}
		}
	}
}

var categoryProductions = map[string]FunctionCategory{
	"Shaping":   ShapingCategory,
	"Time":      TimeCategory,
	"Statistic": StatisticCategory,
	"Result":    ResultCategory,
}

// The grammar and the parser hold one function vocabulary, each function in one category.
func TestTheGrammarAndTheParserHoldTheSameFunctions(t *testing.T) {
	for _, fixture := range grammarVersionFixtures(t) {
		declared := map[string]FunctionCategory{}
		for production, category := range categoryProductions {
			for _, alternative := range fixture.grammar[production].Expr.(ebnf.Alternative) {
				name := alternative.(*ebnf.Name).String
				functionName := fixture.grammar[name].Expr.(ebnf.Sequence)[0].(*ebnf.Token).String
				declared[functionName] = category
			}
		}
		held := map[string]FunctionCategory{}
		for function, spec := range Functions {
			held[function] = spec.Category
		}
		if !maps.Equal(declared, held) {
			t.Errorf("grammar %s declares %v and the parser holds %v", fixture.version, declared, held)
		}
	}
}

func tokensIn(expression ebnf.Expression) []string {
	switch node := expression.(type) {
	case ebnf.Alternative:
		var held []string
		for _, alternative := range node {
			held = append(held, tokensIn(alternative)...)
		}
		return held
	case ebnf.Sequence:
		var held []string
		for _, element := range node {
			held = append(held, tokensIn(element)...)
		}
		return held
	case *ebnf.Group:
		return tokensIn(node.Body)
	case *ebnf.Option:
		return tokensIn(node.Body)
	case *ebnf.Repetition:
		return tokensIn(node.Body)
	case *ebnf.Token:
		return []string{node.String}
	}
	return nil
}
