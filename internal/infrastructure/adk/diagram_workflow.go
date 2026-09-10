package adk

import (
	"context"
	"fmt"
	"strings"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/diagram/drawio"
	"ai-agent-scaffold/internal/domain/diagram/repair"
	"ai-agent-scaffold/internal/domain/diagram/reviewer"
	diagramworkflow "ai-agent-scaffold/internal/domain/diagram/workflow"
)

const analysisResultVariable = "analysis_result"

// NewDrawIORepairAgent 是 Day 5 的生产装配入口：它接收 Armory 已创建的真实 Agent，
// 校验四个角色与协议，再把 Day 4 Controller 放回原 Workflow/Runner 运行时。
func (f *Factory) NewDrawIORepairAgent(
	_ context.Context,
	config model.AgentWorkflowConfig,
	subAgents []ports.Agent,
) (ports.Agent, error) {
	if config.Type != model.WorkflowTypeDrawIORepair {
		return nil, fmt.Errorf("drawio repair factory received workflow type %q", config.Type)
	}

	roles, err := resolveDrawIORoleAgents(config.Roles, subAgents)
	if err != nil {
		return nil, fmt.Errorf("drawio repair workflow %q: %w", config.Name, err)
	}
	// Reviewer/Repairer 的系统指令属于领域协议。启动时检查 YAML 与 Go 常量一致，
	// 避免配置被改成“直接修图”后破坏结构化审查边界。
	if err := requireProtocolInstruction("reviewer", roles.reviewer, reviewer.Instruction); err != nil {
		return nil, fmt.Errorf("drawio repair workflow %q: %w", config.Name, err)
	}
	if err := requireProtocolInstruction("repairer", roles.repairer, repair.Instruction); err != nil {
		return nil, fmt.Errorf("drawio repair workflow %q: %w", config.Name, err)
	}

	maxRepairs := diagramworkflow.DefaultMaxRepairs
	if config.MaxRepairs != nil {
		maxRepairs = *config.MaxRepairs
	}
	validator, err := f.resolveValidator(drawio.NewValidator().Name())
	if err != nil {
		return nil, fmt.Errorf("drawio repair workflow %q: %w", config.Name, err)
	}
	controller, err := diagramworkflow.NewController(
		diagramworkflow.Config{MaxRepairs: maxRepairs},
		diagramworkflow.Dependencies{
			Analyst:   analystAgentAdapter{agent: roles.analyst},
			Drawer:    drawerAgentAdapter{agent: roles.drawer},
			Reviewer:  reviewerAgentAdapter{agent: roles.reviewer},
			Repairer:  repairerAgentAdapter{agent: roles.repairer},
			Validator: validator,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create drawio repair workflow %q: %w", config.Name, err)
	}

	agent, err := newWorkflowAgent(string(model.WorkflowTypeDrawIORepair), config, subAgents, f.router)
	if err != nil {
		return nil, err
	}
	impl, ok := agent.(*Agent)
	if !ok {
		return nil, fmt.Errorf("drawio repair workflow %q has unsupported runtime type %T", config.Name, agent)
	}
	impl.diagramController = controller
	return impl, nil
}

// runDrawIORepair 由原 Agent.runWithVars 分发。它只把用户需求交给 Controller，
// 成功时返回已经同时通过 Validator 和 Reviewer 的 FinalXML。
func (a *Agent) runDrawIORepair(ctx context.Context, content model.ChatContent) (string, error) {
	if a.diagramController == nil {
		return "", fmt.Errorf("drawio repair workflow %q has no controller", a.name)
	}
	runID := fmt.Sprintf("%s:%d", a.name, a.runCounter.Add(1))
	state, err := a.diagramController.Run(ctx, diagramworkflow.RunInput{
		RunID:               runID,
		OriginalRequirement: firstText(content),
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(state.FinalXML) == "" {
		return "", fmt.Errorf("drawio repair workflow %q completed without final XML", a.name)
	}
	return state.FinalXML, nil
}

type drawIORoleAgents struct {
	analyst  *Agent
	drawer   *Agent
	reviewer *Agent
	repairer *Agent
}

// resolveDrawIORoleAgents 按名称绑定而不是按切片下标绑定，并再次防御绕过 Loader 的直接 Factory 调用。
func resolveDrawIORoleAgents(config model.DrawIORepairRoles, subAgents []ports.Agent) (drawIORoleAgents, error) {
	available := make(map[string]*Agent, len(subAgents))
	for _, subAgent := range subAgents {
		if subAgent == nil {
			return drawIORoleAgents{}, fmt.Errorf("sub-agent is nil")
		}
		impl, ok := subAgent.(*Agent)
		if !ok || impl.kind != "llm" {
			return drawIORoleAgents{}, fmt.Errorf("role agent %q must be a runnable LLM agent", subAgent.Name())
		}
		available[subAgent.Name()] = impl
	}

	seen := make(map[string]string, 4)
	lookup := func(role, name string) (*Agent, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("roles.%s is required", role)
		}
		if previousRole, exists := seen[name]; exists {
			return nil, fmt.Errorf("roles.%s duplicates roles.%s agent %q", role, previousRole, name)
		}
		seen[name] = role
		agent, exists := available[name]
		if !exists {
			return nil, fmt.Errorf("roles.%s references unavailable agent %q", role, name)
		}
		return agent, nil
	}

	analyst, err := lookup("analyst", config.Analyst)
	if err != nil {
		return drawIORoleAgents{}, err
	}
	drawer, err := lookup("drawer", config.Drawer)
	if err != nil {
		return drawIORoleAgents{}, err
	}
	reviewerAgent, err := lookup("reviewer", config.Reviewer)
	if err != nil {
		return drawIORoleAgents{}, err
	}
	repairerAgent, err := lookup("repairer", config.Repairer)
	if err != nil {
		return drawIORoleAgents{}, err
	}
	return drawIORoleAgents{
		analyst: analyst, drawer: drawer, reviewer: reviewerAgent, repairer: repairerAgent,
	}, nil
}

func requireProtocolInstruction(role string, agent *Agent, expected string) error {
	if strings.TrimSpace(agent.instruction) != strings.TrimSpace(expected) {
		return fmt.Errorf("roles.%s agent %q instruction does not match the required protocol", role, agent.name)
	}
	return nil
}

type analystAgentAdapter struct{ agent *Agent }

func (a analystAgentAdapter) Analyze(ctx context.Context, requirement string) (string, error) {
	return a.agent.runWithVars(ctx, textContent(requirement), nil)
}

type drawerAgentAdapter struct{ agent *Agent }

func (a drawerAgentAdapter) Draw(ctx context.Context, input diagramworkflow.DrawInput) (string, error) {
	return a.agent.runWithVars(ctx, textContent(input.OriginalRequirement), map[string]string{
		analysisResultVariable: input.AnalysisResult,
	})
}

type reviewerAgentAdapter struct{ agent *Agent }

func (a reviewerAgentAdapter) Review(ctx context.Context, prompt reviewer.Prompt) (string, error) {
	if err := requireProtocolInstruction("reviewer", a.agent, prompt.Instruction); err != nil {
		return "", err
	}
	return a.agent.runWithVars(ctx, textContent(prompt.Payload), nil)
}

type repairerAgentAdapter struct{ agent *Agent }

func (a repairerAgentAdapter) Repair(ctx context.Context, prompt repair.Prompt) (string, error) {
	if err := requireProtocolInstruction("repairer", a.agent, prompt.Instruction); err != nil {
		return "", err
	}
	return a.agent.runWithVars(ctx, textContent(prompt.Payload), nil)
}

func textContent(text string) model.ChatContent {
	return model.ChatContent{Texts: []model.TextPart{{Message: text}}}
}
