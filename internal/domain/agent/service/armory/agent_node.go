package armory

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

type AgentNode struct {
	agentFactory ports.AgentFactory
	next         Handler
}

func NewAgentNode(agentFactory ports.AgentFactory, next Handler) AgentNode {
	return AgentNode{
		agentFactory: agentFactory,
		next:         next,
	}
}

func (n AgentNode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *DynamicContext) (model.RegisteredAgent, error) {
	for _, config := range command.Table.Module.Agents {
		agent, err := n.agentFactory.NewLLMAgent(
			ctx,
			config,
			dynamic.ChatModel,
		)
		if err != nil {
			return model.RegisteredAgent{}, err
		}

		dynamic.AddAgent(agent)
	}

	return tree.Route(ctx, n.next, command, dynamic)
}
