package github

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/manansati/cloneable/internal/ui"
)

var (
	colorSaffron = lipgloss.Color("#FF8C00")
	colorGray    = lipgloss.Color("#888888")
	colorWhite   = lipgloss.Color("#F2F2F2")
	colorDark    = lipgloss.Color("#3A3A3A")
	colorGreen   = lipgloss.Color("#00E676")

	styleNormalName   = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	styleDesc         = lipgloss.NewStyle().Foreground(colorGray)
	styleMeta         = lipgloss.NewStyle().Foreground(colorGray)
	styleStars        = lipgloss.NewStyle().Foreground(colorSaffron)
	styleLang         = lipgloss.NewStyle().Foreground(colorGreen)
	styleDivider      = lipgloss.NewStyle().Foreground(colorDark)
	styleHint         = lipgloss.NewStyle().Foreground(colorDark)
	styleSearchPrompt = lipgloss.NewStyle().Foreground(colorSaffron).Bold(true)
	

)

const itemsPerPage = 10
const maxTotalItems = 30 // Increased slightly to 30 (3 pages of 10)

type SearchModel struct {
	query      string
	results    []SearchResult
	totalCount int
	cursor     int
	chosen     *SearchResult
	cancelled  bool
	height     int
	width      int
	page       int

	textInput textinput.Model
	typing    bool
	loading   bool
	fetching  bool 
	apiPage   int  
	err       error
}

func NewSearchModel(query string, results []SearchResult, total int) SearchModel {
	ti := textinput.New()
	ti.Placeholder = "Type repository name..."
	ti.CharLimit = 156
	ti.Width = 40
	
	// Make text white and placeholder grey
	ti.TextStyle = lipgloss.NewStyle().Foreground(colorWhite)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorGray)

	if len(results) > maxTotalItems {
		results = results[:maxTotalItems]
	}

	return SearchModel{
		query:      query,
		results:    results,
		totalCount: total,
		textInput:  ti,
		apiPage:    1,
	}
}

func (m SearchModel) Init() tea.Cmd {
	return textinput.Blink
}

type searchMsg struct {
	query   string
	results []SearchResult
	total   int
	err     error
}

type loadMoreMsg struct {
	results []SearchResult
	err     error
}

func doSearchCmd(query string, page int) tea.Cmd {
	return func() tea.Msg {
		results, total, err := SearchRepos(query, page)
		return searchMsg{query, results, total, err}
	}
}

func (m SearchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width

	case searchMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.results = msg.results
		if len(m.results) > maxTotalItems {
			m.results = m.results[:maxTotalItems]
		}
		m.totalCount = msg.total
		m.query = msg.query
		m.cursor = 0
		m.page = 0
		m.apiPage = 1
		return m, nil

	case tea.KeyMsg:
		if m.typing {
			switch msg.String() {
			case "enter":
				if m.textInput.Value() != "" {
					m.typing = false
					m.loading = true
					return m, doSearchCmd(m.textInput.Value(), 1)
				}
				m.typing = false
			case "esc":
				m.typing = false
				m.textInput.SetValue("")
			default:
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		} else {
			switch msg.String() {
			case "ctrl+c", "q", "esc":
				m.cancelled = true
				return m, tea.Quit

			case "/":
				m.typing = true
				m.textInput.SetValue("")
				m.textInput.Focus()
				return m, textinput.Blink

			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}

			case "down", "j":
				itemsOnPage := len(m.results) - (m.page * itemsPerPage)
				if itemsOnPage > itemsPerPage {
					itemsOnPage = itemsPerPage
				}
				if m.cursor < itemsOnPage-1 {
					m.cursor++
				}

			case "left", "h":
				if m.page > 0 {
					m.page--
					m.cursor = 0
				}

			case "right", "l":
				if (m.page+1)*itemsPerPage < len(m.results) {
					m.page++
					m.cursor = 0
				}

			case "enter", " ":
				idx := m.page*itemsPerPage + m.cursor
				if idx < len(m.results) {
					r := m.results[idx]
					m.chosen = &r
				}
				return m, tea.Quit
			}
		}
	}

	if m.typing {
		m.textInput, cmd = m.textInput.Update(msg)
	}

	return m, cmd
}

func (m SearchModel) View() string {
	if m.chosen != nil || m.cancelled {
		return ""
	}

	var b strings.Builder

	// Ascii Art
	for _, line := range strings.Split(ui.AsciiArt, "\n") {
		b.WriteString(ui.StyleSaffron.Render(" " + line) + "\n")
	}
	b.WriteString("\n")

	// Search Box
	dividerLen := m.width - 4
	if dividerLen < 66 {
		dividerLen = 66
	}

	if m.typing {
		b.WriteString(fmt.Sprintf("  %s %s\n\n", styleSearchPrompt.Render("Search GitHub:"), m.textInput.View()))
	} else {
		b.WriteString(fmt.Sprintf("  %s\n\n", lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render("Press / to search GitHub")))
	}

	// Status / Header
	if m.loading {
		b.WriteString("  Fetching results...\n")
		return b.String()
	}

	if m.err != nil {
		b.WriteString(fmt.Sprintf("  Error: %s\n", m.err.Error()))
		return b.String()
	}

	desc := "Search Results"
	isExplore := strings.HasPrefix(m.query, "created:>")
	if isExplore {
		desc = "Trending repositories - last 30 days"
	} else if m.query != "" {
		desc = "Results for: " + m.query
	}

	b.WriteString("  ")
	b.WriteString(styleSearchPrompt.Render(desc))
	
	if m.totalCount > 0 {
		b.WriteString(styleMeta.Render(fmt.Sprintf("  (showing %d of %s repos)", len(m.results), FormatStars(m.totalCount))))
	}
	b.WriteString("\n\n")

	// Divider
	divider := "  " + styleDivider.Render(strings.Repeat("─", dividerLen))
	b.WriteString(divider + "\n")

	if len(m.results) == 0 {
		b.WriteString("  No repositories found.\n")
		return b.String()
	}

	// Page logic
	start := m.page * itemsPerPage
	end := start + itemsPerPage
	if end > len(m.results) {
		end = len(m.results)
	}

	for i := start; i < end; i++ {
		r := m.results[i]
		selected := (i - start) == m.cursor

		name := fmt.Sprintf(" %s", r.FullName)
		if selected {
			name = fmt.Sprintf(" › %s", r.FullName)
		}

		descStr := r.Description
		if descStr != "" {
			descStr = TruncateDesc(descStr, dividerLen-4) 
		}

		if selected {
			topLine, botLine := buildSelectedRow(name, getMetaParts(r), descStr, dividerLen)
			b.WriteString(topLine + "\n")
			if botLine != "" {
				b.WriteString(botLine + "\n")
			}
		} else {
			metaStr := buildMetaNormal(r)
			padLen := dividerLen - lipgloss.Width(name) - lipgloss.Width(metaStr)
			if padLen < 1 {
				padLen = 1
			}
			b.WriteString("  " + styleNormalName.Render(name) + strings.Repeat(" ", padLen) + metaStr + "\n")
			if descStr != "" {
				b.WriteString("    " + styleDesc.Render(descStr) + "\n")
			}
		}

		b.WriteString(divider + "\n")
	}

	// Pagination & Hints
	b.WriteString("\n  ")
	totalPages := (len(m.results) + itemsPerPage - 1) / itemsPerPage
	if totalPages == 0 {
		totalPages = 1
	}
	b.WriteString(styleHint.Render(fmt.Sprintf("Page %d of %d   ", m.page+1, totalPages)))
	b.WriteString(styleHint.Render("↑/↓: navigate   ←/→: page   /: search   enter: select   q: quit"))
	b.WriteString("\n\n")

	return b.String()
}

func getMetaParts(r SearchResult) []string {
	var parts []string
	if r.Stars > 0 {
		parts = append(parts, "★ "+FormatStars(r.Stars))
	}
	if r.Language != "" {
		parts = append(parts, r.Language)
	}
	updated := FormatUpdated(r.UpdatedAt)
	if updated != "" {
		parts = append(parts, updated)
	}
	return parts
}

func buildMetaNormal(r SearchResult) string {
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

func buildSelectedRow(name string, metaParts []string, desc string, rowWidth int) (string, string) {
	bg := lipgloss.NewStyle().Background(colorSaffron)
	fgBlack := bg.Copy().Foreground(lipgloss.Color("#000000")).Bold(true)
	fgWhite := bg.Copy().Foreground(colorWhite)

	metaStr := ""
	if len(metaParts) > 0 {
		for i, part := range metaParts {
			metaStr += fgWhite.Render(part)
			if i < len(metaParts)-1 {
				metaStr += bg.Render("  ")
			}
		}
	}

	nameStr := fgBlack.Render(name)

	nameWidth := lipgloss.Width(nameStr)
	metaWidth := lipgloss.Width(metaStr)

	padLen := rowWidth - nameWidth - metaWidth
	if padLen < 1 {
		padLen = 1
	}

	topLine := nameStr + bg.Render(strings.Repeat(" ", padLen)) + metaStr

	var botLine string
	if desc != "" {
		descWidth := lipgloss.Width("  " + desc)
		descPad := rowWidth - descWidth
		if descPad < 0 {
			descPad = 0
		}
		botLine = fgBlack.Render("  " + desc) + bg.Render(strings.Repeat(" ", descPad))
	}

	return "  " + topLine, "  " + botLine
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
