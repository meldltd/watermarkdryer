// Unicode classification and contextual preservation rules adapted from
// guillaumemeyer/watermarks-remover, service/scripts/text_unicode.py (MIT).
// See internal/cli/licenses.txt for the original copyright and license.
package watermark

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/unicode/runenames"
)

func between(r, lo, hi rune) bool { return lo <= r && r <= hi }

func oneOf(r rune, chars string) bool { return strings.ContainsRune(chars, r) }

func variation(r rune) bool {
	return between(r, 0xFE00, 0xFE0F) || between(r, 0xE0100, 0xE01EF) || oneOf(r, "\u180b\u180c\u180d\u180f")
}

func glue(r rune) bool {
	return variation(r) || oneOf(r, "\u200c\u200d\u17b4\u17b5\u115f\u1160\u3164\uffa0") || between(r, 0xE0020, 0xE007F)
}

func emoji(r rune) bool {
	return between(r, 0x1F000, 0x1FAFF) || between(r, 0x2190, 0x27BF) || between(r, 0x2B00, 0x2BFF) ||
		between(r, '0', '9') || oneOf(r, "#*\u203c\u2049\u2139\u2934\u2935\u00a9\u00ae\u2122\u3030\u303d\u3297\u3299")
}

func cjk(r rune) bool {
	return between(r, 0x3400, 0x4DBF) || between(r, 0x4E00, 0x9FFF) || between(r, 0xF900, 0xFAFF) || between(r, 0x20000, 0x323AF)
}

func hangul(r rune) bool {
	return between(r, 0x1100, 0x11FF) || between(r, 0xA960, 0xA97C) || between(r, 0xD7B0, 0xD7C6) || between(r, 0x3131, 0x318E) || between(r, 0xFFA1, 0xFFDC)
}

func joiningScript(r rune) int {
	if !unicode.IsLetter(r) && !unicode.IsMark(r) {
		return 0
	}
	for i, span := range [][2]rune{{0x600, 0x8FF}, {0x900, 0xDFF}, {0xF00, 0x109F}, {0x1780, 0x17FF}, {0x1800, 0x18AF}} {
		if between(r, span[0], span[1]) {
			return i + 1
		}
	}
	return 0
}

func bidi(r rune) bool {
	return oneOf(r, "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069")
}
func preservableBidi(r rune) bool { return oneOf(r, "\u061c\u200e\u200f\u2066\u2067\u2068\u2069") }

func stripKind(r rune) string {
	switch {
	case between(r, 0xE0001, 0xE007F):
		return "tag_character"
	case between(r, 0xFDD0, 0xFDEF) || r&0xFFFE == 0xFFFE:
		return "noncharacter"
	case r == 0x2065 || r == 0xE0000 || between(r, 0xFFF0, 0xFFF8) || between(r, 0xE0080, 0xE00FF) || between(r, 0xE01F0, 0xE0FFF):
		return "reserved_ignorable"
	case variation(r):
		return "variation_selector"
	case bidi(r):
		return "bidi_control"
	case oneOf(r, "\u200b\u200c\u200d\u2060\ufeff\u180e"):
		return "zero_width"
	case unicode.Is(unicode.Co, r):
		return "private_use"
	case oneOf(r, "\u00ad\u034f\u115f\u1160\u17b4\u17b5\u3164\uffa0\ufff9\ufffa\ufffb"):
		return "invisible"
	case unicode.Is(unicode.Cf, r):
		return "format_control"
	}
	return ""
}

func exoticSpace(r rune) bool {
	return oneOf(r, "\u00a0\u1680\u202f\u205f\u3000") || between(r, 0x2000, 0x200A)
}

func confusable(r rune) rune {
	const from = "АВЕКМНОРСТХаеорсухі"
	const to = "ABEKMHOPCTXaeopcyxi"
	for i, c := range []rune(from) {
		if c == r {
			return rune(to[i])
		}
	}
	if between(r, 0xFF21, 0xFF3A) || between(r, 0xFF41, 0xFF5A) {
		return r - 0xFEE0
	}
	return r
}

// validContext computes paired bidi embeddings and complete emoji flag tags.
func validContext(rs []rune) (map[int]bool, map[int]bool) {
	flags, pairs := map[int]bool{}, map[int]bool{}
	var stack []int
	for i, r := range rs {
		if oneOf(r, "\u202a\u202b\u202d\u202e") {
			stack = append(stack, i)
		}
		if r == 0x202C && len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if rs[j] == 0x202A || rs[j] == 0x202B {
				pairs[i], pairs[j] = true, true
			}
		}
		if r != 0x1F3F4 {
			continue
		}
		j := i + 1
		for j < len(rs) && between(rs[j], 0xE0020, 0xE007E) {
			j++
		}
		if j > i+1 && j < len(rs) && rs[j] == 0xE007F {
			for k := i + 1; k <= j; k++ {
				flags[k] = true
			}
		}
	}
	return flags, pairs
}

func preserveGlue(r, previous, base, next rune, flag bool) bool {
	if (between(r, 0xE0100, 0xE01EF) || between(r, 0xFE00, 0xFE0D)) && cjk(previous) {
		return true
	}
	if oneOf(r, "\u180b\u180c\u180d\u180f") && between(previous, 0x1800, 0x18AF) {
		return true
	}
	if (r == 0xFE0E || r == 0xFE0F) && emoji(previous) {
		return true
	}
	if r == 0x200D && emoji(base) && emoji(next) {
		return true
	}
	if r == 0x200C || r == 0x200D {
		if s := joiningScript(previous); s != 0 && s == joiningScript(next) {
			return true
		}
	}
	if between(r, 0xE0020, 0xE007F) && flag {
		return true
	}
	if oneOf(r, "\u180b\u180c\u180d\u180f") && between(base, 0x1800, 0x18AF) && unicode.IsLetter(base) {
		return true
	}
	if (r == 0x17B4 || r == 0x17B5) && between(base, 0x1780, 0x17FF) && unicode.IsLetter(base) {
		return true
	}
	if oneOf(r, "\u115f\u1160\u3164\uffa0") && hangul(base) {
		return true
	}
	if oneOf(r, "\u0600\u0601\u0602\u0603\u0604\u0605\u06dd\u070f\u08e2\U000110bd\U000110cd") {
		return true
	}
	for _, s := range [][4]rune{{0x13430, 0x1343F, 0x13000, 0x143FF}, {0x1BCA0, 0x1BCA3, 0x1BC00, 0x1BCA3}, {0x1D173, 0x1D17A, 0x1D100, 0x1D1FF}} {
		if between(r, s[0], s[1]) && (between(previous, s[2], s[3]) || between(next, s[2], s[3])) {
			return true
		}
	}
	return false
}

// CleanText returns findings and the exact text removal mode would write.
// Sample locations refer to the input, including when earlier characters vanish.
func CleanText(input string, opts Options) (string, []Finding) {
	rs := []rune(input)
	flags, pairs := validContext(rs)
	hits := []Finding{}
	buckets := map[string]int{}
	var out strings.Builder
	out.Grow(len(input))
	var base rune
	previousSurvived := true
	line, column := 1, 1
	for i, r := range rs {
		if r < 0x80 {
			out.WriteRune(r)
			base = r
			previousSurvived = true
			if r == '\n' {
				line++
				column = 1
			} else {
				column++
			}
			continue
		}
		var prev, next rune
		if i > 0 {
			prev = rs[i-1]
			if !previousSurvived {
				prev = 0
			}
		}
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		kind, action, replacement := "", "preserve", r
		switch {
		case bidi(r) && !opts.StripBidi && (pairs[i] || preservableBidi(r)):
			kind = "bidi_control"
		case !opts.StripGlue && preserveGlue(r, prev, base, next, flags[i]):
		case stripKind(r) != "":
			kind, action = stripKind(r), "remove"
		case exoticSpace(r):
			kind = "space_homoglyph"
			if !opts.KeepSpaces {
				action, replacement = "replace", ' '
			}
		case opts.AggressiveHomoglyphs && confusable(r) != r:
			kind, action, replacement = "confusable", "replace", confusable(r)
		}
		if kind != "" {
			key := fmt.Sprintf("%X/%s", r, action)
			idx, ok := buckets[key]
			if !ok {
				idx = len(hits)
				buckets[key] = idx
				name := runenames.Name(r)
				if name == "" {
					name = kind
				}
				confidence := "indicator"
				if kind == "space_homoglyph" || action == "preserve" {
					confidence = "informational"
				}
				hits = append(hits, Finding{Kind: kind, Detail: name, Codepoint: fmt.Sprintf("U+%04X", r), Action: action, Confidence: confidence})
			}
			hits[idx].Count++
			if len(hits[idx].Locations) < 10 {
				hits[idx].Locations = append(hits[idx].Locations, Location{i, line, column})
			}
		}
		if action != "remove" {
			out.WriteRune(replacement)
			if !glue(r) {
				base = replacement
			}
		}
		previousSurvived = action != "remove"
		if r == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	cleaned := out.String()
	// Findings use original positions even if Unicode cleanup shortened text.
	_, whitespaceHits := trailingWhitespace(input, opts.StripTrailingWhitespace)
	if opts.StripTrailingWhitespace {
		cleaned, _ = trailingWhitespace(cleaned, true)
	}
	hits = append(hits, whitespaceHits...)
	if opts.NFKC && !norm.NFKC.IsNormalString(cleaned) {
		// Normalization can change the script context of a preserved selector
		// (for example by composing Hangul jamo). Scrub that result too.
		plainOpts := opts
		plainOpts.NFKC = false
		for {
			next, _ := CleanText(norm.NFKC.String(cleaned), plainOpts)
			if next == cleaned {
				break
			}
			cleaned = next
		}
		f := finding("normalization", "Unicode NFKC compatibility normalization")
		f.Action = "replace"
		f.Confidence = "informational"
		hits = append(hits, f)
	}
	return cleaned, hits
}
