package ui

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/bjarneo/cliamp/theme"
)

func TestContrastingTextColor(t *testing.T) {
	tests := []struct {
		name   string
		accent string
		want   string
	}{
		{name: "light accent", accent: "#f7df50", want: "#000000"},
		{name: "dark accent", accent: "#3e4a5e", want: "#ffffff"},
		{name: "invalid accent", accent: "blue", want: "#ffffff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contrastingTextColor(tt.accent); got != tt.want {
				t.Errorf("contrastingTextColor(%q) = %q, want %q", tt.accent, got, tt.want)
			}
		})
	}
}

func TestBlendHex(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		t    float64
		want string
	}{
		{name: "midpoint", a: "#000000", b: "#ffffff", t: 0.5, want: "#808080"},
		{name: "start", a: "#bd93f9", b: "#ff5555", t: 0, want: "#bd93f9"},
		{name: "end", a: "#bd93f9", b: "#ff5555", t: 1, want: "#ff5555"},
		{name: "channels mixed independently", a: "#00ff00", b: "#ff0000", t: 0.5, want: "#808000"},
		{name: "pads leading zeros", a: "#000000", b: "#000010", t: 0.5, want: "#000008"},
		{name: "clamps t", a: "#000000", b: "#ffffff", t: 2, want: "#ffffff"},
		{name: "invalid input returns a", a: "#123456", b: "blue", t: 0.5, want: "#123456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := blendHex(tt.a, tt.b, tt.t); got != tt.want {
				t.Errorf("blendHex(%q, %q, %v) = %q, want %q", tt.a, tt.b, tt.t, got, tt.want)
			}
		})
	}
}

func TestApplyThemeColorsDerivesSpectrumFromAccent(t *testing.T) {
	th := theme.Theme{
		Name: "test", BG: "#000000", Accent: "#bd93f9", BrightFG: "#ffffff",
		FG: "#888888", Green: "#00ff00", Yellow: "#ffff00", Red: "#ff5555",
	}
	ApplyThemeColors(th)
	t.Cleanup(func() { ApplyThemeColors(theme.Default()) })

	if got, want := SpectrumLow, lipgloss.Color("#bd93f9"); got != want {
		t.Errorf("SpectrumLow = %v, want accent %v", got, want)
	}
	if got, want := SpectrumMid, lipgloss.Color("#dec9fc"); got != want {
		t.Errorf("SpectrumMid = %v, want accent/bright_fg blend %v", got, want)
	}
	if got, want := SpectrumHigh, lipgloss.Color("#ffffff"); got != want {
		t.Errorf("SpectrumHigh = %v, want bright_fg %v", got, want)
	}
	if specLowPrefix == "" || specMidPrefix == "" || specHighPrefix == "" {
		t.Errorf("spectrum ANSI prefixes were not rebuilt: %q %q %q", specLowPrefix, specMidPrefix, specHighPrefix)
	}
}

// The blended middle tier is not in any theme file, so the theme package's
// accessibility test cannot cover it. Check it here against every built-in bg.
func TestBuiltinThemesSpectrumMidContrast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	themes := theme.LoadAll()
	if len(themes) == 0 {
		t.Fatal("LoadAll() returned no built-in themes")
	}
	for _, th := range themes {
		mid := spectrumMidColor(th)
		if _, err := relativeLuminance(mid); err != nil {
			t.Fatalf("theme %q: spectrum mid %q is not a hex color: %v", th.Name, mid, err)
		}
		if ratio := contrastRatio(mid, th.BG); ratio < minSpectrumContrast {
			t.Errorf("theme %q spectrum mid %s contrast = %.2f:1, want at least %.1f:1", th.Name, mid, ratio, minSpectrumContrast)
		}
	}
}

func TestSpectrumMidColorNudgesTowardContrast(t *testing.T) {
	// Dark accent and text on a dark bg: the even blend is too dim, so the
	// result must move toward the endpoint with more contrast (accent here).
	th := theme.Theme{BG: "#000000", Accent: "#9a9a9a", BrightFG: "#5a0000"}
	got := spectrumMidColor(th)
	if got == blendHex(th.Accent, th.BrightFG, 0.5) {
		t.Fatalf("spectrumMidColor returned the even blend %s despite low contrast", got)
	}
	if ratio := contrastRatio(got, th.BG); ratio < minSpectrumContrast {
		t.Errorf("spectrumMidColor = %s, contrast %.2f:1, want at least %.1f:1", got, ratio, minSpectrumContrast)
	}
	// No background: nothing to contrast against, keep the even blend.
	if got := spectrumMidColor(theme.Theme{Accent: "#000000", BrightFG: "#ffffff"}); got != "#808080" {
		t.Errorf("spectrumMidColor without bg = %s, want #808080", got)
	}
}
