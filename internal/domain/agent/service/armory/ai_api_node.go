package armory

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

type AiAPINode struct {
	modelProvider ports.ModelProvider
	next          Handler
}

func NewAiAPINode(modelProvider ports.ModelProvider, next Handler) AiAPINode {
	return AiAPINode{
		modelProvider: modelProvider,
		next:          next,
	}
}

func (n AiAPINode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *DynamicContext) (model.RegisteredAgent, error) {
	api, err := n.modelProvider.NewAPI(
		ctx,
		command.Table.Module.AiAPI,
	)
	if err != nil {
		return model.RegisteredAgent{}, err
	}

	dynamic.ModelAPI = api

	return tree.Route(ctx, n.next, command, dynamic)
}
