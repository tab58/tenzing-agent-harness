package harness

import "tenzing-agent/internal/tools"

type Harness struct {
	agent    Agent
	registry *tools.Registry
}

func New(agent Agent, registry *tools.Registry) (*Harness, error) {
	return &Harness{
		agent:    agent,
		registry: registry,
	}, nil
}
