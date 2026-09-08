package armory

import (
	"sync"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

type DynamicContext struct {
	mu               sync.RWMutex
	ModelAPI         ports.ModelAPI
	ChatModel        ports.ChatModel
	agentGroup       map[string]ports.Agent
	currentStepIndex int
	currentWorkflow  *model.AgentWorkflowConfig
	values           map[string]any
}

func NewDynamicContext() *DynamicContext {
	return &DynamicContext{
		agentGroup: make(map[string]ports.Agent),
		values:     make(map[string]any),
	}
}

func (c *DynamicContext) AddAgent(agent ports.Agent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentGroup[agent.Name()] = agent
}

func (c *DynamicContext) Agent(name string) (ports.Agent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	agent, ok := c.agentGroup[name]
	return agent, ok
}

func (c *DynamicContext) QueryAgentList(names []string) []ports.Agent {
	c.mu.RLock()
	defer c.mu.RUnlock()

	agents := make([]ports.Agent, 0, len(names))
	for _, name := range names {
		if agent, ok := c.agentGroup[name]; ok {
			agents = append(agents, agent)
		}
	}
	return agents
}

// 来记录 Workflow 已经装配到第几个
func (c *DynamicContext) AddCurrentStepIndex() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentStepIndex++
}

// 记录当前正在创建的 Workflow 配置
func (c *DynamicContext) CurrentStepIndex() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentStepIndex
}

func (c *DynamicContext) SetCurrentWorkflow(workflow *model.AgentWorkflowConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentWorkflow = workflow
}

func (c *DynamicContext) CurrentWorkflow() *model.AgentWorkflowConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentWorkflow
}

func (c *DynamicContext) SetValue(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *DynamicContext) Value(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.values[key]
	return value, ok
}
