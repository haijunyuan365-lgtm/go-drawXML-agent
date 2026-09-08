package armory

import (
	"context"
	"fmt"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

type RunnerNode struct {
	runnerFactory ports.RunnerFactory
	registry      ports.AgentRegistry
}

func NewRunnerNode(runnerFactory ports.RunnerFactory, registry ports.AgentRegistry) RunnerNode {
	return RunnerNode{runnerFactory: runnerFactory, registry: registry}
}

func (n RunnerNode) Apply(ctx context.Context, command model.ArmoryCommand, dynamic *DynamicContext) (model.RegisteredAgent, error) {
	runnerConfig := command.Table.Module.Runner

	if runnerConfig.AgentName == "" {
		return model.RegisteredAgent{},
			fmt.Errorf("runner.agent-name is required")
	}

	agent, ok := dynamic.Agent(runnerConfig.AgentName)
	if !ok {
		return model.RegisteredAgent{},
			fmt.Errorf(
				"runner agent %q not found",
				runnerConfig.AgentName,
			)
	}

	runner, err := n.runnerFactory.NewRunner(
		ctx,
		command.Table.AppName,
		agent,
		runnerConfig.PluginNameList,
	)
	if err != nil {
		return model.RegisteredAgent{}, err
	}

	registered := model.RegisteredAgent{
		AppName:   command.Table.AppName,
		AgentID:   command.Table.Agent.AgentID,
		AgentName: command.Table.Agent.AgentName,
		AgentDesc: command.Table.Agent.AgentDesc,
		Runner:    runner,
	}

	if err := n.registry.Register(registered); err != nil {
		return model.RegisteredAgent{}, err
	}

	return registered, nil
}
