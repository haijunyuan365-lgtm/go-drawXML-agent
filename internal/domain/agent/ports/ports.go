package ports

import (
	"context"
	"sync"

	"ai-agent-scaffold/internal/domain/agent/model"
)

type ModelProvider interface {
	NewAPI(ctx context.Context, config model.AiAPIConfig) (ModelAPI, error)
	NewChatModel(ctx context.Context, api ModelAPI, config model.ChatModelConfig, tools []Tool) (ChatModel, error)
}

type ModelAPI interface{}

type ChatRole string

const (
	ChatRoleSystem    ChatRole = "system"
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleTool      ChatRole = "tool"
)

type ChatMessage struct {
	Role       ChatRole
	Content    string
	ToolCallID string
	Name       string
	ToolCalls  []ChatToolCall
}

type ChatToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type ChatReply struct {
	Content   string
	ToolCalls []ChatToolCall
}

type ChatStreamEvent struct {
	Delta     string
	ToolCalls []ChatToolCall
	Done      bool
}

type ChatModel interface {
	Generate(ctx context.Context, messages []ChatMessage) (ChatReply, error)
	Stream(ctx context.Context, messages []ChatMessage) (<-chan ChatStreamEvent, <-chan error)
	//返回这个模型当前绑定的工具。
	Tools() []Tool
}

type Tool interface {
	Name() string
}

type ToolDescriptor interface {
	Description() string
}

type MCPToolFactory interface {
	BuildTools(ctx context.Context, config model.ToolMCPConfig) ([]Tool, error)
}

type SkillFactory interface {
	BuildTools(ctx context.Context, config model.ToolSkillsConfig) ([]Tool, error)
}

type ToolRouter interface {
	CallTool(ctx context.Context, name, arguments string) (string, error)
}

type AgentFactory interface {
	NewLLMAgent(ctx context.Context, config model.AgentConfig, chatModel ChatModel) (Agent, error)
	NewLoopAgent(ctx context.Context, config model.AgentWorkflowConfig, subAgents []Agent) (Agent, error)
	NewParallelAgent(ctx context.Context, config model.AgentWorkflowConfig, subAgents []Agent) (Agent, error)
	NewSequentialAgent(ctx context.Context, config model.AgentWorkflowConfig, subAgents []Agent) (Agent, error)
}

type Agent interface {
	Name() string
}

type RunnerPlugin interface {
	Name() string
	OnUserMessage(ctx context.Context, appName, userID, sessionID string, agent Agent, content model.ChatContent) error
	BeforeAgent(ctx context.Context, appName, userID, sessionID string, agent Agent) error
}

type RunnerFactory interface {
	NewRunner(ctx context.Context, appName string, agent Agent, pluginNames []string) (model.Runner, error)
}

type AgentRegistry interface {
	Register(agent model.RegisteredAgent) error
	Get(agentID string) (model.RegisteredAgent, bool)
	List() []model.RegisteredAgent
}

type SessionStore interface {
	Get(userID, agentID string) (string, bool)
	Set(userID, agentID, sessionID string) error
}

// 是AgentRegistry的内存版实现
type InMemoryAgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]model.RegisteredAgent
}

func NewInMemoryAgentRegistry() *InMemoryAgentRegistry {
	return &InMemoryAgentRegistry{agents: make(map[string]model.RegisteredAgent)}
}

func (r *InMemoryAgentRegistry) Register(agent model.RegisteredAgent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.AgentID] = agent
	return nil
}

func (r *InMemoryAgentRegistry) Get(agentID string) (model.RegisteredAgent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[agentID]
	return agent, ok
}

func (r *InMemoryAgentRegistry) List() []model.RegisteredAgent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]model.RegisteredAgent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	return agents
}

// 是SessionStore的内存版实现
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]string
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{sessions: make(map[string]string)}
}

func (s *InMemorySessionStore) Get(userID, agentID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionID, ok := s.sessions[sessionKey(userID, agentID)]
	return sessionID, ok
}

func (s *InMemorySessionStore) Set(userID, agentID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionKey(userID, agentID)] = sessionID
	return nil
}

func sessionKey(userID, agentID string) string {
	return userID + ":" + agentID
}
