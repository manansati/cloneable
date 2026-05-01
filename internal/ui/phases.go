package ui

import "fmt"

// PhaseState is the current status of a workflow phase.
type PhaseState int

const (
	StatePending PhaseState = iota // Not yet started — shown dimmed
	StateRunning                   // In progress — animated spinner on left
	StateDone                      // Completed — green tick on left
	StateFailed                    // Failed — red cross on left
)

// Phase represents a single named step in the Cloneable workflow.
type Phase struct {
	Name  string
	State PhaseState
}

// PhaseRunner manages the three main phases of Cloneable and renders them
// cleanly below the header. Only one phase runs at a time.
type PhaseRunner struct {
	phases []Phase
}

// NewPhaseRunner returns a runner with all three standard phases in Pending state.
func NewPhaseRunner() *PhaseRunner {
	return &PhaseRunner{
		phases: []Phase{
			{Name: "Cloning"},
			{Name: "Installing dependencies"},
			{Name: "Finishing up"},
		},
	}
}

// Run executes a task for the given phase index, showing a spinner while it
// runs and a tick/cross when it finishes. It prints the result to stdout
// and updates the phase state in the runner.
//
// Example:
//
//	runner := ui.NewPhaseRunner()
//	runner.Run(0, func() error { return cloneRepo(url) })
//	runner.Run(1, func() error { return installDeps() })
//	runner.Run(2, func() error { return launch() })
func (r *PhaseRunner) Run(index int, task func() error) error {
	if index < 0 || index >= len(r.phases) {
		return fmt.Errorf("invalid phase index: %d", index)
	}

	r.phases[index].State = StateRunning

	err := RunWithSpinner(r.phases[index].Name, task)
	if err != nil {
		r.phases[index].State = StateFailed
		return err
	}

	r.phases[index].State = StateDone
	return nil
}

// PrintSummary prints a final summary of all phases after the workflow ends.
func (r *PhaseRunner) PrintSummary() {
	fmt.Println()
	for _, phase := range r.phases {
		switch phase.State {
		case StateDone:
			fmt.Printf("  %s  %s\n", Tick(), Bold(phase.Name))
		case StateFailed:
			fmt.Printf("  %s  %s\n", Cross(), Err(phase.Name))
		case StatePending:
			fmt.Printf("  %s  %s\n", Muted("○"), Muted(phase.Name))
		}
	}
	fmt.Println()
}
