package dqlsyntax

import (
	"reflect"
	"testing"
)

func TestACallParsesIntoItsFunctionAndArguments(t *testing.T) {
	got, err := ParseCall(`Mock(job, 2.5, "__name__:mock_up and env:${env}")`, "1.0")
	want := Call{Function: "mock", Arguments: []CallArgument{
		{Kind: FieldCallArgument, Text: "job"},
		{Kind: NumberCallArgument, Text: "2.5"},
		{Kind: QuotedCallArgument, Text: "__name__:mock_up and env:${env}"},
	}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("call = %#v (%v), want %#v", got, err, want)
	}
}

func TestACallInAnUnsupportedGrammarVersionIsRefused(t *testing.T) {
	_, err := ParseCall("mock()", "2.0")
	if want := "grammar version 2.0 is not supported; supported: 1.1"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}
