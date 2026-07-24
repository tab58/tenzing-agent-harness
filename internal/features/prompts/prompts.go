package prompts

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed default_main.gotmpl
var defaultMainPromptString string

var defaultMainPromptTmpl = template.Must(template.New("default_system_prompt").Parse(defaultMainPromptString))

type systemPromptData struct{}

func DefaultSystemPrompt() string {
	var buf bytes.Buffer
	if err := defaultMainPromptTmpl.Execute(&buf, systemPromptData{}); err != nil {
		panic("default system prompt template: " + err.Error())
	}
	return buf.String()
}
