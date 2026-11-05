package dqlsyntax

import (
	"errors"
	"fmt"
	"strings"
)

// Call is a function call written on its own where a host language takes one: its function and its arguments as written.
type Call struct {
	Function  string
	Arguments []CallArgument
}

// CallArgumentKind is what an argument of a call is written as.
type CallArgumentKind int

const (
	// FieldCallArgument is a field's name, written unquoted.
	FieldCallArgument CallArgumentKind = iota
	// NumberCallArgument is a number.
	NumberCallArgument
	// QuotedCallArgument is a quoted string, its quotes and escapes read.
	QuotedCallArgument
)

// CallArgument is one argument of a call.
type CallArgument struct {
	Kind CallArgumentKind
	Text string
}

// ParseCall reads text as a call written in grammarVersion. The call's form is the grammar's; which functions exist and what their arguments mean are the host's.
func ParseCall(text, grammarVersion string) (Call, error) {
	call, _, err := parseCallRecordingTokens(text, grammarVersion)
	return call, err
}

func parseCallRecordingTokens(text, grammarVersion string) (Call, []grammarToken, error) {
	if err := checkGrammarVersion(grammarVersion); err != nil {
		return Call{}, nil, err
	}
	parser := &queryParser{reader: queryReader{runes: []rune(text)}}
	call, err := parser.parseCall()
	return call, parser.recorded, err
}

func (parser *queryParser) parseCall() (Call, error) {
	name, err := parser.peekStage()
	if err != nil {
		return Call{}, err
	}
	if name.kind != wordToken || name.escaped {
		return Call{}, fmt.Errorf("a call needs a function, not %s", describeToken(name))
	}
	parser.take(name, grammarUnquotedToken)
	call := Call{Function: strings.ToLower(name.text)}
	opening, err := parser.peekStage()
	if err != nil {
		return Call{}, err
	}
	if !opening.isSymbol("(") {
		return Call{}, fmt.Errorf("%q needs \"(\", not %s", call.Function, describeToken(opening))
	}
	parser.take(opening, grammarSymbolToken)
	closing, err := parser.peekStage()
	if err != nil {
		return Call{}, err
	}
	for separated := !closing.isSymbol(")"); separated; {
		argument, err := parser.parseCallArgument(call.Function)
		if err != nil {
			return Call{}, err
		}
		call.Arguments = append(call.Arguments, argument)
		if separated, err = parser.takeStageSymbol(","); err != nil {
			return Call{}, err
		}
	}
	if closing, err = parser.peekStage(); err != nil {
		return Call{}, err
	}
	if !closing.isSymbol(")") {
		return Call{}, errors.New("a parenthesis is not closed")
	}
	parser.take(closing, grammarSymbolToken)
	following, err := parser.peekStage()
	if err != nil {
		return Call{}, err
	}
	if following.kind != endToken {
		return Call{}, fmt.Errorf("unexpected %s", describeToken(following))
	}
	return call, nil
}

func (parser *queryParser) parseCallArgument(function string) (CallArgument, error) {
	argument, err := parser.peekStage()
	if err != nil {
		return CallArgument{}, err
	}
	switch argument.kind {
	case wordToken:
		parser.take(argument, grammarUnquotedToken)
		return CallArgument{Kind: FieldCallArgument, Text: argument.text}, nil
	case numberToken:
		parser.take(argument, grammarNumberToken)
		return CallArgument{Kind: NumberCallArgument, Text: argument.text}, nil
	case quotedToken:
		parser.take(argument, grammarQuotedToken)
		return CallArgument{Kind: QuotedCallArgument, Text: argument.text}, nil
	}
	return CallArgument{}, fmt.Errorf("%q needs a field, a number or a quoted string, not %s", function, describeToken(argument))
}
