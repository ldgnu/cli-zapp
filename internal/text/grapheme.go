package text

import (
	"strconv"
	"strings"

	"github.com/rivo/uniseg"
)

// clusterIter iterates the grapheme clusters of s.
//
// A grapheme cluster is what a reader perceives as one character: a base letter
// with its combining accents, or an emoji with its skin-tone modifier. Splitting
// one apart produces a replacement box, so both the width measurement and the
// truncation here work on clusters rather than on runes.
// iter is a single pass over a string's grapheme clusters, stopping early when
// the callback returns false.
//
// A callback rather than a slice because callers here are single-pass and
// streaming; materialising the clusters would allocate for every message line.
func iter(s string, fn func(cluster string) bool) {
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		if !fn(gr.Str()) {
			return
		}
	}
}

// clusterWidth returns the cells a grapheme cluster occupies.
func clusterWidth(cluster string) int {
	return uniseg.StringWidth(cluster)
}

// clusters splits s into grapheme clusters.
func clusters(s string) []string {
	var out []string
	iter(s, func(c string) bool {
		out = append(out, c)
		return true
	})
	return out
}

// splitByCluster breaks s into one grapheme cluster per element.
//
// Used as the last resort when the available width is narrower than a single
// cluster. The resulting lines may exceed the requested width, because a
// double-width character cannot be split across two columns without producing
// mojibake — but looping forever would be worse.
func splitByCluster(s string) []string { return clusters(s) }

// Trim collapses surrounding whitespace and returns the result.
//
// It exists so that callers do not reach for strings.TrimSpace and then need a
// separate length check; "is there anything to send" appears at every call site
// where a composer decides whether its buffer is worth submitting.
func Trim(s string) string { return strings.TrimSpace(s) }

// Itoa formats a small integer.
//
// Exported because the message list and the chat list both render counts inline,
// and duplicating the conversion in each is how the two drift apart.
func Itoa(n int) string { return strconv.Itoa(n) }
