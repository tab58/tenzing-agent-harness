package prompts

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed default_main.gotmpl
var defaultMainPromptString string

var defaultMainPromptTmpl = template.Must(template.New("default_system_prompt").Parse(defaultMainPromptString))

//go:embed default_subagent.gotmpl
var defaultSubagentPromptString string

var defaultSubagentPromptTmpl = template.Must(template.New("subagent_system_prompt").Parse(defaultSubagentPromptString))

type systemPromptData struct {
	Cwd string
}

func DefaultSystemPrompt(cwd string) string {
	var buf bytes.Buffer
	if err := defaultMainPromptTmpl.Execute(&buf, systemPromptData{Cwd: cwd}); err != nil {
		panic("default system prompt template: " + err.Error())
	}
	return buf.String()
}

func SubagentSystemPrompt(cwd string) string {
	var buf bytes.Buffer
	if err := defaultSubagentPromptTmpl.Execute(&buf, systemPromptData{Cwd: cwd}); err != nil {
		panic("subagent system prompt template: " + err.Error())
	}
	return buf.String()
}

//go:embed rlm_guidance.gotmpl
var rlmGuidanceString string

func RLMGuidance() string {
	return rlmGuidanceString
}
