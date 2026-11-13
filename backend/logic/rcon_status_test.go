package logic

import "testing"

func TestParseStatusSanitizesPlayerNames(t *testing.T) {
	t.Parallel()

	status := ParseStatus(`hostname: Test
map     : c2m1_highway
players : 1 humans, 0 bots (8 max)
# 1 1 "<#FFB7DD>寿<#FFEE99>司" STEAM_1:0:1 01:02 30 0 active 100000 192.0.2.1:27005
`)
	if len(status.Users) != 1 {
		t.Fatalf("users = %d, want 1", len(status.Users))
	}
	if got := status.Users[0].Name; got != "寿司" {
		t.Fatalf("name = %q, want %q", got, "寿司")
	}
}
