package armory

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

type ChatModelNode struct {
	modelProvider ports.ModelProvider
	mcpFactory    ports.MCPToolFactory
	skillFactory  ports.SkillFactory
	next          Handler
}

func NewChatModelNode(modelProvider ports.ModelProvider, mcpFactory ports.MCPToolFactory, skillFactory ports.SkillFactory, next Handler) ChatModelNode {
	return ChatModelNode{
		modelProvider: modelProvider,
		mcpFactory:    mcpFactory,
		skillFactory:  skillFactory,
		next:          next,
	}
}

func (n ChatModelNode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *DynamicContext) (model.RegisteredAgent, error) {
	var tools []ports.Tool

	for _, config := range command.Table.Module.ChatModel.ToolMCPList {
		built, err := n.mcpFactory.BuildTools(ctx, config)
		if err != nil {
			return model.RegisteredAgent{}, err
		}
		tools = append(tools, built...)
	}

	for _, config := range command.Table.Module.ChatModel.ToolSkillsList {
		built, err := n.skillFactory.BuildTools(ctx, config)
		if err != nil {
			return model.RegisteredAgent{}, err
		}
		tools = append(tools, built...)
	}

	chatModel, err := n.modelProvider.NewChatModel(
		ctx,
		dynamic.ModelAPI,
		command.Table.Module.ChatModel,
		tools,
	)
	if err != nil {
		return model.RegisteredAgent{}, err
	}

	dynamic.ChatModel = chatModel

	return tree.Route(ctx, n.next, command, dynamic)
}
