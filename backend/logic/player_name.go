package logic

import (
	"regexp"
	"strings"
	"unicode"
)

var playerColorTagPattern = regexp.MustCompile(`(?i)<#(?:[0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})>`)

// SanitizePlayerName removes formatting tags and characters that do not
// contribute visible text. It intentionally keeps ZWJ/ZWNJ so valid emoji
// sequences and languages that use joiners are not split.
func SanitizePlayerName(name string) string {
	name = playerColorTagPattern.ReplaceAllString(name, "")
	name = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && !unicode.IsSpace(r)) || isHiddenPlayerNameRune(r) {
			return -1
		}
		return r
	}, name)
	return strings.Join(strings.Fields(name), " ")
}

func DisplayPlayerName(name string) string {
	name = SanitizePlayerName(name)
	if name == "" {
		return "Unknown"
	}
	return name
}

func isHiddenPlayerNameRune(r rune) bool {
	switch r {
	case '\u00ad', // soft hyphen
		'\u034f',                                         // combining grapheme joiner
		'\u061c',                                         // Arabic letter mark
		'\u115f',                                         // Hangul choseong filler
		'\u1160',                                         // Hangul jungseong filler
		'\u17b4',                                         // Khmer inherent vowel AQ
		'\u17b5',                                         // Khmer inherent vowel AA
		'\u180e',                                         // Mongolian vowel separator
		'\u200b',                                         // zero width space
		'\u200e',                                         // left-to-right mark
		'\u200f',                                         // right-to-left mark
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e', // bidi embedding/override
		'\u2060', '\u2061', '\u2062', '\u2063', '\u2064', // invisible operators
		'\u2066', '\u2067', '\u2068', '\u2069', // bidi isolates
		'\u2800', // braille pattern blank
		'\u3164', // Hangul filler
		'\ufeff', // zero width no-break space / BOM
		'\uffa0': // halfwidth Hangul filler
		return true
	default:
		return false
	}
}
