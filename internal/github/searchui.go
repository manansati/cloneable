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

	styleSelectedRow  = lipgloss.NewStyle().Background(colorSaffron).Foreground(lipgloss.Color("#000000")).Bold(true)
	styleNormalName   = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleDesc         = lipgloss.NewStyle().Foreground(colorGray)
	styleMeta         = lipgloss.NewStyle().Foreground(colorGray)
	styleStars        = lipgloss.NewStyle().Foreground(colorSaffron)
	styleLang         = lipgloss.NewStyle().Foreground(colorGreen)
	styleDivider      = lipgloss.NewStyle().Foreground(colorDark)
	styleHeader       = lipgloss.NewStyle().Foreground(colorSaffron).Bold(true)
	styleHint         = lipgloss.NewStyle().Foreground(colorDark)
)

// ── Model ─────────────────────────────────────────────────────────────────────

type SearchModel struct {
	query      string
	results    []SearchResult
	totalCount int
	cursor     int
	chosen     *SearchResult
	cancelled  bool
	height     int // terminal height for viewport clipping
}

func NewSearchModel(query string, results []SearchResult, total int) SearchModel {
	return SearchModel{
		query:      query,
		results:    results,
		totalCount: total,
		cursor:     0, // always start at top
	}
}

func (m SearchModel) Init() tea.Cmd { return nil }

func (m SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height

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

	// Header line
	b.WriteString("\n  ")
	b.WriteString(styleHeader.Render("Results for: "))
	b.WriteString(lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render(`"` + m.query + `"`))
	b.WriteString(styleMeta.Render(fmt.Sprintf("  (%s repos)", FormatStars(m.totalCount))))
	b.WriteString("\n\n")

	divider := "  " + styleDivider.Render(strings.Repeat("─", 66))
	b.WriteString(divider + "\n")

	for i, r := range m.results {
		selected := i == m.cursor

		var row string
		if selected {
			// Full row highlight — arrow + name + meta all highlighted
			name := fmt.Sprintf(" › %s", r.FullName)
			meta := buildMeta(r)
			if meta != "" {
				row = styleSelectedRow.Render(fmt.Sprintf("  %-40s %s  ", name, meta))
			} else {
				row = styleSelectedRow.Render(fmt.Sprintf("  %-40s  ", name))
			}
			b.WriteString(row + "\n")
		} else {
			b.WriteString("    " + styleNormalName.Render(r.FullName))
			meta := buildMeta(r)
			if meta != "" {
				b.WriteString("  " + meta)
			}
			b.WriteString("\n")
		}

		if r.Description != "" {
			b.WriteString("    " + styleDesc.Render(TruncateDesc(r.Description, 70)) + "\n")
		}

		b.WriteString(divider + "\n")
	}

	b.WriteString("\n  ")
	b.WriteString(styleHint.Render("↑/↓  navigate    enter  select    q  quit"))
	b.WriteString("\n\n")

	return b.String()
}

func buildMeta(r SearchResult) string {
	var parts []string
	if r.Stars > 0 {
		parts = append(parts, styleStars.Render("★ "+FormatStars(r.Stars)))
	}
	if r.Language != "" {
		parts = append(parts, styleLang.Render(r.Language))
	}
	updated := FormatUpdated(r.UpdatedAt)
	if updated != "" {
		parts = append(parts, styleMeta.Render(updated))
	}
	return strings.Join(parts, "  ")
}

// RunSearchUI shows the search results in an alt-screen so it always starts
// at the top of the terminal, not pushed to the bottom.
func RunSearchUI(query string, results []SearchResult, total int) (*SearchResult, error) {
	if len(results) == 0 {
		return nil, nil
	}

	model := NewSearchModel(query, results, total)

	// WithAltScreen puts the UI in a full-screen mode — cursor always at top
	p := tea.NewProgram(model, tea.WithAltScreen())

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
