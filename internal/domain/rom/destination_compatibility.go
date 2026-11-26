package rom

import (
	"path"
	"slices"
	"strings"
)

type DestinationCompatibility struct {
	Destination      string `json:"destination"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	Evidence         string `json:"evidence"`
	GameplayVerified bool   `json:"gameplay_verified"`
}

var playstationDestinations = []struct {
	id, evidence, supported, unsupported string
}{
	{
		"knulli", "https://github.com/knulli-cfw/distribution/blob/3378f497/package/batocera/emulationstation/batocera-es-system/es_systems.yml",
		".cue .img .mdf .pbp .toc .cbn .m3u .ccd .chd .iso", ".bin",
	},
	{
		"pcsx_rearmed", "https://docs.libretro.com/library/pcsx_rearmed/#extensions",
		".bin .cue .img .mdf .pbp .toc .cbn .m3u .ccd .chd .iso .exe", "",
	},
	{
		"mednafen_psx_hw", "https://docs.libretro.com/library/beetle_psx_hw/#extensions",
		".cue .toc .m3u .ccd .exe .pbp .chd", ".bin .img",
	},
}

func PlayStationCompatibility(report GameReadiness) []DestinationCompatibility {
	ext := strings.ToLower(path.Ext(report.EntryPoint))
	destinations := make([]DestinationCompatibility, 0, len(playstationDestinations))
	for _, profile := range playstationDestinations {
		result := DestinationCompatibility{Destination: profile.id, Evidence: profile.evidence}
		switch {
		case report.Status == ReadinessIncomplete:
			result.Status = "requirements_missing"
			result.Reason = "The installed game is missing files or discs listed in this report."
		case slices.Contains(strings.Fields(profile.unsupported), ext):
			result.Status = "format_unsupported"
			result.Reason = "The documented launch formats do not include this standalone entry point."
		case report.Status == ReadinessComplete && slices.Contains(strings.Fields(profile.supported), ext):
			result.Status = "format_supported"
			result.Reason = "The launch format is documented. Firmware, emulator configuration, and gameplay have not been checked."
		default:
			result.Status = "unknown"
			result.Reason = "The installed set or its launch format could not be confirmed for this destination."
		}
		destinations = append(destinations, result)
	}
	return destinations
}
