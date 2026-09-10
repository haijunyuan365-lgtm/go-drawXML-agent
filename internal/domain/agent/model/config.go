package model

type WorkflowType string

const (
	WorkflowTypeLoop         WorkflowType = "loop"
	WorkflowTypeParallel     WorkflowType = "parallel"
	WorkflowTypeSequential   WorkflowType = "sequential"
	WorkflowTypeDrawIORepair WorkflowType = "drawio-repair"
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
	// Roles 为 Draw.io 质量闭环显式声明四个角色，避免依赖 sub-agents 的位置猜测职责。
	Roles DrawIORepairRoles `yaml:"roles,omitempty" json:"roles,omitempty"`
	// MaxRepairs 使用指针区分“未配置”和显式配置 0；0 是合法的无修复对照模式。
	MaxRepairs *int `yaml:"max-repairs,omitempty" json:"maxRepairs,omitempty"`
}

// DrawIORepairRoles 保存质量闭环中每个职责对应的 Agent 名称。
// 名称仍由 YAML 决定，运行时通过 Armory 已创建的 Agent 解析。
type DrawIORepairRoles struct {
	Analyst  string `yaml:"analyst" json:"analyst"`
	Drawer   string `yaml:"drawer" json:"drawer"`
	Reviewer string `yaml:"reviewer" json:"reviewer"`
	Repairer string `yaml:"repairer" json:"repairer"`
}

// AgentNames 按工作流首次执行顺序返回角色引用，供 Armory 查询真实 Agent。
func (r DrawIORepairRoles) AgentNames() []string {
	return []string{r.Analyst, r.Drawer, r.Reviewer, r.Repairer}
}

type RunnerConfig struct {
	AgentName      string   `yaml:"agent-name" json:"agentName"`
	PluginNameList []string `yaml:"plugin-name-list" json:"pluginNameList"`
	// OutputValidator 为空时保持原 Runner 行为；有值时选择最终输出 Guardrail。
	OutputValidator string `yaml:"output-validator,omitempty" json:"outputValidator,omitempty"`
}
