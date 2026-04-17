package github

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	colorSaffron = lipgloss.Color("#FF8C00")
	colorGray    = lipgloss.Color("#888888")
	colorWhite   = lipgloss.Color("#F2F2F2")
	colorDark    = lipgloss.Color("#3A3A3A")
	colorGreen   = lipgloss.Color("#00E676")

	styleSelectedBg   = lipgloss.NewStyle().Background(colorSaffron).Foreground(lipgloss.Color("#000000")).Bold(true)
	styleSelectedName = lipgloss.NewStyle().Background(colorSaffron).Foreground(lipgloss.Color("#000000")).Bold(true).PaddingLeft(1)
	styleNormalName   = lipgloss.NewStyle().Foreground(colorWhite).Bold(true).PaddingLeft(2)
	styleDesc         = lipgloss.NewStyle().Foreground(colorGray).PaddingLeft(2)
	styleMeta         = lipgloss.NewStyle().Foreground(colorGray)
	styleStars        = lipgloss.NewStyle().Foreground(colorSaffron)
	styleLang         = lipgloss.NewStyle().Foreground(colorGreen)
	styleDivider      = lipgloss.NewStyle().Foreground(colorDark)
	styleHeader       = lipgloss.NewStyle().Foreground(colorSaffron).Bold(true)
	styleHint         = lipgloss.NewStyle().Foreground(colorDark)
)

// ── Model ─────────────────────────────────────────────────────────────────────

// SearchModel is the bubbletea model for the search results screen.
type SearchModel struct {
	query      string
	results    []SearchResult
	totalCount int
	cursor     int
	chosen     *SearchResult
	cancelled  bool
}

// NewSearchModel creates a search model ready to render.
func NewSearchModel(query string, results []SearchResult, total int) SearchModel {
	return SearchModel{
		query:      query,
		results:    results,
		totalCount: total,
	}
}

func (m SearchModel) Init() tea.Cmd { return nil }

func (m SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}

		case "enter", " ":
			if len(m.results) > 0 {
				r := m.results[m.cursor]
				m.chosen = &r
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m SearchModel) View() string {
	if m.chosen != nil || m.cancelled {
		return ""
	}

	var b strings.Builder

	// Header
	b.WriteString("\n  ")
	b.WriteString(styleHeader.Render("Search results for: "))
	b.WriteString(lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render(`"` + m.query + `"`))
	b.WriteString(styleMeta.Render(fmt.Sprintf("  (%s total)", FormatStars(m.totalCount))))
	b.WriteString("\n\n")

	divider := "  " + styleDivider.Render(strings.Repeat("─", 66))
	b.WriteString(divider + "\n")

	// Results
	for i, r := range m.results {
		selected := i == m.cursor

		if selected {
			// Highlighted row
			b.WriteString("  " + styleSelectedBg.Render(" › ") + " ")
			b.WriteString(styleSelectedName.Render(r.FullName))
		} else {
			b.WriteString("    ")
			b.WriteString(styleNormalName.Render(r.FullName))
		}

		// Stars + language + updated
		meta := ""
		if r.Stars > 0 {
			meta += styleStars.Render("★ "+FormatStars(r.Stars))
		}
		if r.Language != "" {
			if meta != "" {
				meta += styleMeta.Render("  ")
			}
			meta += styleLang.Render(r.Language)
		}
		updated := FormatUpdated(r.UpdatedAt)
		if updated != "" {
			if meta != "" {
				meta += styleMeta.Render("  ")
			}
			meta += styleMeta.Render(updated)
		}
		if meta != "" {
			b.WriteString("  " + meta)
		}
		b.WriteString("\n")

		// Description
		if r.Description != "" {
			b.WriteString(styleDesc.Render(TruncateDesc(r.Description, 72)))
			b.WriteString("\n")
		}

		b.WriteString(divider + "\n")
	}

	// Key hints
	b.WriteString("\n  ")
	b.WriteString(styleHint.Render("↑/↓  navigate    enter  clone + install + launch    q  quit"))
	b.WriteString("\n\n")

	return b.String()
}

// RunSearchUI shows the arrow-key search results list.
// Returns the selected result, or nil if cancelled.
func RunSearchUI(query string, results []SearchResult, total int) (*SearchResult, error) {
	if len(results) == 0 {
		return nil, nil
	}

	model := NewSearchModel(query, results, total)
	p := tea.NewProgram(model)

	final, err := p.Run()
	if err != nil {
		return nil, err
	}

	m, ok := final.(SearchModel)
	if !ok || m.cancelled {
		return nil, nil
	}

	return m.chosen, nil
}
