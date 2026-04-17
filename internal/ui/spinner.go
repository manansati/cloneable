package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea  "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// spinnerStyle is the saffron-colored spinner.
var spinnerStyle = lipgloss.NewStyle().Foreground(ColorSaffron)

// SpinnerModel is a bubbletea model for showing a spinner next to a label.
// Usage:
//
//	s := ui.NewSpinner("Cloning")
//	p := tea.NewProgram(s)
//	go func() { p.Run() }()
//	// ... do work ...
//	p.Send(ui.SpinnerDoneMsg{})
type SpinnerModel struct {
	spinner  spinner.Model
	label    string
	done     bool
	failed   bool
	quitting bool
}

// SpinnerDoneMsg signals the spinner to show a tick and exit.
type SpinnerDoneMsg struct{}

// SpinnerFailMsg signals the spinner to show a cross and exit.
type SpinnerFailMsg struct{}

// NewSpinner creates a new spinner model with the given label.
func NewSpinner(label string) SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return SpinnerModel{
		spinner: s,
		label:   label,
	}
}

func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case SpinnerDoneMsg:
		m.done = true
		m.quitting = true
		return m, tea.Quit
	case SpinnerFailMsg:
		m.failed = true
		m.quitting = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m SpinnerModel) View() string {
	if m.done {
		return fmt.Sprintf("  %s  %s\n", Tick(), Bold(m.label))
	}
	if m.failed {
		return fmt.Sprintf("  %s  %s\n", Cross(), Err(m.label))
	}
	return fmt.Sprintf("  %s  %s\n", m.spinner.View(), Muted(m.label+"..."))
}

// ── Simple blocking spinner ────────────────────────────────────────────────────
// RunWithSpinner runs a task function while showing an animated spinner.
// When the task completes, the spinner is replaced by a green tick.
// If the task returns an error, a red cross is shown instead.
//
// This is the main API other packages use — they don't need to manage
// bubbletea programs directly.
//
// Example:
//
//	err := ui.RunWithSpinner("Cloning", func() error {
//	    return cloneRepo(url)
//	})
func RunWithSpinner(label string, task func() error) error {
	model := NewSpinner(label)
	p := tea.NewProgram(model)

	// Run the task in a goroutine while the spinner renders.
	var taskErr error
	go func() {
		// Small delay so the spinner renders at least one frame before task starts.
		time.Sleep(50 * time.Millisecond)
		taskErr = task()
		if taskErr != nil {
			p.Send(SpinnerFailMsg{})
		} else {
			p.Send(SpinnerDoneMsg{})
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}

	return taskErr
}
