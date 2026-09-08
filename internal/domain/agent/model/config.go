package model

type WorkflowType string

const (
	WorkflowTypeLoop       WorkflowType = "loop"
	WorkflowTypeParallel   WorkflowType = "parallel"
	WorkflowTypeSequential WorkflowType = "sequential"
)

// AiAgentConfigTable 表示一套完整、独立的 Agent 应用配置
type AiAgentConfigTable struct {
	AppName string       `yaml:"app-name" json:"appName"`
	Agent   AgentSummary `yaml:"agent" json:"agent"`
	Module  AgentModule  `yaml:"module" json:"module"`
}

type AgentSummary struct {
	AgentID   string `yaml:"agent-id" json:"agentId"`
	AgentName string `yaml:"agent-name" json:"agentName"`
	AgentDesc string `yaml:"agent-desc" json:"agentDesc"`
}

type AgentModule struct {
	AiAPI          AiAPIConfig           `yaml:"ai-api" json:"aiApi"`
	ChatModel      ChatModelConfig       `yaml:"chat-model" json:"chatModel"`
	Agents         []AgentConfig         `yaml:"agents" json:"agents"`
	AgentWorkflows []AgentWorkflowConfig `yaml:"agent-workflows" json:"agentWorkflows"`
	Runner         RunnerConfig          `yaml:"runner" json:"runner"`
}

type AiAPIConfig struct {
	BaseURL         string `yaml:"base-url" json:"baseUrl"`
	APIKey          string `yaml:"api-key" json:"apiKey"`
	CompletionsPath string `yaml:"completions-path" json:"completionsPath"`
	EmbeddingsPath  string `yaml:"embeddings-path" json:"embeddingsPath"`
}

type ChatModelConfig struct {
	Model          string             `yaml:"model" json:"model"`
	ToolMCPList    []ToolMCPConfig    `yaml:"tool-mcp-list" json:"toolMcpList"`
	ToolSkillsList []ToolSkillsConfig `yaml:"tool-skills-list" json:"toolSkillsList"`
}

type ToolMCPConfig struct {
	SSE   *SSEServerParameters   `yaml:"sse,omitempty" json:"sse,omitempty"`
	Stdio *StdioServerParameters `yaml:"stdio,omitempty" json:"stdio,omitempty"`
	Local *LocalToolParameters   `yaml:"local,omitempty" json:"local,omitempty"`
}

type SSEServerParameters struct {
	Name           string `yaml:"name" json:"name"`
	BaseURI        string `yaml:"base-uri" json:"baseUri"`
	SSEEndpoint    string `yaml:"sse-endpoint" json:"sseEndpoint"`
	RequestTimeout int    `yaml:"request-timeout" json:"requestTimeout"`
}

type StdioServerParameters struct {
	Name             string           `yaml:"name" json:"name"`
	RequestTimeout   int              `yaml:"request-timeout" json:"requestTimeout"`
	ServerParameters ServerParameters `yaml:"server-parameters" json:"serverParameters"`
}

type ServerParameters struct {
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args" json:"args"`
	Env     map[string]string `yaml:"env" json:"env"`
}

type LocalToolParameters struct {
	Name string `yaml:"name" json:"name"`
}

type ToolSkillsConfig struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path" json:"path"`
}

type AgentConfig struct {
	Name        string `yaml:"name" json:"name"`
	Instruction string `yaml:"instruction" json:"instruction"`
	Description string `yaml:"description" json:"description"`
	OutputKey   string `yaml:"output-key" json:"outputKey"`
}

type AgentWorkflowConfig struct {
	Type          WorkflowType `yaml:"type" json:"type"`
	Name          string       `yaml:"name" json:"name"`
	SubAgents     []string     `yaml:"sub-agents" json:"subAgents"`
	Description   string       `yaml:"description" json:"description"`
	MaxIterations int          `yaml:"max-iterations" json:"maxIterations"`
}

type RunnerConfig struct {
	AgentName      string   `yaml:"agent-name" json:"agentName"`
	PluginNameList []string `yaml:"plugin-name-list" json:"pluginNameList"`
}
