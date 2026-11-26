package rom

import (
	"reflect"
	"testing"
)

func TestPlayStationCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, entry, readiness string
		statuses               []string
	}{
		{"documented playlist", "game.M3U", ReadinessComplete, []string{"format_supported", "format_supported", "format_supported"}},
		{"standalone bin differs by destination", "game.bin", ReadinessComplete, []string{"format_unsupported", "format_supported", "format_unsupported"}},
		{"missing bundle member", "game.m3u", ReadinessIncomplete, []string{"requirements_missing", "requirements_missing", "requirements_missing"}},
		{"uncertain bundle", "game.m3u", ReadinessUnknown, []string{"unknown", "unknown", "unknown"}},
		{"unclassified format", "game.other", ReadinessComplete, []string{"unknown", "unknown", "unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := PlayStationCompatibility(GameReadiness{Status: tc.readiness, EntryPoint: tc.entry})
			platform, ok := PlatformBySlug("psx")
			if !ok {
				t.Fatal("PS1 platform missing")
			}
			var ids, statuses []string
			for _, result := range report {
				ids = append(ids, result.Destination)
				statuses = append(statuses, result.Status)
				if result.GameplayVerified || result.Reason == "" || result.Evidence == "" {
					t.Fatalf("unexpected evidence: %+v", result)
				}
			}
			if !reflect.DeepEqual(ids, append([]string{"knulli"}, platform.Cores...)) || !reflect.DeepEqual(statuses, tc.statuses) {
				t.Fatalf("destinations = %v, statuses = %v; want %v for Knulli and %v", ids, statuses, tc.statuses, platform.Cores)
			}
		})
	}
}
