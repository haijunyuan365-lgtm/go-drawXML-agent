package workflow

import (
	"context"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

// DrawIORepairNode 保持原 Armory Builder 扩展方式，负责把配置交给运行时 Factory。
// 它不执行 Repair Loop，避免装配层承担请求级状态。
type DrawIORepairNode struct {
	agentFactory ports.AgentFactory
}

func NewDrawIORepairNode(agentFactory ports.AgentFactory) DrawIORepairNode {
	return DrawIORepairNode{agentFactory: agentFactory}
}

func (n DrawIORepairNode) Build(
	ctx context.Context,
	workflow model.AgentWorkflowConfig,
	subAgents []ports.Agent,
) (ports.Agent, error) {
	return n.agentFactory.NewDrawIORepairAgent(ctx, workflow, subAgents)
}
