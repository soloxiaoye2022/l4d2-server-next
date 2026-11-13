package logic

import "testing"

func TestSanitizePlayerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "普通玩家", want: "普通玩家"},
		{name: "gradient tags", in: "<#FFB7DD>寿<#FFEE99>司<#CCFF99>团", want: "寿司团"},
		{name: "mixed tag lengths", in: "<#abc>A<#abcd>B<#ABCDEF>C<#11223344>D", want: "ABCD"},
		{name: "controls and bidi", in: "A\x00B\u200bC\u202eD\ufeff", want: "ABCD"},
		{name: "whitespace", in: "  玩家\t名字  ", want: "玩家 名字"},
		{name: "emoji joiner preserved", in: "👨\u200d👩\u200d👧\u200d👦", want: "👨\u200d👩\u200d👧\u200d👦"},
		{name: "only formatting", in: "<#fff><#000000>", want: ""},
		{name: "non tag angle brackets", in: "<A>#123", want: "<A>#123"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SanitizePlayerName(test.in); got != test.want {
				t.Fatalf("SanitizePlayerName(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestDisplayPlayerNameFallback(t *testing.T) {
	t.Parallel()

	if got := DisplayPlayerName("<#fff><#000000>"); got != "Unknown" {
		t.Fatalf("DisplayPlayerName fallback = %q, want Unknown", got)
	}
}
