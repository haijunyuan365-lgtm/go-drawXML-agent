package workflow

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

type LoopNode struct {
	agentFactory ports.AgentFactory
}

func NewLoopNode(agentFactory ports.AgentFactory) LoopNode {
	return LoopNode{
		agentFactory: agentFactory,
	}
}

func (n LoopNode) Build(ctx context.Context, workflow model.AgentWorkflowConfig, subAgents []ports.Agent) (ports.Agent, error) {
	return n.agentFactory.NewLoopAgent(ctx, workflow, subAgents)
}
