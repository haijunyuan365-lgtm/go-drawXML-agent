package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
)

// 实现ModelProvider，构造API和ChatModel实体
type EinoProvider struct {
	requestTimeout time.Duration
}

type EinoAPIConfig struct {
	BaseURL         string
	APIKey          string
	CompletionsPath string
	EmbeddingsPath  string
}

type EinoChatModel struct {
	client *OpenAIClient
	tools  []ports.Tool
}

type EinoTool struct {
	ToolName string
}

func NewEinoProvider() *EinoProvider {
	return &EinoProvider{
		requestTimeout: 5 * time.Minute,
	}
}

func (p *EinoProvider) WithRequestTimeout(
	timeout time.Duration,
) *EinoProvider {
	if timeout > 0 {
		p.requestTimeout = timeout
	}
	return p
}

func (p *EinoProvider) NewAPI(
	_ context.Context,
	config model.AiAPIConfig,
) (ports.ModelAPI, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil,
			fmt.Errorf("base url is required")
	}

	if strings.TrimSpace(config.APIKey) == "" {
		return nil,
			fmt.Errorf("api key is required")
	}

	return EinoAPIConfig{
		BaseURL:         config.BaseURL,
		APIKey:          config.APIKey,
		CompletionsPath: config.CompletionsPath,
		EmbeddingsPath:  config.EmbeddingsPath,
	}, nil
}

func (p *EinoProvider) NewChatModel(
	_ context.Context,
	api ports.ModelAPI,
	config model.ChatModelConfig,
	tools []ports.Tool,
) (ports.ChatModel, error) {
	if api == nil {
		return nil,
			fmt.Errorf("model api is required")
	}

	if strings.TrimSpace(config.Model) == "" {
		return nil,
			fmt.Errorf("model is required")
	}
	//类型断言为EinoAPIConfig
	apiCfg, ok := api.(EinoAPIConfig)
	if !ok {
		return nil, fmt.Errorf(
			"unsupported model api type %T",
			api,
		)
	}

	completionsURL := joinURL(
		apiCfg.BaseURL,
		apiCfg.CompletionsPath,
	)

	timeout := p.requestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	client := NewOpenAIClient(
		completionsURL,
		apiCfg.APIKey,
		config.Model,
		timeout,
	)

	return &EinoChatModel{
		client: client,
		tools:  tools,
	}, nil
}

func (m *EinoChatModel) Tools() []ports.Tool {
	return m.tools
}

func (m *EinoChatModel) Generate(ctx context.Context, messages []ports.ChatMessage) (ports.ChatReply, error) {
	return m.client.Generate(ctx, messages, m.toolDefs())
}

func (m *EinoChatModel) Stream(ctx context.Context, messages []ports.ChatMessage) (
	<-chan ports.ChatStreamEvent,
	<-chan error,
) {
	return m.client.Stream(ctx, messages, m.toolDefs())
}

func (m *EinoChatModel) toolDefs() []OpenAIToolDef {
	if len(m.tools) == 0 {
		return nil
	}

	defs := make([]OpenAIToolDef, 0, len(m.tools))

	for _, tool := range m.tools {
		desc := ""

		if d, ok := tool.(ports.ToolDescriptor); ok {
			desc = d.Description()
		}

		defs = append(
			defs,
			OpenAIToolDef{
				Name:        tool.Name(),
				Description: desc,
			},
		)
	}

	return defs
}

func (t EinoTool) Name() string {
	return t.ToolName
}

func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	path = strings.TrimLeft(path, "/")

	if path == "" {
		return base
	}

	return base + "/" + path
}
