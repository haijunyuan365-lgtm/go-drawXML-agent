package factory

import (
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/agent/service/armory"
	"ai-agent-scaffold/internal/domain/agent/service/armory/workflow"
)

// 创建并连接整条 Armory 节点链。

type DefaultFactory struct {
	root armory.Handler
}

func NewDefaultFactory(
	modelProvider ports.ModelProvider,
	mcpFactory ports.MCPToolFactory,
	skillFactory ports.SkillFactory,
	agentFactory ports.AgentFactory,
	runnerFactory ports.RunnerFactory,
	registry ports.AgentRegistry,
) *DefaultFactory {
	runner := armory.NewRunnerNode(runnerFactory, registry)
	workflowNode := workflow.NewAgentWorkflowNode(agentFactory, runner)
	agent := armory.NewAgentNode(agentFactory, workflowNode)
	chatModel := armory.NewChatModelNode(modelProvider, mcpFactory, skillFactory, agent)
	api := armory.NewAiAPINode(modelProvider, chatModel)
	root := armory.NewRootNode(api)

	return &DefaultFactory{
		root: root,
	}
}

// 外部不需要知道内部有多少节点。只拿到入口：root 这叫封装
func (f *DefaultFactory) ArmoryStrategyHandler() armory.Handler {
	return f.root
}
