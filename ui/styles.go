package ui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/bjarneo/cliamp/theme"
)

// CLIAMP color palette using standard ANSI terminal colors (0-15).
// These adapt to the user's terminal theme for consistent appearance.
var (
	ColorBackground color.Color
	ColorTitle      color.Color = lipgloss.ANSIColor(10) // bright green
	ColorText       color.Color = lipgloss.ANSIColor(15) // bright white
	ColorDim        color.Color = lipgloss.ANSIColor(7)  // white (light gray)
	ColorAccent     color.Color = lipgloss.ANSIColor(11) // bright yellow
	ColorPlaying    color.Color = lipgloss.ANSIColor(10) // bright green
	ColorSeekBar    color.Color = lipgloss.ANSIColor(11) // bright yellow
	ColorError      color.Color = lipgloss.ANSIColor(9)  // bright red
	ColorWarning    color.Color = lipgloss.ANSIColor(11) // bright yellow
	ColorKeyBG      color.Color = lipgloss.ANSIColor(8)  // bright black (dark gray)
	ColorKeyFG      color.Color = lipgloss.ANSIColor(15) // bright white

	// Spectrum gradient: green -> yellow -> red for the terminal default;
	// themed runs accent -> accent/bright_fg blend -> bright_fg (see
	// ApplyThemeColors).
	SpectrumLow  color.Color = lipgloss.ANSIColor(10) // bright green
	SpectrumMid  color.Color = lipgloss.ANSIColor(11) // bright yellow
	SpectrumHigh color.Color = lipgloss.ANSIColor(9)  // bright red
)

// PaddingH is the horizontal padding inside the frame.
var PaddingH = 3

// paddingV is the vertical padding inside the frame.
var paddingV = 1

// PanelWidth is the usable inner width of the frame.
// Updated dynamically in WindowSizeMsg based on terminal width.
var PanelWidth = 80 - 2*PaddingH

// SetPadding updates the frame padding and derived styles.
func SetPadding(h, v int) {
	PaddingH = h
	paddingV = v
	PanelWidth = 80 - 2*PaddingH
	FrameStyle = FrameStyle.Padding(paddingV, PaddingH)
}

// VerticalPadding returns the current frame padding above and below content.
func VerticalPadding() int {
	return paddingV
}

// FrameStyle is the outer frame style for the TUI.
var FrameStyle = lipgloss.NewStyle().
	Padding(paddingV, PaddingH).
	Width(80)

// ApplyThemeColors updates all color variables and rebuilds spectrum styles.
// If the theme is the default (empty hex values), ANSI fallback colors are restored.
func ApplyThemeColors(t theme.Theme) {
	if t.IsDefault() {
		ColorBackground = nil
		ColorTitle = lipgloss.ANSIColor(10)
		ColorText = lipgloss.ANSIColor(15)
		ColorDim = lipgloss.ANSIColor(7)
		ColorAccent = lipgloss.ANSIColor(11)
		ColorPlaying = lipgloss.ANSIColor(10)
		ColorSeekBar = lipgloss.ANSIColor(11)
		ColorError = lipgloss.ANSIColor(9)
		ColorWarning = lipgloss.ANSIColor(11)
		ColorKeyBG = lipgloss.ANSIColor(8)
		ColorKeyFG = lipgloss.ANSIColor(15)
		SpectrumLow = lipgloss.ANSIColor(10)
		SpectrumMid = lipgloss.ANSIColor(11)
		SpectrumHigh = lipgloss.ANSIColor(9)
	} else {
		if t.BG == "" {
			ColorBackground = nil
		} else {
			ColorBackground = lipgloss.Color(t.BG)
		}
		ColorTitle = lipgloss.Color(t.Accent)
		ColorText = lipgloss.Color(t.BrightFG)
		ColorDim = lipgloss.Color(t.FG)
		ColorAccent = lipgloss.Color(t.Accent)
		ColorPlaying = lipgloss.Color(t.Green)
		ColorSeekBar = lipgloss.Color(t.Accent)
		ColorError = lipgloss.Color(t.Red)
		ColorWarning = lipgloss.Color(t.Yellow)
		ColorKeyBG = lipgloss.Color(t.Accent)
		ColorKeyFG = lipgloss.Color(contrastingTextColor(t.Accent))
		// The visualizer gradient runs from the theme's accent at the base
		// to its primary text color at the peak so every theme is
		// recognizable in the visualizer, not just in the chrome around it.
		SpectrumLow = lipgloss.Color(t.Accent)
		SpectrumMid = lipgloss.Color(spectrumMidColor(t))
		SpectrumHigh = lipgloss.Color(t.BrightFG)
	}

	// Rebuild visualizer spectrum styles.
	specLowStyle = lipgloss.NewStyle().Foreground(SpectrumLow)
	specMidStyle = lipgloss.NewStyle().Foreground(SpectrumMid)
	specHighStyle = lipgloss.NewStyle().Foreground(SpectrumHigh)
	refreshSpecANSI()
}

func contrastingTextColor(hex string) string {
	luminance, err := relativeLuminance(hex)
	if err != nil {
		return "#ffffff"
	}
	// This is the crossover where black provides more contrast than white.
	if luminance > 0.179 {
		return "#000000"
	}
	return "#ffffff"
}

// relativeLuminance returns the WCAG relative luminance of a #RRGGBB color.
func relativeLuminance(hex string) (float64, error) {
	value, err := parseHex(hex)
	if err != nil {
		return 0, err
	}
	linear := func(channel uint64) float64 {
		component := float64(channel) / 255
		if component <= 0.04045 {
			return component / 12.92
		}
		return math.Pow((component+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(value>>16) + 0.7152*linear((value>>8)&0xff) + 0.0722*linear(value&0xff), nil
}

// minSpectrumContrast matches the text contrast floor built-in themes are
// tested against, so the derived middle tier never reads worse than the
// theme's own colors.
const minSpectrumContrast = 4.5

// spectrumMidColor returns the middle visualizer tier for a custom theme: an
// even accent/bright_fg blend, nudged toward whichever endpoint contrasts
// better with the background when the even blend would fall below the floor.
func spectrumMidColor(t theme.Theme) string {
	mid := blendHex(t.Accent, t.BrightFG, 0.5)
	if t.BG == "" || contrastRatio(mid, t.BG) >= minSpectrumContrast {
		return mid
	}
	target := 0.0 // toward accent
	if contrastRatio(t.BrightFG, t.BG) > contrastRatio(t.Accent, t.BG) {
		target = 1.0 // toward bright_fg
	}
	for step := 1; step <= 5; step++ {
		f := 0.5 + (target-0.5)*float64(step)/5
		mid = blendHex(t.Accent, t.BrightFG, f)
		if contrastRatio(mid, t.BG) >= minSpectrumContrast {
			return mid
		}
	}
	return mid
}

// contrastRatio returns the WCAG contrast ratio between two #RRGGBB colors,
// or 0 when either is malformed.
func contrastRatio(a, b string) float64 {
	la, errA := relativeLuminance(a)
	lb, errB := relativeLuminance(b)
	if errA != nil || errB != nil {
		return 0
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// blendHex mixes two #RRGGBB colors per channel; t=0 yields a, t=1 yields b.
// Falls back to a when either input is malformed so a bad theme value never
// produces an unparseable color.
func blendHex(a, b string, t float64) string {
	va, errA := parseHex(a)
	vb, errB := parseHex(b)
	if errA != nil || errB != nil {
		return a
	}
	t = math.Max(0, math.Min(1, t))
	mix := func(shift uint) uint64 {
		ca := float64((va >> shift) & 0xff)
		cb := float64((vb >> shift) & 0xff)
		return uint64(math.Round(ca + (cb-ca)*t))
	}
	return fmt.Sprintf("#%06x", mix(16)<<16|mix(8)<<8|mix(0))
}

// parseHex parses a #RRGGBB string into a packed 24-bit value.
func parseHex(hex string) (uint64, error) {
	body := strings.TrimPrefix(hex, "#")
	if len(body) != 6 {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseUint(body, 16, 24)
}
