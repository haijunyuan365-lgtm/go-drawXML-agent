package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ai-agent-scaffold/internal/domain/agent/ports"
)

// openai兼容的模型实体类，OpenAI 协议客户端 ，模型 HTTP 调用组件
type OpenAIClient struct {
	httpClient     *http.Client
	completionsURL string
	apiKey         string
	model          string
}

type OpenAIToolDef struct {
	Name        string
	Description string
}

func NewOpenAIClient(
	completionsURL,
	apiKey,
	model string,
	requestTimeout time.Duration,
) *OpenAIClient {
	if requestTimeout <= 0 {
		requestTimeout = 5 * time.Minute
	}

	return &OpenAIClient{
		httpClient: &http.Client{
			Timeout: requestTimeout,
		},
		completionsURL: completionsURL,
		apiKey:         apiKey,
		model:          model,
	}
}

// 读出响应体里面的内容，然后使用自定义的结构体接收响应的content和toolcalls返回
func (c *OpenAIClient) Generate(ctx context.Context, messages []ports.ChatMessage, tools []OpenAIToolDef) (ports.ChatReply, error) {
	//创建规范请求格式
	body, err := buildRequestBody(c.model, messages, tools, false)
	if err != nil {
		return ports.ChatReply{}, err
	}

	resp, err := c.do(ctx, body)
	if err != nil {
		return ports.ChatReply{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ChatReply{},
			fmt.Errorf("openai read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.ChatReply{}, fmt.Errorf(
			"openai upstream %d: %s",
			resp.StatusCode,
			truncate(string(raw), 400),
		)
	}

	var parsed openaiCompletion

	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ports.ChatReply{},
			fmt.Errorf("openai decode: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return ports.ChatReply{},
			fmt.Errorf(
				"openai response has no choices",
			)
	}

	choice := parsed.Choices[0].Message
	//请求模型之后的回答内容
	reply := ports.ChatReply{
		Content: choice.Content,
	}
	//请求模型之后的工具调用信息
	for _, tc := range choice.ToolCalls {
		reply.ToolCalls = append(
			reply.ToolCalls,
			ports.ChatToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		)
	}

	return reply, nil
}

// Stream 发起 OpenAI-compatible 流式请求，并把 SSE 数据转换成领域层事件。
func (c *OpenAIClient) Stream(
	ctx context.Context,
	messages []ports.ChatMessage,
	tools []OpenAIToolDef,
) (<-chan ports.ChatStreamEvent, <-chan error) {
	events := make(chan ports.ChatStreamEvent, 8)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)

		body, err := buildRequestBody(c.model, messages, tools, true)
		if err != nil {
			errs <- err
			return
		}

		resp, err := c.do(ctx, body)
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 ||
			resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			errs <- fmt.Errorf(
				"openai upstream %d: %s",
				resp.StatusCode,
				truncate(string(raw), 400),
			)
			return
		}

		// OpenAI 的流式 tool_calls 会拆成多个 delta，
		// 因此需要按 index 暂存并拼接 arguments。
		toolCallBuf := map[int]*ports.ChatToolCall{}
		reader := bufio.NewReader(resp.Body)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					emitToolCalls(events, toolCallBuf)
					events <- ports.ChatStreamEvent{Done: true}
					return
				}
				errs <- fmt.Errorf("openai stream read: %w", err)
				return
			}

			line = strings.TrimRight(line, "\r\n")
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(
				strings.TrimPrefix(line, "data:"),
			)
			//OpenAI-compatible API 约定的流结束标记
			if payload == "[DONE]" {
				emitToolCalls(events, toolCallBuf)
				events <- ports.ChatStreamEvent{Done: true}
				return
			}

			var chunk openaiStreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				errs <- fmt.Errorf("openai stream decode: %w", err)
				return
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			//获取openai传过来的content内容，将其通过channel传给前端
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				select {
				case events <- ports.ChatStreamEvent{Delta: delta.Content}:
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
			}

			for _, toolCall := range delta.ToolCalls {
				current, ok := toolCallBuf[toolCall.Index]
				if !ok {
					current = &ports.ChatToolCall{}
					toolCallBuf[toolCall.Index] = current
				}

				if toolCall.ID != "" {
					current.ID = toolCall.ID
				}
				if toolCall.Function.Name != "" {
					current.Name = toolCall.Function.Name
				}
				if toolCall.Function.Arguments != "" {
					current.Arguments += toolCall.Function.Arguments
				}
			}
		}
	}()

	return events, errs
}

// emitToolCalls 按 index 顺序把聚合完成的工具调用发送给上层。
func emitToolCalls(
	events chan<- ports.ChatStreamEvent,
	buffer map[int]*ports.ChatToolCall,
) {
	if len(buffer) == 0 {
		return
	}

	calls := make([]ports.ChatToolCall, 0, len(buffer))
	for index := 0; index < len(buffer); index++ {
		if call, ok := buffer[index]; ok && call != nil {
			calls = append(calls, *call)
		}
	}

	if len(calls) > 0 {
		events <- ports.ChatStreamEvent{
			ToolCalls: calls,
		}
	}
}

func (c *OpenAIClient) do(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.completionsURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"openai build request: %w",
			err,
		)
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)
	req.Header.Set(
		"Accept",
		"application/json",
	)

	if c.apiKey != "" {
		req.Header.Set(
			"Authorization",
			"Bearer "+c.apiKey,
		)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil,
			fmt.Errorf("openai http: %w", err)
	}

	return resp, nil
}

func buildRequestBody(modelName string, messages []ports.ChatMessage, tools []OpenAIToolDef, stream bool) ([]byte, error) {
	payload := map[string]any{
		"model":    modelName,
		"messages": encodeMessages(messages),
		"stream":   stream,
	}
	//模型有哪些工具的配置
	if len(tools) > 0 {
		payload["tools"] = encodeTools(tools)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf(
			"openai marshal request: %w",
			err,
		)
	}

	return raw, nil
}

func encodeMessages(messages []ports.ChatMessage) []map[string]any {
	encoded := make([]map[string]any, 0, len(messages))

	for _, m := range messages {
		entry := map[string]any{"role": string(m.Role)}

		if m.Content != "" {
			entry["content"] = m.Content
		} else if m.Role != ports.ChatRoleAssistant ||
			len(m.ToolCalls) == 0 {
			entry["content"] = ""
		}

		if m.Name != "" {
			entry["name"] = m.Name
		}

		if m.ToolCallID != "" {
			entry["tool_call_id"] = m.ToolCallID
		}

		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))

			for _, call := range m.ToolCalls {
				calls = append(
					calls,
					map[string]any{
						"id":   call.ID,
						"type": "function",
						"function": map[string]any{
							"name":      call.Name,
							"arguments": call.Arguments,
						},
					},
				)
			}

			entry["tool_calls"] = calls
		}

		encoded = append(encoded, entry)
	}

	return encoded
}

func encodeTools(tools []OpenAIToolDef) []map[string]any {
	out := make(
		[]map[string]any,
		0,
		len(tools),
	)

	for _, tool := range tools {
		out = append(
			out,
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": fallbackDescription(tool),
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "自由文本输入或检索查询，工具会按其语义解释",
							},
						},
					},
				},
			},
		)
	}

	return out
}

func fallbackDescription(tool OpenAIToolDef) string {
	if strings.TrimSpace(tool.Description) != "" {
		return tool.Description
	}

	return "外部工具 " +
		tool.Name +
		"，参数 query 为自由文本"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max] + "..."
}

// 模型回答结构体定义
type openaiCompletion struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`

			ToolCalls []openaiToolCallV1 `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

// openaiStreamChunk 对应 Chat Completions SSE 中每一个 data JSON。
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`

			ToolCalls []openaiStreamToolCall `json:"tool_calls"`
		} `json:"delta"`

		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// openaiStreamToolCall 表示流式响应中的一段工具调用增量。
type openaiStreamToolCall struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Type  string `json:"type"`

	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiToolCallV1 struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	Function struct {
		Name string `json:"name"`

		Arguments string `json:"arguments"`
	} `json:"function"`
}
