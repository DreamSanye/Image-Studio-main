package ui

import (
	_ "embed"

	"gioui.org/font"
	"gioui.org/font/opentype"
)

const (
	uiFallbackSansTypeface = font.Typeface("HarmonyOS Sans SC")
	uiFallbackMonoTypeface = font.Typeface("JetBrains Mono")
	uiSansTypeface         = font.Typeface(`"Segoe UI Variable Text", "Segoe UI", "HarmonyOS Sans SC"`)
	uiTitleTypeface        = font.Typeface(`"Segoe UI Variable Display", "Segoe UI", "HarmonyOS Sans SC"`)
	uiMonoTypeface         = font.Typeface(`"Cascadia Code", "JetBrains Mono", Consolas`)
)

//go:embed assets/HarmonyOS_SansSC_Regular.ttf
var harmonySansSC []byte

//go:embed assets/JetBrainsMono-Regular.ttf
var jetBrainsMono []byte

func bundledFontCollection() []font.FontFace {
	out := make([]font.FontFace, 0, 4)
	out = append(out, parseBundledFont(harmonySansSC, uiFallbackSansTypeface)...)
	out = append(out, parseBundledFont(jetBrainsMono, uiFallbackMonoTypeface)...)
	return out
}

func parseBundledFont(src []byte, typeface font.Typeface) []font.FontFace {
	faces, err := opentype.ParseCollection(src)
	if err != nil {
		return nil
	}
	for idx := range faces {
		faces[idx].Font.Typeface = typeface
	}
	return faces
}
