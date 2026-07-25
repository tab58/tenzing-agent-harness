package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tab58/llm-providers/common"

	"github.com/tab58/tenzing-agent-harness/pkg/tenzing"
)

// knownModels are the standard models from tenzing.StandardModels, keyed by
// modelKey.
var knownModels = func() map[string]common.ModelDefinition {
	defs := tenzing.StandardModels()
	m := make(map[string]common.ModelDefinition, len(defs))
	for _, d := range defs {
		m[modelKey(d)] = d
	}
	return m
}()

// modelKey is the canonical CLI name for a model: "provider/name", lowercase.
func modelKey(def common.ModelDefinition) string {
	return strings.ToLower(string(def.Provider) + "/" + def.Name)
}

// resolveModel maps a "provider/name" flag value to a known ModelDefinition.
func resolveModel(s string) (common.ModelDefinition, error) {
	key := strings.ToLower(strings.TrimSpace(s))
	if def, ok := knownModels[key]; ok {
		return def, nil
	}
	return common.ModelDefinition{}, fmt.Errorf("unknown model %q; valid models:\n%s", s, modelList())
}

// modelList returns the known models, one per line, sorted.
func modelList() string {
	keys := make([]string, 0, len(knownModels))
	for k := range knownModels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		d := knownModels[k]
		fmt.Fprintf(&b, "%-40s ctx=%-8d max_tokens=%d\n", k, d.ContextWindowSize, d.MaxTokens)
	}
	return b.String()
}
