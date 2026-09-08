package workflow

import (
	"context"
	"fmt"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/agent/service/armory"
	"ai-agent-scaffold/internal/domain/shared/tree"
)

// 牛汁儿，多态创建父接口
type workflowBuilder interface {
	Build(context.Context, model.AgentWorkflowConfig, []ports.Agent) (ports.Agent, error)
}

type AgentWorkflowNode struct {
	next       armory.Handler
	builders   map[model.WorkflowType]workflowBuilder //使用父接口作为类型，承接多个子类对象
	buildOrder []model.WorkflowType
}

func NewAgentWorkflowNode(agentFactory ports.AgentFactory, next armory.Handler) *AgentWorkflowNode {
	buildOrder := []model.WorkflowType{
		model.WorkflowTypeLoop,
		model.WorkflowTypeParallel,
		model.WorkflowTypeSequential,
	}

	return &AgentWorkflowNode{
		next: next,
		builders: map[model.WorkflowType]workflowBuilder{
			model.WorkflowTypeLoop:       NewLoopNode(agentFactory),
			model.WorkflowTypeParallel:   NewParallelNode(agentFactory),
			model.WorkflowTypeSequential: NewSequentialNode(agentFactory),
		},
		buildOrder: buildOrder,
	}
}

func (n *AgentWorkflowNode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *armory.DynamicContext) (model.RegisteredAgent, error) {
	workflows := command.Table.Module.AgentWorkflows
	//递归出口
	if dynamic.CurrentStepIndex() >= len(workflows) {
		dynamic.SetCurrentWorkflow(nil)
		return tree.Route(ctx, n.next, command, dynamic)
	}

	workflow := workflows[dynamic.CurrentStepIndex()]
	dynamic.SetCurrentWorkflow(&workflow)
	dynamic.AddCurrentStepIndex()

	subAgents := dynamic.QueryAgentList(workflow.SubAgents)

	agent, err := n.build(ctx, workflow, subAgents)
	if err != nil {
		return model.RegisteredAgent{}, err
	}

	dynamic.AddAgent(agent)
	//此处使用递归，当然也可以使用for循环
	return n.Apply(ctx, command, dynamic)
}

func (n *AgentWorkflowNode) build(ctx context.Context, workflow model.AgentWorkflowConfig, subAgents []ports.Agent) (ports.Agent, error) {
	for _, workflowType := range n.buildOrder {
		if workflow.Type != workflowType {
			continue
		}

		return n.builders[workflowType].Build(ctx, workflow, subAgents)
	}

	return nil, fmt.Errorf(
		"agentWorkflow type is error: %s",
		workflow.Type,
	)
}
