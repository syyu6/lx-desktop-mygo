package subscription

import (
	"regexp"
	"strings"
)

// The helpers ParseLink and SourceOf ask, which the standard library has
// no names for.

func trimSpace(s string) string { return strings.TrimSpace(s) }

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

var patterns = map[string]*regexp.Regexp{}

// matchID returns the first group of pat in text.
func matchID(pat, text string) (string, bool) {
	re, ok := patterns[pat]
	if !ok {
		var err error
		re, err = regexp.Compile(pat)
		if err != nil {
			return "", false
		}
		patterns[pat] = re
	}
	m := re.FindStringSubmatch(text)
	if len(m) < 2 || m[1] == "" {
		return "", false
	}
	return m[1], true
}
