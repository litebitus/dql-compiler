package dqlsyntax

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var latestMinorByMajor = map[int]int{1: 0}

var grammarVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)$`)

// SupportedGrammarVersions is the latest version of every major this compiler reads.
func SupportedGrammarVersions() []string {
	var supported []string
	for _, major := range slices.Sorted(maps.Keys(latestMinorByMajor)) {
		supported = append(supported, fmt.Sprintf("%d.%d", major, latestMinorByMajor[major]))
	}
	return supported
}

func checkGrammarVersion(grammarVersion string) error {
	version := grammarVersionPattern.FindStringSubmatch(grammarVersion)
	if version == nil {
		return fmt.Errorf("a grammar version is written <major>.<minor>, not %q", grammarVersion)
	}
	major, _ := strconv.Atoi(version[1])
	minor, _ := strconv.Atoi(version[2])
	if latestMinor, known := latestMinorByMajor[major]; !known || minor > latestMinor {
		return fmt.Errorf("grammar version %s is not supported; supported: %s",
			grammarVersion, strings.Join(SupportedGrammarVersions(), ", "))
	}
	return nil
}
