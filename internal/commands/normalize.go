package commands

import (
	"strings"
	"unicode"
)

// fillerWords carry no meaning for matching a request to a command.
var fillerWords = map[string]bool{
	"the": true, "a": true, "an": true, "please": true, "can": true, "could": true,
	"would": true, "you": true, "will": true, "hey": true, "ok": true, "okay": true,
	"just": true, "now": true, "for": true, "me": true, "my": true, "thanks": true,
	"thank": true, "i": true, "want": true, "like": true, "go": true, "ahead": true,
}

// relativeWords mark requests whose repetition is the point ("brighter",
// "volume up", "next song"); they are never treated as a failed attempt.
var relativeWords = map[string]bool{
	"brighter": true, "dimmer": true, "dim": true, "up": true, "down": true,
	"next": true, "previous": true, "skip": true, "volume": true, "louder": true,
	"quieter": true, "more": true, "less": true, "increase": true, "decrease": true,
	"raise": true, "lower": true, "again": true, "warmer": true, "cooler": true,
}

// Normalize lowercases a spoken request, drops punctuation (keeping % as its
// own token) and collapses whitespace: "Turn the Basement lights OFF." becomes
// "turn the basement lights off".
func Normalize(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == '%':
			b.WriteString(" % ")
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'':
			// "don't" -> "dont"
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Tokens returns the meaningful words of a normalized request as a set.
// With numbers=false, numeric tokens and % are dropped so "50%" and "30%"
// requests match the same examples.
func Tokens(normalized string, numbers bool) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(normalized) {
		if fillerWords[w] {
			continue
		}
		if !numbers && (w == "%" || isNumber(w)) {
			continue
		}
		out[w] = true
	}
	return out
}

// IsRelative reports whether a request is relative (repeating it is intended).
// A request with a number ("dim the lights to 30%") is absolute.
func IsRelative(normalized string) bool {
	relative := false
	for _, w := range strings.Fields(normalized) {
		if isNumber(w) {
			return false
		}
		if relativeWords[w] {
			relative = true
		}
	}
	return relative
}

// Similarity is the Jaccard overlap of two token sets (0..1).
func Similarity(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func isNumber(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
