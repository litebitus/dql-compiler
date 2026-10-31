package dqlsyntax

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type tokenKind int

const (
	endToken tokenKind = iota
	symbolToken
	wordToken
	quotedToken
	numberToken
	durationToken
)

type queryToken struct {
	kind      tokenKind
	text      string
	writtenAs string
	parts     []string
	escaped   bool
	end       int
}

func (token queryToken) isSymbol(symbol string) bool {
	return token.kind == symbolToken && token.text == symbol
}

func (token queryToken) isKeyword(keyword string) bool {
	return token.kind == wordToken && !token.escaped && strings.EqualFold(token.text, keyword)
}

func (token queryToken) isAnyFilterKeyword() bool {
	return token.isKeyword("and") || token.isKeyword("or") || token.isKeyword("not")
}

func (token queryToken) isLiteral() bool {
	return token.kind == quotedToken || token.kind == wordToken
}

func describeToken(token queryToken) string {
	switch token.kind {
	case endToken:
		return "end of query"
	case wordToken:
		return fmt.Sprintf("%q", token.text)
	}
	return fmt.Sprintf("%q", token.writtenAs)
}

const (
	filterWordBreaks = `:()<>"{}|`
	stageWordBreaks  = `(),=<>!|+-*/"`
)

var (
	errQuoteNotClosed = errors.New("a quote is not closed")
	numberPattern     = regexp.MustCompile(`^\d+(\.\d+)?$`)
	durationPattern   = regexp.MustCompile(`^(\d+(ms|s|m|h|d|w|y))+$`)
)

type queryReader struct {
	runes []rune
}

func (reader queryReader) spacesEnd(at int) int {
	for at < len(reader.runes) && unicode.IsSpace(reader.runes[at]) {
		at++
	}
	return at
}

func (reader queryReader) filterTokenAt(at int) (queryToken, error) {
	at = reader.spacesEnd(at)
	if at >= len(reader.runes) {
		return queryToken{kind: endToken, end: at}, nil
	}
	switch character := reader.runes[at]; {
	case strings.ContainsRune(`:(){}|`, character):
		return symbolAt(string(character), at), nil
	case character == '<' || character == '>':
		return reader.comparisonAt(at), nil
	case character == '"':
		return reader.quotedAt(at)
	}
	return reader.wordAt(at, filterWordBreaks, true), nil
}

func (reader queryReader) stageTokenAt(at int) (queryToken, error) {
	at = reader.spacesEnd(at)
	if at >= len(reader.runes) {
		return queryToken{kind: endToken, end: at}, nil
	}
	switch character := reader.runes[at]; {
	case strings.ContainsRune(`(),|+-*/`, character):
		return symbolAt(string(character), at), nil
	case strings.ContainsRune(`<>=!`, character):
		return reader.comparisonAt(at), nil
	case character == '"':
		return reader.quotedAt(at)
	}
	word := reader.wordAt(at, stageWordBreaks, false)
	switch {
	case word.escaped:
	case numberPattern.MatchString(word.text):
		word.kind = numberToken
	case durationPattern.MatchString(word.text):
		word.kind = durationToken
	}
	return word, nil
}

func symbolAt(symbol string, at int) queryToken {
	return queryToken{kind: symbolToken, text: symbol, writtenAs: symbol, end: at + len(symbol)}
}

func (reader queryReader) comparisonAt(at int) queryToken {
	symbol := string(reader.runes[at])
	if at+1 < len(reader.runes) && reader.runes[at+1] == '=' {
		symbol += "="
	}
	return symbolAt(symbol, at)
}

func (reader queryReader) quotedAt(quoteStart int) (queryToken, error) {
	var text strings.Builder
	for at := quoteStart + 1; at < len(reader.runes); at++ {
		switch character := reader.runes[at]; {
		case character == '\\' && at+1 < len(reader.runes):
			at++
			text.WriteRune(reader.runes[at])
		case character == '"':
			return queryToken{
				kind:      quotedToken,
				text:      text.String(),
				writtenAs: string(reader.runes[quoteStart : at+1]),
				parts:     []string{text.String()},
				end:       at + 1,
			}, nil
		default:
			text.WriteRune(character)
		}
	}
	return queryToken{}, errQuoteNotClosed
}

func (reader queryReader) wordAt(wordStart int, wordBreaks string, wildcardsSplit bool) queryToken {
	var part strings.Builder
	var parts []string
	escaped := false
	at := wordStart
	for ; at < len(reader.runes); at++ {
		character := reader.runes[at]
		if unicode.IsSpace(character) || strings.ContainsRune(wordBreaks, character) {
			break
		}
		if character == '*' && wildcardsSplit {
			parts = append(parts, part.String())
			part.Reset()
			continue
		}
		if character == '\\' && at+1 < len(reader.runes) {
			at++
			escaped = true
			character = reader.runes[at]
		}
		part.WriteRune(character)
	}
	parts = append(parts, part.String())
	return queryToken{
		kind:      wordToken,
		text:      strings.Join(parts, "*"),
		writtenAs: string(reader.runes[wordStart:at]),
		parts:     parts,
		escaped:   escaped,
		end:       at,
	}
}

// holdsTopLevelPipe reports whether the parentheses opening at an operand's start hold a `|` of their own.
func (reader queryReader) holdsTopLevelPipe(openAt int) bool {
	depth := 0
	for at := openAt; at < len(reader.runes); at++ {
		switch reader.runes[at] {
		case '\\':
			at++
		case '"':
			if quoted, err := reader.quotedAt(at); err == nil {
				at = quoted.end - 1
			}
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return false
			}
		case '|':
			if depth == 1 {
				return true
			}
		}
	}
	return false
}

// numberOperandAt reports whether an operand's start is a number an arithmetic operator follows.
func (reader queryReader) numberOperandAt(at int) bool {
	token, err := reader.stageTokenAt(at)
	if err != nil || token.kind != numberToken {
		return false
	}
	following, err := reader.stageTokenAt(token.end)
	return err == nil && following.kind == symbolToken && strings.Contains("+-*/", following.text)
}
