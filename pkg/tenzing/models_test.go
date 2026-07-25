package tenzing

import (
	"strings"
	"testing"
)

func TestStandardModelsNonEmptyUniqueKeys(t *testing.T) {
	defs := StandardModels()
	if len(defs) == 0 {
		t.Fatal("StandardModels() is empty")
	}
	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		key := strings.ToLower(string(d.Provider) + "/" + d.Name)
		if d.Provider == "" || d.Name == "" {
			t.Errorf("model %+v has empty provider or name", d)
		}
		if seen[key] {
			t.Errorf("duplicate provider/name key %q", key)
		}
		seen[key] = true
	}
}
