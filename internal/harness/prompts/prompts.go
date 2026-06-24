package prompts

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed default_main.gotmpl
var defaultMainPromptString string

var defaultMainPromptTmpl = template.Must(template.New("default_system_prompt").Parse(defaultMainPromptString))

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

//go:embed rlm_guidance.gotmpl
var rlmGuidanceString string

func RLMGuidance() string {
	return rlmGuidanceString
}
