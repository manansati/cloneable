package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// confirmModel is the bubbletea model for a yes/no prompt.
type confirmModel struct {
	prompt   string
	answer   bool
	answered bool
	quit     bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.answer = true
			m.answered = true
			return m, tea.Quit
		case "n", "N", "enter":
			m.answer = false
			m.answered = true
			return m, tea.Quit
		case "ctrl+c", "q":
			m.quit = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.answered || m.quit {
		return ""
	}
	return fmt.Sprintf("\n  %s %s  %s\n\n",
		Saffron("?"),
		Bold(m.prompt),
		Muted("[y/N]"),
	)
}

// Confirm displays a yes/no prompt and returns the user's answer.
// Pressing enter or n = false. Pressing y = true.
func Confirm(prompt string) (bool, error) {
	model := confirmModel{prompt: prompt}
	p := tea.NewProgram(model)

	final, err := p.Run()
	if err != nil {
		return false, err
	}

	m, ok := final.(confirmModel)
	if !ok || m.quit {
		return false, nil
	}

	return m.answer, nil
}
