package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/rlm"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/harness/skills"
	"tenzing-agent/internal/harness/todo"
	"tenzing-agent/internal/harness/tools"
	"tenzing-agent/internal/provider"
)

const (
	defaultSubAgentMaxDepth      = 2
	defaultSubAgentMaxIterations = 30
)

var _ AgentFactory = (*SubAgentFactory)(nil)

type SubAgentFactoryConfig struct {
	AgentLLM      provider.LLM
	RLMModel      provider.LLM
	AgentBuilder  runner.AgentBuilder
	MaxDepth      int
	MaxIterations int
	Cwd           string
	Emitter       events.Emitter
}

type SubAgentFactory struct {
	agentLLM      provider.LLM
	rlmModel      provider.LLM
	agentBuilder  runner.AgentBuilder
	maxDepth      int
	currentDepth  int
	maxIterations int
	cwd           string
	emitter       events.Emitter
}

func NewSubAgentFactory(cfg SubAgentFactoryConfig) *SubAgentFactory {
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultSubAgentMaxDepth
	}
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultSubAgentMaxIterations
	}
	rlmLLM := cfg.RLMModel
	if rlmLLM == nil {
		rlmLLM = cfg.AgentLLM
	}
	return &SubAgentFactory{
		agentLLM:      cfg.AgentLLM,
		rlmModel:      rlmLLM,
		agentBuilder:  cfg.AgentBuilder,
		maxDepth:      maxDepth,
		currentDepth:  0,
		maxIterations: maxIter,
		cwd:           cfg.Cwd,
		emitter:       cfg.Emitter,
	}
}

func (f *SubAgentFactory) SpawnAgent(ctx context.Context, task string, taskContext string) (string, error) {
	childDepth := f.currentDepth + 1
	slog.Info("[subagent] spawning", "depth", childDepth, "max_depth", f.maxDepth, "task_len", len(task))
	start := time.Now()

	registry := f.buildChildToolRegistry()

	systemPrompt := "You are a sub-agent. Complete the assigned task using your tools. " +
		"Be thorough but concise in your final answer — it will be returned to the orchestrating agent."

	childAgent, err := f.agentBuilder(f.agentLLM, systemPrompt)
	if err != nil {
		return "", fmt.Errorf("create child agent: %w", err)
	}

	todoFile := todo.NewTodoItemFile(f.cwd)
	skillsReg := skills.NewRegistry()

	childRunner, err := runner.NewAgentRunner(runner.AgentRunnerConfig{
		Agent:          childAgent,
		ToolRegistry:   registry,
		SystemPrompt:   systemPrompt,
		TodoFile:       todoFile,
		SkillsRegistry: skillsReg,
	})
	if err != nil {
		return "", fmt.Errorf("create child runner: %w", err)
	}

	if f.emitter != nil {
		f.emitter.Emit(events.SubagentStartedEvent{
			BaseEvent: events.NewBaseEvent(events.EventSubagentStarted, childRunner.ID()),
			AgentID:   childRunner.ID(),
			Prompt:    task,
		})
	}

	input := task
	if taskContext != "" {
		input = task + "\n\nContext:\n" + taskContext
	}

	result, err := childRunner.RunLoop(ctx, input)
	duration := time.Since(start)
	if err != nil {
		slog.Error("[subagent] failed", "depth", childDepth, "duration", duration.Round(time.Millisecond), "error", err)
		if f.emitter != nil {
			f.emitter.Emit(events.SubagentStoppedEvent{
				BaseEvent: events.NewBaseEvent(events.EventSubagentStopped, childRunner.ID()),
				AgentID:   childRunner.ID(),
				Duration:  duration.Round(time.Millisecond),
			})
		}
		return "", err
	}

	slog.Info("[subagent] completed", "depth", childDepth, "duration", duration.Round(time.Millisecond), "answer_len", len(result))
	if f.emitter != nil {
		f.emitter.Emit(events.SubagentStoppedEvent{
			BaseEvent: events.NewBaseEvent(events.EventSubagentStopped, childRunner.ID()),
			AgentID:   childRunner.ID(),
			Duration:  duration.Round(time.Millisecond),
		})
	}
	return result, nil
}

func (f *SubAgentFactory) buildChildToolRegistry() *tools.Registry {
	childDepth := f.currentDepth + 1
	registry := tools.NewRegistry()

	rlmEngine, err := rlm.NewEngine(rlm.EngineConfig{
		NewFetcher: rlm.NewLLMFetcherFactory(f.agentLLM),
		Querier:    rlm.NewLLMQuerier(f.rlmModel),
		MaxDepth:   1,
		WorkingDir: f.cwd,
	})
	if err == nil {
		registry.RegisterFromProvider(rlmEngine)
	}

	if childDepth < f.maxDepth {
		childFactory := &SubAgentFactory{
			agentLLM:      f.agentLLM,
			rlmModel:      f.rlmModel,
			agentBuilder:  f.agentBuilder,
			maxDepth:      f.maxDepth,
			currentDepth:  childDepth,
			maxIterations: f.maxIterations,
			cwd:           f.cwd,
			emitter:       f.emitter,
		}
		registry.Register(NewSpawnAgentTool(childFactory))
	}

	return registry
}
