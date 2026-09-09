package adk

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/validation"
)

// scriptedChatModel 按顺序返回预先配置好的模型响应。
type scriptedChatModel struct {
	replies  []ports.ChatReply
	messages [][]ports.ChatMessage
}

func (m *scriptedChatModel) Generate(
	_ context.Context,
	messages []ports.ChatMessage,
) (ports.ChatReply, error) {
	// 保存每次模型调用收到的消息，便于测试断言。
	snapshot := append(
		[]ports.ChatMessage(nil),
		messages...,
	)
	m.messages = append(m.messages, snapshot)

	if len(m.replies) == 0 {
		return ports.ChatReply{},
			fmt.Errorf("no scripted reply")
	}

	reply := m.replies[0]
	m.replies = m.replies[1:]

	return reply, nil
}

func (m *scriptedChatModel) Stream(
	_ context.Context,
	_ []ports.ChatMessage,
) (
	<-chan ports.ChatStreamEvent,
	<-chan error,
) {
	events := make(chan ports.ChatStreamEvent)
	errs := make(chan error)

	close(events)
	close(errs)

	return events, errs
}

func (m *scriptedChatModel) Tools() []ports.Tool {
	return nil
}

// fakeToolRouter 模拟实际工具执行。
type fakeToolRouter struct {
	result string
	calls  []ports.ChatToolCall
}

func (r *fakeToolRouter) CallTool(
	_ context.Context,
	name string,
	arguments string,
) (string, error) {
	r.calls = append(r.calls, ports.ChatToolCall{
		Name:      name,
		Arguments: arguments,
	})

	return r.result, nil
}

// 验证上一个 Agent 的 output-key 能注入下一个 Agent 的 instruction。
func TestRunSequentialInjectsOutputKey(t *testing.T) {
	analystModel := &scriptedChatModel{
		replies: []ports.ChatReply{
			{
				Content: "绘制用户登录流程图",
			},
		},
	}

	drawerModel := &scriptedChatModel{
		replies: []ports.ChatReply{
			{
				Content: "<mxfile>test</mxfile>",
			},
		},
	}

	analyst := &Agent{
		name:        "agent_analyst",
		kind:        "llm",
		instruction: "分析用户需求",
		outputKey:   "analysis_result",
		chatModel:   analystModel,
	}

	drawer := &Agent{
		name:        "agent_drawer",
		kind:        "llm",
		instruction: "上一步结果：{analysis_result}",
		outputKey:   "draft_diagram",
		chatModel:   drawerModel,
	}

	workflow := &Agent{
		name: "sequential_draw_process",
		kind: "sequential",
		subAgents: []ports.Agent{
			analyst,
			drawer,
		},
	}

	content := model.ChatContent{
		Texts: []model.TextPart{
			{
				Message: "帮我画登录流程图",
			},
		},
	}

	output, err := workflow.runSequential(
		context.Background(),
		content,
		nil,
	)
	if err != nil {
		t.Fatalf("run sequential: %v", err)
	}

	if output != "<mxfile>test</mxfile>" {
		t.Fatalf(
			"unexpected output: %s",
			output,
		)
	}

	if len(drawerModel.messages) != 1 {
		t.Fatalf(
			"expected drawer model to be called once, got %d",
			len(drawerModel.messages),
		)
	}

	gotInstruction :=
		drawerModel.messages[0][0].Content

	wantInstruction :=
		"上一步结果：绘制用户登录流程图"

	if gotInstruction != wantInstruction {
		t.Fatalf(
			"unexpected instruction:\ngot:  %s\nwant: %s",
			gotInstruction,
			wantInstruction,
		)
	}
}

// 验证“模型 -> 工具 -> 模型”的完整调用循环。
func TestRunLLMToolCallLoop(t *testing.T) {
	chatModel := &scriptedChatModel{
		replies: []ports.ChatReply{
			{
				ToolCalls: []ports.ChatToolCall{
					{
						ID:        "call_001",
						Name:      "search",
						Arguments: `{"query":"Go 学习路线"}`,
					},
				},
			},
			{
				Content: "最终 Go 学习计划",
			},
		},
	}

	router := &fakeToolRouter{
		result: "搜索结果：Go 官方教程",
	}

	agent := &Agent{
		name:        "study_agent",
		kind:        "llm",
		instruction: "根据需要调用工具",
		chatModel:   chatModel,
		router:      router,
	}

	content := model.ChatContent{
		Texts: []model.TextPart{
			{
				Message: "帮我制定 Go 学习计划",
			},
		},
	}

	output, err := agent.runLLM(
		context.Background(),
		content,
		nil,
	)
	if err != nil {
		t.Fatalf("run llm: %v", err)
	}

	if output != "最终 Go 学习计划" {
		t.Fatalf(
			"unexpected output: %s",
			output,
		)
	}

	// 第一次模型决定调用工具，第二次模型生成最终答案。
	if len(chatModel.messages) != 2 {
		t.Fatalf(
			"expected 2 model calls, got %d",
			len(chatModel.messages),
		)
	}

	if len(router.calls) != 1 {
		t.Fatalf(
			"expected 1 tool call, got %d",
			len(router.calls),
		)
	}

	// 第二次模型请求应该包含：
	// system、user、assistant tool_calls、tool result。
	secondMessages := chatModel.messages[1]

	if len(secondMessages) != 4 {
		t.Fatalf(
			"expected 4 messages, got %d",
			len(secondMessages),
		)
	}

	if secondMessages[2].Role != ports.ChatRoleAssistant {
		t.Fatalf(
			"expected assistant message, got %s",
			secondMessages[2].Role,
		)
	}

	if secondMessages[3].Role != ports.ChatRoleTool {
		t.Fatalf(
			"expected tool message, got %s",
			secondMessages[3].Role,
		)
	}

	if secondMessages[3].ToolCallID != "call_001" {
		t.Fatalf(
			"unexpected tool call id: %s",
			secondMessages[3].ToolCallID,
		)
	}

	if secondMessages[3].Content !=
		"搜索结果：Go 官方教程" {
		t.Fatalf(
			"unexpected tool result: %s",
			secondMessages[3].Content,
		)
	}
}

// 验证 Guardrail 通过时 Runner 仍逐字返回原结果，保持成功接口兼容。
// 以下测试验证 Guardrail 的 Runner 边界：合法结果保持兼容，坏结果在同步和
// 流式入口都不能发出，配置名称错误则必须在组装阶段暴露。
func TestRunnerOutputValidatorAllowsValidDocument(t *testing.T) {
	const output = `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="a" vertex="1" parent="1"><mxGeometry width="80" height="40"/></mxCell></root></mxGraphModel></diagram></mxfile>`
	runner := newTestRunnerWithOutput(t, output, "drawio-xml")

	outputs, err := runner.Run("user", "session", model.ChatContent{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(outputs) != 1 || outputs[0] != output {
		t.Fatalf("unexpected outputs: %#v", outputs)
	}
}

// 验证 Guardrail 拒绝时同步入口没有任何成功 outputs，并保留结构化问题。
func TestRunnerOutputValidatorBlocksInvalidDocument(t *testing.T) {
	runner := newTestRunnerWithOutput(t, `<mxfile><diagram>`, "drawio-xml")

	outputs, err := runner.Run("user", "session", model.ChatContent{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if outputs != nil {
		t.Fatalf("invalid output must not be returned, got %#v", outputs)
	}

	var validationErr *validation.Error
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation.Error, got %T: %v", err, err)
	}
	if validationErr.Result.Passed || len(validationErr.Result.Issues) == 0 {
		t.Fatalf("expected structured issues, got %+v", validationErr.Result)
	}
}

// 验证流式入口会先完成整份 XML 校验，坏结果不会提前进入输出 Channel。
func TestRunnerStreamDoesNotEmitInvalidDocument(t *testing.T) {
	runner := newTestRunnerWithOutput(t, `<mxfile/>`, "drawio-xml")
	outputs, errs := runner.Stream("user", "session", model.ChatContent{})

	for output := range outputs {
		t.Fatalf("invalid output was emitted: %q", output)
	}
	err := <-errs
	var validationErr *validation.Error
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation.Error, got %T: %v", err, err)
	}
}

// 配置名称拼错应在 Runner 组装时暴露，不能推迟到用户请求期间。
func TestNewRunnerRejectsUnknownOutputValidator(t *testing.T) {
	factory := NewFactory()
	agent := &Agent{name: "test", kind: "llm", chatModel: &scriptedChatModel{}}

	_, err := factory.NewRunner(context.Background(), "app", agent, nil, "missing-validator")
	if err == nil {
		t.Fatal("expected unknown validator to fail")
	}
}

func newTestRunnerWithOutput(t *testing.T, output, validatorName string) model.Runner {
	t.Helper()
	factory := NewFactory()
	agent := &Agent{
		name: "test",
		kind: "llm",
		chatModel: &scriptedChatModel{replies: []ports.ChatReply{{
			Content: output,
		}}},
	}
	runner, err := factory.NewRunner(context.Background(), "app", agent, nil, validatorName)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	return runner
}
