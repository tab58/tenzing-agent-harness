package nexus

import (
	"fmt"
	"os"
	"regexp"

	yaml "go.yaml.in/yaml/v3"
)

const (
	TypeFileTail = "file-tail"
	TypeCommand  = "command"
	TypeWebhook  = "webhook"

	DefaultErrorPattern = `(?i)error|panic|fatal`
	DefaultBufferSize   = 1000
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ChannelConfig configures one input channel.
type ChannelConfig struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Path         string   `yaml:"path"`          // file-tail only
	Cmd          []string `yaml:"cmd"`           // command only
	ErrorPattern string   `yaml:"error_pattern"` // default DefaultErrorPattern
	BufferSize   int      `yaml:"buffer_size"`   // default DefaultBufferSize
	Trigger      *bool    `yaml:"trigger"`       // nil means true
}

// TriggerEnabled reports whether error lines on this channel wake the agent.
func (c ChannelConfig) TriggerEnabled() bool {
	return c.Trigger == nil || *c.Trigger
}

// Config is the root of nexus.yaml.
type Config struct {
	Channels []ChannelConfig `yaml:"channels"`
}

// LoadConfig reads and validates the YAML config at path. A missing file is
// not an error: it returns an empty config (zero channels). Defaults are
// applied to the returned config.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read nexus config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse nexus config: %w", err)
	}

	seen := make(map[string]bool, len(cfg.Channels))
	for i := range cfg.Channels {
		ch := &cfg.Channels[i]

		if !nameRe.MatchString(ch.Name) {
			return Config{}, fmt.Errorf("channel %d: name %q invalid (must match %s)", i, ch.Name, nameRe)
		}
		if seen[ch.Name] {
			return Config{}, fmt.Errorf("duplicate channel name %q", ch.Name)
		}
		seen[ch.Name] = true

		switch ch.Type {
		case TypeFileTail:
			if ch.Path == "" {
				return Config{}, fmt.Errorf("channel %q: file-tail requires path", ch.Name)
			}
		case TypeCommand:
			if len(ch.Cmd) == 0 {
				return Config{}, fmt.Errorf("channel %q: command requires cmd", ch.Name)
			}
		case TypeWebhook:
			// no extra fields
		default:
			return Config{}, fmt.Errorf("channel %q: unknown type %q", ch.Name, ch.Type)
		}

		if ch.ErrorPattern == "" {
			ch.ErrorPattern = DefaultErrorPattern
		}
		if _, err := regexp.Compile(ch.ErrorPattern); err != nil {
			return Config{}, fmt.Errorf("channel %q: error_pattern: %w", ch.Name, err)
		}

		if ch.BufferSize == 0 {
			ch.BufferSize = DefaultBufferSize
		}
		if ch.BufferSize < 0 {
			return Config{}, fmt.Errorf("channel %q: buffer_size must be > 0", ch.Name)
		}
	}
	return cfg, nil
}
