package adk

import (
	"context"
	"fmt"
	"testing"

	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
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
