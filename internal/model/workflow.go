package model

// WorkflowDefinition is the parsed WORKFLOW.md payload containing config and prompt template.
type WorkflowDefinition struct {
	Config         map[string]any `json:"config"`
	PromptTemplate string         `json:"prompt_template"`
}
