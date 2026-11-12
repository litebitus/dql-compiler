package promqlbackend

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/litebitus/dql-compiler/dqlsyntax"
)

type compiledExample struct {
	Description string `yaml:"description"`
	Query       string `yaml:"query"`
	PromQL      string `yaml:"promql"`
}

type examplesFile struct {
	GrammarVersion string            `yaml:"grammar_version"`
	Examples       []compiledExample `yaml:"examples"`
}

func readExamplesFile(t *testing.T, path string) examplesFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var file examplesFile
	if err := decoder.Decode(&file); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return file
}

func TestEveryExampleCompilesToThePromQLItStates(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "examples", "promql", "*.yml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no example files: %v", err)
	}
	for _, path := range paths {
		file := readExamplesFile(t, path)
		if len(file.Examples) == 0 {
			t.Errorf("%s: holds no examples", path)
		}
		for _, example := range file.Examples {
			if example.Description == "" {
				t.Errorf("%s: %q has no description", path, example.Query)
			}
			parsed, err := dqlsyntax.ParseQuery(example.Query, file.GrammarVersion)
			if err != nil {
				t.Errorf("%s: %q: %v", path, example.Query, err)
				continue
			}
			got, err := CompileQuery(parsed, BackendConfig{})
			if err != nil || got != example.PromQL {
				t.Errorf("%s: %q:\n got  %s (%v)\n want %s", path, example.Query, got, err, example.PromQL)
			}
		}
	}
}
