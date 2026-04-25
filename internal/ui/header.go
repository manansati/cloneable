package ui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// AsciiArt is the ANSI Shadow font Cloneable logo.
// Rendered in saffron via lipgloss.
const AsciiArt = ` ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗
██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝
██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗  
██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝  
╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗
 ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝`

// CompactLogo is a smaller version for narrow terminals (Windows cmd, etc.)
const CompactLogo = `  ╔═╗┬  ┌─┐┌┐┌┌─┐┌─┐┌┐ ┬  ┌─┐
  ║  │  │ ││││├┤ ├─┤├┴┐│  ├┤ 
  ╚═╝┴─┘└─┘┘└┘└─┘┴ ┴└─┘┴─┘└─┘`

// HeaderInfo holds everything shown below the ASCII art banner.
type HeaderInfo struct {
	OS         string // e.g. "Linux", "macOS", "Windows"
	Distro     string // e.g. "Arch" — empty on macOS/Windows
	PkgManager string // e.g. "pacman + yay"
	Tech       string // e.g. "Node.js v20" — filled in after tech detection
	RepoName   string // e.g. "ghostty"
}

// ClearScreen wipes the terminal.
func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

// getTerminalWidth returns the current terminal width, defaulting to 80.
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

// PrintHeader clears the screen and renders the full Cloneable header:
//
//	[top margin]
//	[ASCII art in saffron]
//	[OS info bar]
//	[divider]
//	[repo name]
//
// Error 4: Added top margin to prevent logo being carved out at top.
// Error 5: Uses compact logo on narrow terminals (Windows cmd).
func PrintHeader(info HeaderInfo) {
	ClearScreen()

	// Error 4: Add top margin so logo is never clipped at the top
	fmt.Println()
	fmt.Println()

	termWidth := getTerminalWidth()

	// Error 5: On narrow terminals (< 85 cols, common on Windows cmd or small windows),
	// use the compact logo to prevent wrapping and visual corruption.
	if termWidth >= 85 {
		// Full ASCII art — every line padded by 1 space, rendered in saffron
		for _, line := range strings.Split(AsciiArt, "\n") {
			fmt.Println(StyleSaffron.Render(" " + line))
		}
	} else {
		// Compact logo for narrow terminals
		for _, line := range strings.Split(CompactLogo, "\n") {
			fmt.Println(StyleSaffron.Render(line))
		}
	}

	fmt.Println()

	// Info bar: Linux  │  Arch  │  pacman + yay  │  Node.js v20
	fmt.Println("  " + buildInfoBar(info))

	// Divider — fit to terminal width
	dividerLen := 68
	if termWidth < 72 {
		dividerLen = termWidth - 4
		if dividerLen < 20 {
			dividerLen = 20
		}
	}
	fmt.Println("  " + StyleDim.Render(strings.Repeat("─", dividerLen)))

	// Repo name (shown as soon as we know it)
	if info.RepoName != "" {
		fmt.Printf("  %s %s\n", Muted("repo:"), SaffronBold(info.RepoName))
	}

	fmt.Println()
}

// UpdateHeader re-renders the header with updated tech info.
// Called after tech detection completes so the tech field gets filled in.
func UpdateHeader(info HeaderInfo) {
	PrintHeader(info)
}

// buildInfoBar assembles the pipe-separated info bar string.
func buildInfoBar(info HeaderInfo) string {
	sep := "  " + StyleDim.Render(SymbolPipe) + "  "

	var parts []string
	if info.OS != "" {
		parts = append(parts, Bold(info.OS))
	}
	if info.Distro != "" {
		parts = append(parts, Muted(info.Distro))
	}
	if info.PkgManager != "" {
		parts = append(parts, Muted(info.PkgManager))
	}
	if info.Tech != "" {
		parts = append(parts, Saffron(info.Tech))
	}

	return strings.Join(parts, sep)
}
