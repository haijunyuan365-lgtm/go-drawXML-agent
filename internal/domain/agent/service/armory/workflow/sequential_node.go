package workflow

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

type SequentialNode struct {
	agentFactory ports.AgentFactory
}

func NewSequentialNode(
	agentFactory ports.AgentFactory,
) SequentialNode {
	return SequentialNode{
		agentFactory: agentFactory,
	}
}

func (n SequentialNode) Build(
	ctx context.Context,
	workflow model.AgentWorkflowConfig,
	subAgents []ports.Agent,
) (ports.Agent, error) {
	return n.agentFactory.NewSequentialAgent(
		ctx,
		workflow,
		subAgents,
	)
}
