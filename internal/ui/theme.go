// Package ui provides all visual components for Cloneable's terminal interface.
// Built on Charm's lipgloss (styling) and bubbletea (interactive TUI) libraries.
package ui

import "github.com/charmbracelet/lipgloss"

// ── Color Palette ─────────────────────────────────────────────────────────────

const (
	ColorSaffron      = lipgloss.Color("#FF8C00") // Primary brand color
	ColorSaffronLight = lipgloss.Color("#FFB347") // Highlights
	ColorSaffronDark  = lipgloss.Color("#CC6600") // Borders / shadows

	ColorWhite    = lipgloss.Color("#FFFFFF")
	ColorOffWhite = lipgloss.Color("#F2F2F2")
	ColorGray     = lipgloss.Color("#888888")
	ColorDarkGray = lipgloss.Color("#3A3A3A")

	ColorGreen  = lipgloss.Color("#00E676") // Success / tick
	ColorRed    = lipgloss.Color("#FF5252") // Error / cross
	ColorYellow = lipgloss.Color("#FFD740") // Warning
	ColorBlue   = lipgloss.Color("#40C4FF") // Info
)

// ── Base Styles ───────────────────────────────────────────────────────────────

var (
	StyleSaffron     = lipgloss.NewStyle().Foreground(ColorSaffron)
	StyleSaffronBold = lipgloss.NewStyle().Foreground(ColorSaffron).Bold(true)
	StyleSuccess     = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleError       = lipgloss.NewStyle().Foreground(ColorRed)
	StyleWarning     = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleMuted       = lipgloss.NewStyle().Foreground(ColorGray)
	StyleBold        = lipgloss.NewStyle().Foreground(ColorOffWhite).Bold(true)
	StyleDim         = lipgloss.NewStyle().Foreground(ColorDarkGray)

	// StyleSelectedItem is used in the arrow-key selector for the focused row.
	StyleSelectedItem = lipgloss.NewStyle().
				Background(ColorSaffron).
				Foreground(ColorWhite).
				Bold(true).
				PaddingLeft(1).
				PaddingRight(1)

	// StyleNormalItem is used for non-focused rows in the selector.
	StyleNormalItem = lipgloss.NewStyle().
			Foreground(ColorOffWhite).
			PaddingLeft(2)

	// StyleDescription is used for the description line below each selector item.
	StyleDescription = lipgloss.NewStyle().
				Foreground(ColorGray).
				PaddingLeft(3)
)

// ── Symbols ───────────────────────────────────────────────────────────────────

const (
	SymbolTick  = "✓"
	SymbolCross = "✗"
	SymbolArrow = "›"
	SymbolPipe  = "│"
	SymbolDot   = "●"
)

// ── Helper functions ──────────────────────────────────────────────────────────

func Saffron(s string) string     { return StyleSaffron.Render(s) }
func SaffronBold(s string) string { return StyleSaffronBold.Render(s) }
func Tick() string                { return StyleSuccess.Render(SymbolTick) }
func Cross() string               { return StyleError.Render(SymbolCross) }
func Muted(s string) string       { return StyleMuted.Render(s) }
func Bold(s string) string        { return StyleBold.Render(s) }
func Success(s string) string     { return StyleSuccess.Render(s) }
func Err(s string) string         { return StyleError.Render(s) }
func Warn(s string) string        { return StyleWarning.Render(s) }
