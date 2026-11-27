package web

type platformStyleView struct {
	Symbol string
	Mark   string
	Brand  string
}

var platformSymbols = []string{"cartridge", "handheld", "dual-screen", "disc", "tablet", "arcade", "gamepad"}

var genericPlatformStyle = platformStyleView{Symbol: "gamepad", Mark: "ROM", Brand: "generic"}

var platformStyles = map[string]platformStyleView{
	"nes":       {Symbol: "cartridge", Mark: "NES", Brand: "nintendo"},
	"snes":      {Symbol: "cartridge", Mark: "SNES", Brand: "nintendo"},
	"gb":        {Symbol: "handheld", Mark: "GB", Brand: "nintendo"},
	"gbc":       {Symbol: "handheld", Mark: "GBC", Brand: "nintendo"},
	"gba":       {Symbol: "handheld", Mark: "GBA", Brand: "nintendo"},
	"genesis":   {Symbol: "cartridge", Mark: "MD", Brand: "sega"},
	"n64":       {Symbol: "cartridge", Mark: "N64", Brand: "nintendo"},
	"nds":       {Symbol: "dual-screen", Mark: "NDS", Brand: "nintendo"},
	"psx":       {Symbol: "disc", Mark: "PS1", Brand: "sony"},
	"ps2":       {Symbol: "disc", Mark: "PS2", Brand: "sony"},
	"psp":       {Symbol: "handheld", Mark: "PSP", Brand: "sony"},
	"ngc":       {Symbol: "disc", Mark: "GC", Brand: "nintendo"},
	"wii":       {Symbol: "disc", Mark: "WII", Brand: "nintendo"},
	"n3ds":      {Symbol: "dual-screen", Mark: "3DS", Brand: "nintendo"},
	"saturn":    {Symbol: "disc", Mark: "SAT", Brand: "sega"},
	"dc":        {Symbol: "disc", Mark: "DC", Brand: "sega"},
	"switch":    {Symbol: "tablet", Mark: "NSW", Brand: "nintendo"},
	"psvita":    {Symbol: "handheld", Mark: "VITA", Brand: "sony"},
	"arcade":    {Symbol: "arcade", Mark: "ARC", Brand: "arcade"},
	"atari2600": {Symbol: "cartridge", Mark: "2600", Brand: "atari"},
	"lynx":      {Symbol: "handheld", Mark: "LYNX", Brand: "atari"},
	"ngage":     {Symbol: "handheld", Mark: "NGE", Brand: "nokia"},
}

func platformStyle(slug any) platformStyleView {
	if name, ok := slug.(string); ok {
		if style, ok := platformStyles[name]; ok {
			return style
		}
	}
	return genericPlatformStyle
}
