package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ranscky/neuron/pkg/executor"
)

type Runner struct {
	executor *executor.Executor
}

func NewRunner(ex *executor.Executor) *Runner {
	return &Runner{
		executor: ex,
	}
}

// Execute runs the workflow steps in sequence
func (r *Runner) Execute(wf *Workflow, userInputs map[string]string) error {
	stepOutputs := make(map[string]string)

	for _, step := range wf.Steps {
		fmt.Printf("Executing step [%s]: %s...\n", step.ID, step.Package)

		// Interpolate inputs
		resolvedInputs := r.interpolateInputs(step.Input, userInputs, stepOutputs)

		// Convert resolved inputs to JSON string for Neuron packages (stdin)
		inputJSON, err := json.Marshal(resolvedInputs)
		if err != nil {
			return fmt.Errorf("failed to marshal inputs for step %s: %w", step.ID, err)
		}

		// Run the package
		output, err := r.executor.Execute(step.Package, []string{string(inputJSON)})
		if err != nil {
			return fmt.Errorf("step %s failed: %w", step.ID, err)
		}

		// Store output for future steps
		stepOutputs[step.ID] = output
		fmt.Printf("Step [%s] completed successfully.\n", step.ID)
	}

	// Print final output from the last step
	if len(wf.Steps) > 0 {
		lastStepID := wf.Steps[len(wf.Steps)-1].ID
		fmt.Printf("\nFinal Result:\n%s\n", stepOutputs[lastStepID])
	}

	return nil
}

// interpolateInputs replaces {{$.user_query}} and {{$.steps.step_id.output}}
func (r *Runner) interpolateInputs(inputs map[string]interface{}, userInputs map[string]string, stepOutputs map[string]string) map[string]interface{} {
	resolved := make(map[string]interface{})

	for k, v := range inputs {
		strVal, ok := v.(string)
		if !ok {
			resolved[k] = v
			continue
		}

		// Replace user inputs: {{$.field}}
		for userK, userV := range userInputs {
			placeholder := fmt.Sprintf("{{$.%s}}", userK)
			strVal = strings.ReplaceAll(strVal, placeholder, userV)
		}

		// Replace step outputs: {{$.steps.step_id.output}}
		for stepID, stepOut := range stepOutputs {
			placeholder := fmt.Sprintf("{{$.steps.%s.output}}", stepID)
			strVal = strings.ReplaceAll(strVal, placeholder, stepOut)
		}

		resolved[k] = strVal
	}

	return resolved
}
