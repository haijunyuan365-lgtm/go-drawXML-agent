package adk

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/diagram/drawio"
	diagramworkflow "ai-agent-scaffold/internal/domain/diagram/workflow"
	"ai-agent-scaffold/internal/domain/validation"

	"google.golang.org/adk/plugin"
	"google.golang.org/adk/plugin/loggingplugin"
)

// maxToolCallIterations 限制“模型 -> 工具 -> 模型”的最大循环次数，避免模型无限调用工具。
const maxToolCallIterations = 4

// Factory 同时实现 ports.AgentFactory 和 ports.RunnerFactory。
// 它负责创建 Agent、Workflow Agent、Runner，并统一管理插件构造器和 ToolRouter。
type Factory struct {
	// sessionCounter 被该 Factory 创建的所有 Runner 共享，用于生成递增的 sessionId。
	sessionCounter atomic.Uint64

	// plugins 保存“插件名称 -> 插件构造函数”，根据 YAML 中的名称动态创建插件。
	plugins map[string]func() (ports.RunnerPlugin, error)

	// validators 保存可由 runner.output-validator 选择的确定性输出校验器。
	validators map[string]validation.Validator

	// router 负责把模型返回的 tool call 路由到实际工具。
	router ports.ToolRouter
}

// Agent 是项目内部统一的运行时 Agent 实现。
// kind 决定它是普通 LLM Agent，还是某一种 Workflow Agent。
type Agent struct {
	name              string
	kind              string
	description       string
	instruction       string
	outputKey         string
	chatModel         ports.ChatModel
	router            ports.ToolRouter
	subAgents         []ports.Agent
	diagramController *diagramworkflow.Controller
	// runCounter 只生成进程内唯一运行标识；请求状态仍全部保存在 Controller.Run 的局部变量中。
	runCounter atomic.Uint64
}

// Runner 是应用级执行入口。
// 它负责校验会话参数、执行插件 Hook，然后调用入口 Agent。
type Runner struct {
	appName   string
	agent     ports.Agent
	plugins   []ports.RunnerPlugin
	validator validation.Validator
	counter   *atomic.Uint64
}

// NewFactory 创建默认 Factory，并注册项目内置插件和确定性输出校验器。
func NewFactory() *Factory {
	drawioValidator := drawio.NewValidator()
	return &Factory{
		plugins: defaultPlugins(),
		validators: map[string]validation.Validator{
			drawioValidator.Name(): drawioValidator,
		},
	}
}

// UseToolRouter 向 Factory 注入工具路由器。
// 后续 Factory 创建出来的 LLM Agent 和 Workflow Agent 都会共享该路由器。
func (f *Factory) UseToolRouter(router ports.ToolRouter) {
	f.router = router
}

// NewLLMAgent 根据 YAML 中的 AgentConfig 创建普通 LLM Agent。
func (f *Factory) NewLLMAgent(_ context.Context, config model.AgentConfig, chatModel ports.ChatModel) (ports.Agent, error) {
	if strings.TrimSpace(config.Name) == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if chatModel == nil {
		return nil, fmt.Errorf("agent %q requires a chat model", config.Name)
	}

	return &Agent{
		name:        config.Name,
		kind:        "llm",
		description: config.Description,
		instruction: config.Instruction,
		outputKey:   config.OutputKey,
		chatModel:   chatModel,
		router:      f.router,
	}, nil
}

// NewLoopAgent 创建 loop 类型 Workflow Agent。
func (f *Factory) NewLoopAgent(_ context.Context, config model.AgentWorkflowConfig, subAgents []ports.Agent) (ports.Agent, error) {
	return newWorkflowAgent("loop", config, subAgents, f.router)
}

// NewParallelAgent 创建 parallel 类型 Workflow Agent。
func (f *Factory) NewParallelAgent(_ context.Context, config model.AgentWorkflowConfig, subAgents []ports.Agent) (ports.Agent, error) {
	return newWorkflowAgent("parallel", config, subAgents, f.router)
}

// NewSequentialAgent 创建 sequential 类型 Workflow Agent。
func (f *Factory) NewSequentialAgent(_ context.Context, config model.AgentWorkflowConfig, subAgents []ports.Agent) (ports.Agent, error) {
	return newWorkflowAgent("sequential", config, subAgents, f.router)
}

// NewRunner 根据入口 Agent、插件和可选输出校验器创建真正可执行的 Runner。
// 校验器名称在启动组装阶段解析，配置拼错时立即失败，不把问题拖到用户请求阶段。
func (f *Factory) NewRunner(_ context.Context, appName string, agent ports.Agent, pluginNames []string, outputValidatorName string) (model.Runner, error) {
	if strings.TrimSpace(appName) == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if agent == nil {
		return nil, fmt.Errorf("agent is required")
	}

	plugins, err := f.resolvePlugins(pluginNames)
	if err != nil {
		return nil, err
	}
	validator, err := f.resolveValidator(outputValidatorName)
	if err != nil {
		return nil, err
	}

	return &Runner{
		appName:   appName,
		agent:     agent,
		plugins:   plugins,
		validator: validator,
		counter:   &f.sessionCounter,
	}, nil
}

// newWorkflowAgent 是三种 Workflow Agent 的公共构造函数。
// Workflow 自己不直接绑定 ChatModel，而是组合已经创建好的 subAgents。
func newWorkflowAgent(kind string, config model.AgentWorkflowConfig, subAgents []ports.Agent, router ports.ToolRouter) (ports.Agent, error) {
	if strings.TrimSpace(config.Name) == "" {
		return nil, fmt.Errorf("%s agent name is required", kind)
	}

	return &Agent{
		name:        config.Name,
		kind:        kind,
		description: config.Description,
		subAgents:   subAgents,
		router:      router,
	}, nil
}

// Name 返回 Agent 的唯一名称，Armory 和 Runner 都通过它识别 Agent。
func (a *Agent) Name() string {
	return a.name
}

// Description 返回 Agent 的说明信息。
func (a *Agent) Description() string {
	return a.description
}

// run 是同步执行入口，每次新请求都从一个空变量作用域开始。
func (a *Agent) run(ctx context.Context, content model.ChatContent) (string, error) {
	return a.runWithVars(ctx, content, map[string]string{})
}

// runWithVars 根据 kind 将执行分发到不同运行策略。
func (a *Agent) runWithVars(ctx context.Context, content model.ChatContent, vars map[string]string) (string, error) {
	switch a.kind {
	case "llm":
		return a.runLLM(ctx, content, vars)
	case "sequential":
		return a.runSequential(ctx, content, vars)
	case "drawio-repair":
		return a.runDrawIORepair(ctx, content)
	case "loop", "parallel":
		// 原项目当前把 loop 和 parallel 都实现为依次执行并聚合结果的 fan-out。
		return a.runFanOut(ctx, content, vars)
	default:
		return "", fmt.Errorf("agent %q has unknown kind %q", a.name, a.kind)
	}
}

// stream 是流式执行入口。
// 普通 LLM Agent 走真正的模型流式输出；Workflow 暂时同步执行后一次性发送结果。
func (a *Agent) stream(ctx context.Context, content model.ChatContent, out chan<- string) error {
	switch a.kind {
	case "llm":
		return a.streamLLM(ctx, content, out, map[string]string{})
	default:
		text, err := a.run(ctx, content)
		if err != nil {
			return err
		}
		if text != "" {
			select {
			case out <- text:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
}

// runLLM 执行普通 LLM Agent，并处理模型可能返回的工具调用。
func (a *Agent) runLLM(ctx context.Context, content model.ChatContent, vars map[string]string) (string, error) {
	// 先完成 output-key 变量替换，再组装 system + user 消息。
	messages := initialMessages(
		applyVars(a.instruction, vars),
		firstText(content),
	)
	return a.runLLMMessages(ctx, messages)
}

// runLLMMessages 承担普通 Agent 与协议角色共用的“模型 -> 工具 -> 模型”循环。
// 拆出这一层后，Reviewer/Repairer 适配器仍复用原有工具调用能力，而不复制运行时逻辑。
func (a *Agent) runLLMMessages(ctx context.Context, messages []ports.ChatMessage) (string, error) {
	for iter := 0; iter < maxToolCallIterations; iter++ {
		reply, err := a.chatModel.Generate(ctx, messages)
		if err != nil {
			return "", err
		}

		// 没有 tool_calls，说明模型已经给出最终答案。
		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}

		// 必须把模型本轮的 assistant tool_calls 放回消息历史，
		// 下一轮模型才能知道“这些 tool 结果对应哪一次调用”。
		messages = append(messages, ports.ChatMessage{
			Role:      ports.ChatRoleAssistant,
			Content:   reply.Content,
			ToolCalls: reply.ToolCalls,
		})

		toolMessages, err := a.executeToolCalls(ctx, reply.ToolCalls)
		if err != nil {
			return "", err
		}

		// 工具结果以 role=tool 追加，再让模型继续生成。
		messages = append(messages, toolMessages...)
	}

	return "", fmt.Errorf(
		"agent %q exceeded tool-call iteration limit %d",
		a.name,
		maxToolCallIterations,
	)
}

// streamLLM 执行流式模型调用，同时收集流中的文本和 tool_calls。
func (a *Agent) streamLLM(ctx context.Context, content model.ChatContent, out chan<- string, vars map[string]string) error {
	messages := initialMessages(
		applyVars(a.instruction, vars),
		firstText(content),
	)

	for iter := 0; iter < maxToolCallIterations; iter++ {
		events, errs := a.chatModel.Stream(ctx, messages)

		var (
			// finalText 保存本轮完整 assistant 文本，后续有工具调用时要写回历史。
			finalText strings.Builder
			// toolCalls 汇总流式事件中返回的所有工具调用。
			toolCalls []ports.ChatToolCall
			// done 表示模型是否明确发出了结束事件。
			done bool
		)

		var streamErr error

	streamLoop:
		for {
			select {
			case <-ctx.Done():
				streamErr = ctx.Err()
				break streamLoop

			case ev, ok := <-events:
				if !ok {
					break streamLoop
				}

				if ev.Done {
					done = true
				}

				if ev.Delta != "" {
					finalText.WriteString(ev.Delta)

					select {
					case out <- ev.Delta:
					case <-ctx.Done():
						streamErr = ctx.Err()
						break streamLoop
					}
				}

				if len(ev.ToolCalls) > 0 {
					toolCalls = append(toolCalls, ev.ToolCalls...)
				}

			case err, ok := <-errs:
				if ok && err != nil {
					streamErr = err
				}
				break streamLoop
			}
		}

		if streamErr != nil {
			return streamErr
		}

		// 没有工具调用，本轮流式响应就是最终结果。
		if len(toolCalls) == 0 {
			if !done {
				return fmt.Errorf("agent %q stream closed without completion", a.name)
			}
			return nil
		}

		// 有工具调用时，将本轮 assistant 消息和工具结果放回历史，再进入下一轮。
		messages = append(messages, ports.ChatMessage{
			Role:      ports.ChatRoleAssistant,
			Content:   finalText.String(),
			ToolCalls: toolCalls,
		})

		toolMessages, err := a.executeToolCalls(ctx, toolCalls)
		if err != nil {
			return err
		}
		messages = append(messages, toolMessages...)
	}

	return fmt.Errorf(
		"agent %q exceeded tool-call iteration limit %d",
		a.name,
		maxToolCallIterations,
	)
}

// executeToolCalls 逐个执行模型返回的工具调用，并转换成 role=tool 消息。
func (a *Agent) executeToolCalls(ctx context.Context, calls []ports.ChatToolCall) ([]ports.ChatMessage, error) {
	if a.router == nil {
		return nil, fmt.Errorf(
			"agent %q has no tool router but model requested tool calls",
			a.name,
		)
	}

	out := make([]ports.ChatMessage, 0, len(calls))

	for _, call := range calls {
		result, err := a.router.CallTool(ctx, call.Name, call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", call.Name, err)
		}

		out = append(out, ports.ChatMessage{
			Role:       ports.ChatRoleTool,
			Content:    result,
			ToolCallID: call.ID,
			Name:       call.Name,
		})
	}

	return out, nil
}

// runSequential 按顺序执行子 Agent，并通过 output-key 保存中间结果。
func (a *Agent) runSequential(ctx context.Context, content model.ChatContent, vars map[string]string) (string, error) {
	// 每次工作流执行都克隆一份作用域，避免污染调用方传入的变量。
	scope := cloneVars(vars)
	var last string

	for _, sub := range a.subAgents {
		// 当前项目的可运行实现就是本包内的 *Agent，因此需要类型断言。
		impl, ok := sub.(*Agent)
		if !ok {
			return "", fmt.Errorf("sub-agent %q is not a runnable agent", sub.Name())
		}

		text, err := impl.runWithVars(ctx, content, scope)
		if err != nil {
			return "", err
		}

		last = text

		// 子 Agent 配置了 output-key，就把结果存入变量作用域，供后续 Agent 使用。
		if key := strings.TrimSpace(impl.outputKey); key != "" {
			scope[key] = text
		}
	}

	// 串行工作流最终返回最后一个子 Agent 的结果。
	return last, nil
}

// runFanOut 执行所有子 Agent，并把结果按 Agent 名称聚合。
// 注意：原项目当前实现仍是 for 循环顺序执行，并不是真正 goroutine 并行。
func (a *Agent) runFanOut(ctx context.Context, content model.ChatContent, vars map[string]string) (string, error) {
	parts := make([]string, 0, len(a.subAgents))

	for _, sub := range a.subAgents {
		impl, ok := sub.(*Agent)
		if !ok {
			return "", fmt.Errorf("sub-agent %q is not a runnable agent", sub.Name())
		}

		text, err := impl.runWithVars(ctx, content, vars)
		if err != nil {
			return "", err
		}

		parts = append(parts, fmt.Sprintf("[%s] %s", sub.Name(), text))
	}

	return strings.Join(parts, "\n"), nil
}

// cloneVars 复制变量作用域，避免不同 Workflow 执行之间互相修改同一个 map。
func cloneVars(vars map[string]string) map[string]string {
	out := make(map[string]string, len(vars)+4)
	for k, v := range vars {
		out[k] = v
	}
	return out
}

// applyVars 将 instruction 中的 {key} 替换为对应 output-key 的运行结果。
func applyVars(template string, vars map[string]string) string {
	if template == "" || len(vars) == 0 {
		return template
	}

	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// initialMessages 把 Agent instruction 转成 system 消息，把用户文本转成 user 消息。
func initialMessages(instruction, userText string) []ports.ChatMessage {
	messages := make([]ports.ChatMessage, 0, 2)

	if strings.TrimSpace(instruction) != "" {
		messages = append(messages, ports.ChatMessage{
			Role:    ports.ChatRoleSystem,
			Content: instruction,
		})
	}

	messages = append(messages, ports.ChatMessage{
		Role:    ports.ChatRoleUser,
		Content: userText,
	})

	return messages
}

// CreateSession 生成格式为 appName:userID:counter 的进程内会话 ID。当前项目的 SessionStore 也只是内存版
func (r *Runner) CreateSession(userID string) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", fmt.Errorf("user id is required")
	}

	// atomic.Uint64 保证并发创建会话时计数不会发生数据竞争。
	next := r.counter.Add(1)
	return fmt.Sprintf("%s:%s:%d", r.appName, userID, next), nil
}

// Run 同步执行入口 Agent，并返回项目约定的 []string 结果。
func (r *Runner) Run(userID, sessionID string, content model.ChatContent) ([]string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("user id and session id are required")
	}

	// Agent 执行前先触发插件 Hook。
	if err := r.notifyPlugins(userID, sessionID, content); err != nil {
		return nil, err
	}

	impl, ok := r.agent.(*Agent)
	if !ok {
		return nil, fmt.Errorf("runner agent %q is not runnable", r.agent.Name())
	}

	output, err := impl.run(context.Background(), content)
	if err != nil {
		return nil, err
	}
	if err := r.validateOutput(output); err != nil {
		return nil, err
	}

	if output == "" {
		return []string{}, nil
	}

	return []string{output}, nil
}

// Stream 在 goroutine 中执行入口 Agent，并通过两个 channel 分别返回内容和错误。
func (r *Runner) Stream(userID, sessionID string, content model.ChatContent) (<-chan string, <-chan error) {
	outputs := make(chan string, 8)
	errs := make(chan error, 1)

	go func() {
		defer close(outputs)
		defer close(errs)

		if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
			errs <- fmt.Errorf("user id and session id are required")
			return
		}

		if err := r.notifyPlugins(userID, sessionID, content); err != nil {
			errs <- err
			return
		}

		impl, ok := r.agent.(*Agent)
		if !ok {
			errs <- fmt.Errorf("runner agent %q is not runnable", r.agent.Name())
			return
		}

		// A validator needs the complete document. For configured workflows we
		// therefore validate the final output before exposing any success value.
		if r.validator != nil {
			output, err := impl.run(context.Background(), content)
			if err != nil {
				errs <- err
				return
			}
			if err := r.validateOutput(output); err != nil {
				errs <- err
				return
			}
			if output != "" {
				outputs <- output
			}
			return
		}

		if err := impl.stream(context.Background(), content, outputs); err != nil {
			errs <- err
		}
	}()

	return outputs, errs
}

// validateOutput 对完整最终输出执行 Guardrail。失败时保留结构化 Result，
// 供 HTTP 错误响应和后续 Repair Loop 使用，而不是只返回一段不可解析的文字。
func (r *Runner) validateOutput(output string) error {
	if r.validator == nil {
		return nil
	}

	result := r.validator.Validate(output)
	if result.Passed {
		return nil
	}
	return &validation.Error{Validator: r.validator.Name(), Result: result}
}

// notifyPlugins 按 YAML 配置顺序触发 OnUserMessage 和 BeforeAgent Hook。
func (r *Runner) notifyPlugins(userID, sessionID string, content model.ChatContent) error {
	for _, item := range r.plugins {
		if err := item.OnUserMessage(
			context.Background(),
			r.appName,
			userID,
			sessionID,
			r.agent,
			content,
		); err != nil {
			return fmt.Errorf("plugin %q on user message: %w", item.Name(), err)
		}

		if err := item.BeforeAgent(
			context.Background(),
			r.appName,
			userID,
			sessionID,
			r.agent,
		); err != nil {
			return fmt.Errorf("plugin %q before agent: %w", item.Name(), err)
		}
	}

	return nil
}

// firstText 取 ChatContent 中第一段文本，作为当前实现的用户输入。
func firstText(content model.ChatContent) string {
	if len(content.Texts) == 0 {
		return ""
	}
	return content.Texts[0].Message
}

// defaultPlugins 返回项目内置插件注册表。
// YAML 只保存插件名称，真正的实例由这里的 builder 延迟创建。
func defaultPlugins() map[string]func() (ports.RunnerPlugin, error) {
	return map[string]func() (ports.RunnerPlugin, error){
		"myTestPlugin": newMyTestPlugin,

		"myLogPlugin": func() (ports.RunnerPlugin, error) {
			// 复用 Google ADK 提供的 logging plugin。
			p, err := loggingplugin.New("myLogPlugin")
			if err != nil {
				return nil, err
			}

			// 用适配器把 ADK Plugin 转成项目自己的 ports.RunnerPlugin。
			return adkRunnerPlugin{
				name:   "myLogPlugin",
				plugin: p,
			}, nil
		},
	}
}

// resolvePlugins 根据 runner.plugin-name-list 解析并创建插件实例。
func (f *Factory) resolvePlugins(names []string) ([]ports.RunnerPlugin, error) {
	if len(names) == 0 {
		return nil, nil
	}

	plugins := make([]ports.RunnerPlugin, 0, len(names))

	for _, name := range names {
		pluginName := strings.TrimSpace(name)
		if pluginName == "" {
			continue
		}

		builder, ok := f.plugins[pluginName]
		if !ok {
			return nil, fmt.Errorf("runner plugin %q is not registered", pluginName)
		}

		pluginItem, err := builder()
		if err != nil {
			return nil, fmt.Errorf("create runner plugin %q: %w", pluginName, err)
		}

		plugins = append(plugins, pluginItem)
	}

	return plugins, nil
}

// resolveValidator 根据 YAML 名称解析可选 Guardrail；空名称用于兼容普通 Agent。
func (f *Factory) resolveValidator(name string) (validation.Validator, error) {
	validatorName := strings.TrimSpace(name)
	if validatorName == "" {
		return nil, nil
	}

	validator, ok := f.validators[validatorName]
	if !ok {
		return nil, fmt.Errorf("runner output validator %q is not registered", validatorName)
	}
	return validator, nil
}

// newMyTestPlugin 创建项目自定义的测试插件。
func newMyTestPlugin() (ports.RunnerPlugin, error) {
	return myTestPlugin{}, nil
}

// myTestPlugin 是最简单的自定义插件示例。
type myTestPlugin struct{}

// Name 返回插件注册名称，必须和 YAML 中的名称一致。
func (myTestPlugin) Name() string {
	return "myTestPlugin"
}

// OnUserMessage 在收到用户消息后打印输入内容。
func (myTestPlugin) OnUserMessage(_ context.Context, _, _, _ string, _ ports.Agent, content model.ChatContent) error {
	fmt.Printf("[myTestPlugin] 用户输入信息:%s\n", firstText(content))
	return nil
}

// BeforeAgent 在入口 Agent 开始执行前打印 Agent 名称。
func (myTestPlugin) BeforeAgent(_ context.Context, _, _, _ string, agent ports.Agent) error {
	fmt.Printf("[myTestPlugin] 智能体名称:%s\n", agent.Name())
	return nil
}

// adkRunnerPlugin 把 Google ADK 的 *plugin.Plugin 适配成项目自己的 RunnerPlugin。
type adkRunnerPlugin struct {
	name   string
	plugin *plugin.Plugin //虽然把adk的plugin注册进来了，但是没有用这个
}

// Name 返回适配后插件的名称。
func (p adkRunnerPlugin) Name() string {
	return p.name
}

// OnUserMessage 对应项目自己的用户消息 Hook。
func (p adkRunnerPlugin) OnUserMessage(_ context.Context, _, _, _ string, _ ports.Agent, content model.ChatContent) error {
	// 这里只检查 ADK plugin 是否注册了对应 Callback；原项目并未直接调用 Callback。
	if p.plugin.OnUserMessageCallback() != nil {
		fmt.Printf("[%s] USER MESSAGE RECEIVED %s\n", p.name, firstText(content))
	}
	return nil
}

// BeforeAgent 对应项目自己的 Agent 执行前 Hook。
func (p adkRunnerPlugin) BeforeAgent(_ context.Context, _, _, _ string, agent ports.Agent) error {
	if p.plugin.BeforeAgentCallback() != nil {
		fmt.Printf("[%s] AGENT STARTING %s\n", p.name, agent.Name())
	}
	return nil
}
