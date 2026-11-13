package logic

import "testing"

func TestNormalizeSteamAPIKey(t *testing.T) {
	valid := "0123456789abcdef0123456789ABCDEF"
	normalized, err := NormalizeSteamAPIKey("  " + valid + "  ")
	if err != nil {
		t.Fatalf("expected valid key: %v", err)
	}
	if normalized != valid {
		t.Fatalf("expected trimmed key, got %q", normalized)
	}

	for _, invalid := range []string{"", "1234", "gggggggggggggggggggggggggggggggg"} {
		if invalid == "" {
			empty, err := NormalizeSteamAPIKey(invalid)
			if err != nil || empty != "" {
				t.Fatalf("empty key should clear the value")
			}
			continue
		}
		if _, err := NormalizeSteamAPIKey(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestSelfServiceConfigDoesNotExposeSteamAPIKey(t *testing.T) {
	managerConfigMutex.Lock()
	previous := managerConfig
	managerConfig = &ManagerConfig{SteamAPIKey: "0123456789abcdef0123456789ABCDEF"}
	managerConfigMutex.Unlock()
	t.Cleanup(func() {
		managerConfigMutex.Lock()
		managerConfig = previous
		managerConfigMutex.Unlock()
	})

	if got := GetSelfServiceConfig().SteamAPIKey; got != "" {
		t.Fatalf("GetSelfServiceConfig exposed Steam API key %q", got)
	}
}
