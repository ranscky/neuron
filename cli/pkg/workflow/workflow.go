package workflow

import (
	"encoding/json"
	"fmt"
	"os"
)

type Step struct {
	ID      string                 `json:"id"`
	Package string                 `json:"package"`
	Input   map[string]interface{} `json:"input"`
}

type Workflow struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
}

// ParseWorkflow reads a workflow.json file and parses it into a Workflow struct
func ParseWorkflow(path string) (*Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	var wf Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("failed to parse workflow JSON: %w", err)
	}

	return &wf, nil
}
