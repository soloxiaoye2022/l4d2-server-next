package logic

import "testing"

func TestParseGameServerPortSupportsQuotedAndEqualsForms(t *testing.T) {
	t.Setenv("L4D2_PORT", "")

	cases := map[string]int{
		`/l4d2/srcds_run +hostport 27015`:                          27015,
		`/l4d2/srcds_run +hostport="27016"`:                        27016,
		`/l4d2/srcds_run +port='27017'`:                            27017,
		`/l4d2/srcds_run -port "27018"`:                            27018,
		`/l4d2/srcds_run +hostport 27015 +hostport 27019`:          27019,
		`/l4d2/srcds_run -game "Left 4 Dead 2" +hostport 27020`:    27020,
		`/l4d2/srcds_run -game "path with space" +exec server.cfg`: DefaultGameServerPort,
	}

	for command, want := range cases {
		if got := ParseGameServerPort(command); got != want {
			t.Errorf("ParseGameServerPort(%q) = %d, want %d", command, got, want)
		}
	}
}

func TestParseGameServerPortRejectsMalformedCommand(t *testing.T) {
	t.Setenv("L4D2_PORT", "")

	for _, command := range []string{
		`/l4d2/srcds_run +hostport "27015`,
		`/l4d2/srcds_run +hostport 27015 \`,
		`/l4d2/srcds_run +hostport`,
		`/l4d2/srcds_run +hostport nope`,
	} {
		if got := ParseGameServerPort(command); got != DefaultGameServerPort {
			t.Errorf("ParseGameServerPort(%q) = %d, want fallback %d", command, got, DefaultGameServerPort)
		}
	}
}

func TestParseGameServerPortUsesEnvironmentFallback(t *testing.T) {
	t.Setenv("L4D2_PORT", "27123")
	if got := ParseGameServerPort(`/l4d2/srcds_run -game left4dead2`); got != 27123 {
		t.Fatalf("ParseGameServerPort() = %d, want 27123", got)
	}
}
