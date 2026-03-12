package prompts

import (
	_ "embed"
)

//go:embed system.md
var DefaultSystemPrompt string
