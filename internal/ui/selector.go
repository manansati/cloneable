package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// SelectorOption is a single item in the arrow-key list.
type SelectorOption struct {
	Label       string // Main text shown in the list
	Description string // Dimmed line shown below the label
	Value       string // Machine-readable value returned on selection
}

// SelectorResult is what RunSelector returns after the user picks an option.
type SelectorResult struct {
	Value  string // The Value field of the selected SelectorOption
	Label  string // The Label field of the selected SelectorOption
	Custom string // Non-empty only if the user chose "Custom argument"
}

// selectorModel is the internal bubbletea model for the selector.
type selectorModel struct {
	title   string
	options []SelectorOption
	cursor  int
	chosen  *SelectorResult
	quit    bool
}

func (m selectorModel) Init() tea.Cmd { return nil }

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quit = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}

		case "enter", " ":
			selected := m.options[m.cursor]
			m.chosen = &SelectorResult{
				Value: selected.Value,
				Label: selected.Label,
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	if m.chosen != nil || m.quit {
		return ""
	}

	out := fmt.Sprintf("\n  %s\n\n", SaffronBold(m.title))

	for i, opt := range m.options {
		if i == m.cursor {
			// Focused item: saffron background
			out += "  " + StyleSelectedItem.Render(
				fmt.Sprintf(" %s  %s ", SymbolArrow, opt.Label),
			) + "\n"
		} else {
			// Normal item
			out += "  " + StyleNormalItem.Render(opt.Label) + "\n"
		}

		// Description line (always dimmed, regardless of selection)
		if opt.Description != "" {
			out += StyleDescription.Render(opt.Description) + "\n"
		}
		out += "\n"
	}

	out += "\n  " + Muted("↑/↓ to navigate   enter to select   q to quit") + "\n"
	return out
}

// RunSelector displays an arrow-key navigable list and returns the user's pick.
// Returns nil if the user pressed q or ctrl+c.
func RunSelector(title string, options []SelectorOption) (*SelectorResult, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("no options provided")
	}

	model := selectorModel{
		title:   title,
		options: options,
	}

	p := tea.NewProgram(model)
	final, err := p.Run()
	if err != nil {
		return nil, err
	}

	m, ok := final.(selectorModel)
	if !ok || m.quit {
		return nil, nil
	}

	return m.chosen, nil
}

// ── Pre-built option sets ─────────────────────────────────────────────────────

// GlobalInstallOptions returns the standard install-scope options.
func GlobalInstallOptions(appName string) []SelectorOption {
	return []SelectorOption{
		{
			Label:       "Install globally",
			Description: fmt.Sprintf("Add %s to PATH — usable from anywhere, forever", appName),
			Value:       "global",
		},
		{
			Label:       "Use locally only",
			Description: "Run only from this directory — no PATH changes",
			Value:       "local",
		},
		{
			Label:       "Skip",
			Description: "Don't install, just run this once",
			Value:       "skip",
		},
	}
}
