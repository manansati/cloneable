package receipt

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// saffron colour for receipt display — matches the main UI theme.
var (
	styleName    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00")).Bold(true)
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("#00E676"))
	styleDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("#3A3A3A"))
)

// PrintList renders all installed receipts in a clean table.
// Called by `cloneable list`.
func PrintList(receipts []*Receipt) {
	if len(receipts) == 0 {
		fmt.Println("\n  No installations found.")
		fmt.Println(styleMuted.Render("  Run: cloneable <git-url>  to install something.\n"))
		return
	}

	divider := styleDivider.Render(strings.Repeat("─", 68))

	fmt.Println()
	fmt.Printf("  %s  %s\n\n",
		styleName.Render("Cloneable Installations"),
		styleMuted.Render(fmt.Sprintf("(%d total)", len(receipts))),
	)
	fmt.Println("  " + divider)

	for _, r := range receipts {
		printReceiptRow(r)
		fmt.Println("  " + divider)
	}
	fmt.Println()
}

// printReceiptRow prints a single receipt as a formatted row.
func printReceiptRow(r *Receipt) {
	// Name + global badge
	nameLine := "  " + styleName.Render(r.Name)
	if r.GloballyInstalled {
		nameLine += "  " + styleSuccess.Render("● global")
	}
	fmt.Println(nameLine)

	// Tech + category
	fmt.Printf("  %s  %s\n",
		styleMuted.Render(r.Tech),
		styleMuted.Render(r.Category),
	)

	// Path
	fmt.Printf("  %s\n", styleMuted.Render(r.ClonePath))

	// Binary name if global
	if r.BinaryName != "" && r.GloballyInstalled {
		fmt.Printf("  binary: %s\n", styleMuted.Render(r.BinaryName))
	}

	// Installed date
	fmt.Printf("  installed %s\n", styleMuted.Render(humanTime(r.InstalledAt)))

	fmt.Println()
}

// PrintReceipt prints the full details of a single receipt.
// Called when removing to confirm what will be deleted.
func PrintReceipt(r *Receipt) {
	fmt.Printf("\n  %s\n", styleName.Render(r.Name))
	fmt.Printf("  URL:      %s\n", styleMuted.Render(r.URL))
	fmt.Printf("  Path:     %s\n", styleMuted.Render(r.ClonePath))
	fmt.Printf("  Tech:     %s\n", styleMuted.Render(r.Tech))
	fmt.Printf("  Category: %s\n", styleMuted.Render(r.Category))
	if r.GloballyInstalled && r.BinaryName != "" {
		fmt.Printf("  Binary:   %s\n", styleMuted.Render(r.BinaryName))
	}
	if len(r.Symlinks) > 0 {
		fmt.Printf("  Symlinks:\n")
		for _, sym := range r.Symlinks {
			fmt.Printf("    %s → %s\n",
				styleMuted.Render(sym.Source),
				styleMuted.Render(sym.Target),
			)
		}
	}
	if r.EnvDir != "" {
		fmt.Printf("  Env dir:  %s\n", styleMuted.Render(r.EnvDir))
	}
	fmt.Printf("  Installed: %s\n\n", styleMuted.Render(r.InstalledAt.Format("2006-01-02 15:04")))
}

// humanTime returns a human-friendly relative time string.
// e.g. "2 days ago", "just now", "3 months ago"
func humanTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/(24*7)))
	default:
		return t.Format("2006-01-02")
	}
}
