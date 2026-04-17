package ui

import (
	"fmt"
	"strings"
)

// asciiArt is the ANSI Shadow font Cloneable logo.
// Rendered in saffron via lipgloss.
const asciiArt = ` ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗
██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝
██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗  
██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝  
╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗
 ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝`

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

// PrintHeader clears the screen and renders the full Cloneable header:
//
//	[ASCII art in saffron]
//	[OS info bar]
//	[divider]
//	[repo name]
func PrintHeader(info HeaderInfo) {
	ClearScreen()

	// ASCII art — every line padded by 1 space, rendered in saffron
	for _, line := range strings.Split(asciiArt, "\n") {
		fmt.Println(StyleSaffron.Render(" " + line))
	}

	fmt.Println()

	// Info bar: Linux  │  Arch  │  pacman + yay  │  Node.js v20
	fmt.Println("  " + buildInfoBar(info))

	// Divider
	fmt.Println("  " + StyleDim.Render(strings.Repeat("─", 68)))

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
