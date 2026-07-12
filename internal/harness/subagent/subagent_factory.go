package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/harness/blackboard"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
	"github.com/tab58/tenzing-agent-harness/internal/harness/skills"
	"github.com/tab58/tenzing-agent-harness/internal/harness/todo"
	"github.com/tab58/tenzing-agent-harness/internal/harness/tools"

	"github.com/tab58/llm-providers/common"
)

const (
	defaultSubAgentMaxDepth      = 2
	defaultSubAgentMaxIterations = 30
)

// inlineResultMax is the largest subagent result returned verbatim; longer
// results are deposited on the blackboard and replaced with a preview.
const inlineResultMax = 2000

var _ AgentFactory = (*SubAgentFactory)(nil)

type SubAgentFactoryConfig struct {
	AgentLLM      common.LLM
	AgentBuilder  runner.AgentBuilder
	MaxDepth      int
	MaxIterations int
	Cwd           string
	Emitter       events.Emitter
	Blackboard    *blackboard.Blackboard
	// ParentID is the spawning runner's ID; child IDs chain from it
	// ("438314ea" -> "438314ea_085c6444"). Empty means no prefix.
	ParentID string
}

type SubAgentFactory struct {
	agentLLM      common.LLM
	agentBuilder  runner.AgentBuilder
	maxDepth      int
	currentDepth  int
	maxIterations int
	cwd           string
	emitter       events.Emitter
	blackboard    *blackboard.Blackboard
	parentID      string
}

// childID derives a hierarchical agent ID from the parent's:
// "<parent>_<hex>". It is at once the child's runner ID (so every event it
// emits carries it), its event label, and its blackboard slot (underscores
// satisfy the slot-name rules).
func (f *SubAgentFactory) childID() string {
	id := runner.NewID()
	if f.parentID == "" {
		return id
	}
	return f.parentID + "_" + id
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
	return &SubAgentFactory{
		agentLLM:      cfg.AgentLLM,
		agentBuilder:  cfg.AgentBuilder,
		maxDepth:      maxDepth,
		currentDepth:  0,
		maxIterations: maxIter,
		cwd:           cfg.Cwd,
		emitter:       cfg.Emitter,
		blackboard:    cfg.Blackboard,
		parentID:      cfg.ParentID,
	}
}

func (f *SubAgentFactory) SpawnAgent(ctx context.Context, task string, taskContext string) (string, error) {
	childDepth := f.currentDepth + 1
	slog.Info("[subagent] spawning", "depth", childDepth, "max_depth", f.maxDepth, "task_len", len(task))
	start := time.Now()
	agentID := f.childID()

	registry := f.buildChildToolRegistry(agentID)

	systemPrompt := "You are a sub-agent. Complete the assigned task using your tools. " +
		"Be thorough but concise in your final answer — it will be returned to the orchestrating agent."
	if f.cwd != "" {
		systemPrompt += " The project working directory is " + f.cwd + " — relative paths resolve there; do not guess other locations."
	}
	if f.blackboard != nil {
		systemPrompt += " You share a persistent Python REPL (the repl tool) with other agents. " +
			"Your blackboard slot is bb['" + agentID + "'] — write only there; read anything; " +
			"never busy-wait on another agent's slot."
	}

	childAgent, err := f.agentBuilder(f.agentLLM, systemPrompt)
	if err != nil {
		return "", fmt.Errorf("create child agent: %w", err)
	}

	todoFile := todo.NewTodoStore()
	skillsReg := skills.NewRegistry()

	childRunner, err := runner.NewAgentRunner(
		childAgent,
		runner.WithToolRegistry(registry),
		runner.WithSkillsRegistry(skillsReg),
		runner.WithTodoFile(todoFile),
		runner.WithSystemPrompt(systemPrompt),
		// Share the parent's emitter so the sub-agent's tool/LLM events reach
		// the same bus (and UI). Its events carry the child's RunnerID.
		runner.WithEmitter(f.emitter),
		// The hierarchical agent ID is the runner ID: events, logs, and the
		// blackboard slot all use the same identifier.
		runner.WithID(agentID),
	)
	if err != nil {
		return "", fmt.Errorf("create child runner: %w", err)
	}

	if f.emitter != nil {
		f.emitter.Emit(events.SubagentStartedEvent{
			BaseEvent: events.NewBaseEvent(events.EventSubagentStarted, childRunner.ID()),
			AgentID:   agentID,
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
				AgentID:   agentID,
				Duration:  duration.Round(time.Millisecond),
			})
		}
		return "", err
	}

	slog.Info("[subagent] completed", "depth", childDepth, "duration", duration.Round(time.Millisecond), "answer_len", len(result))
	if f.emitter != nil {
		f.emitter.Emit(events.SubagentStoppedEvent{
			BaseEvent: events.NewBaseEvent(events.EventSubagentStopped, childRunner.ID()),
			AgentID:   agentID,
			Duration:  duration.Round(time.Millisecond),
		})
	}
	if f.blackboard == nil {
		return result, nil
	}
	if len(result) <= inlineResultMax {
		// Name the slot even for inline results: the sub-agent may have
		// written data to bb['<id>'] itself, and the orchestrator otherwise
		// has to guess the slot name.
		return "Sub-agent " + agentID + " completed (blackboard slot bb['" + agentID + "']). " + result, nil
	}
	pv, depositErr := f.blackboard.Deposit(ctx, agentID, "result", result)
	if depositErr != nil {
		slog.Warn("[subagent] blackboard deposit failed, returning result inline",
			"agent_id", agentID, "error", depositErr)
		return result, nil
	}
	return "Sub-agent " + agentID + " completed. " + pv.String(), nil
}

func (f *SubAgentFactory) buildChildToolRegistry(agentID string) *tools.Registry {
	childDepth := f.currentDepth + 1
	registry := tools.NewRegistry(f.cwd)

	if f.blackboard != nil {
		registry.Register(blackboard.NewREPLTool(f.blackboard, agentID))
	}

	if childDepth < f.maxDepth {
		childFactory := &SubAgentFactory{
			agentLLM:      f.agentLLM,
			agentBuilder:  f.agentBuilder,
			maxDepth:      f.maxDepth,
			currentDepth:  childDepth,
			maxIterations: f.maxIterations,
			cwd:           f.cwd,
			emitter:       f.emitter,
			blackboard:    f.blackboard,
			parentID:      agentID,
		}
		registry.Register(NewSpawnAgentTool(childFactory))
	}

	return registry
}
