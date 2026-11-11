package dqlsyntax

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var latestMinorByMajor = map[int]int{1: 1}

var grammarVersionPattern = regexp.MustCompile(`^(\d+)(?:\.(\d+))?$`)

// grammarVersionNumber is the version a query is written in, which decides the forms the parser reads.
type grammarVersionNumber struct {
	major int
	minor int
}

func (version grammarVersionNumber) readsFrom(major, minor int) bool {
	return version.major == major && version.minor >= minor
}

func (version grammarVersionNumber) String() string {
	return fmt.Sprintf("%d.%d", version.major, version.minor)
}

// SupportedGrammarVersions is the latest version of every major this compiler reads.
func SupportedGrammarVersions() []string {
	var supported []string
	for _, major := range slices.Sorted(maps.Keys(latestMinorByMajor)) {
		supported = append(supported, fmt.Sprintf("%d.%d", major, latestMinorByMajor[major]))
	}
	return supported
}

// checkGrammarVersion reads a stated version: a minor names itself, and a major alone floats to its newest minor.
func checkGrammarVersion(writtenVersion string) (grammarVersionNumber, error) {
	version := grammarVersionPattern.FindStringSubmatch(writtenVersion)
	if version == nil {
		return grammarVersionNumber{}, fmt.Errorf(
			"a grammar version is written <major> or <major>.<minor>, not %q", writtenVersion)
	}
	major, _ := strconv.Atoi(version[1])
	latestMinor, known := latestMinorByMajor[major]
	minor := latestMinor
	if version[2] != "" {
		minor, _ = strconv.Atoi(version[2])
	}
	if !known || minor > latestMinor {
		return grammarVersionNumber{}, fmt.Errorf("grammar version %s is not supported; supported: %s",
			writtenVersion, strings.Join(SupportedGrammarVersions(), ", "))
	}
	return grammarVersionNumber{major: major, minor: minor}, nil
}
