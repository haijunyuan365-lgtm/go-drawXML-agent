package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-agent-scaffold/internal/domain/agent/ports"
)

// MCP SSE 传输所使用的协议版本和客户端身份。
const (
	mcpProtocolVersion = "2024-11-05"
	mcpClientName      = "ai-agent-scaffold-go"
	mcpClientVersion   = "0.1.0"
)

// MCPSSEClient 维护一条长期 SSE 接收连接，并通过 JSON-RPC POST 地址发送请求。
// pending 用请求 ID 把 POST 请求和 SSE 返回结果重新关联起来。
type MCPSSEClient struct {
	baseURI    string
	endpoint   string
	httpClient *http.Client
	timeout    time.Duration

	mu          sync.Mutex
	started     bool
	postURL     string
	cancelFn    context.CancelFunc
	pending     map[uint64]chan json.RawMessage
	nextID      atomic.Uint64
	streamErr   chan error
	endpointSig chan struct{} //用来阻塞判定是否获得了Post的地址，如果获得了就close，停止阻塞
}

// NewMCPSSEClient 创建 MCP SSE 客户端。HTTP Client 不设置总超时，避免长期 SSE 被自动关闭。
func NewMCPSSEClient(baseURI, endpoint string, requestTimeoutMillis int) *MCPSSEClient {
	timeout := time.Duration(requestTimeoutMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &MCPSSEClient{
		baseURI:    strings.TrimRight(baseURI, "/"),
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 0},
		timeout:    timeout,
	}
}

// ensureStarted 懒启动 SSE、等待 endpoint 事件，并完成 MCP initialize 握手。
func (c *MCPSSEClient) ensureStarted(ctx context.Context) error {
	//防止重复启动
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	//这些对象不能放在构造函数里只创建一次，因为断线后会执行：shutdown(),重新连接时需要创建新的通信状态。
	c.pending = make(map[uint64]chan json.RawMessage)
	c.endpointSig = make(chan struct{})
	c.streamErr = make(chan error, 1)
	streamCtx, cancel := context.WithCancel(context.Background())
	c.cancelFn = cancel
	c.started = true
	c.mu.Unlock()

	if err := c.openSSE(streamCtx); err != nil {
		c.shutdown()
		return err
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, c.timeout)
	defer waitCancel()
	select {
	case <-c.endpointSig:
	case err := <-c.streamErr:
		c.shutdown()
		return err
	case <-waitCtx.Done():
		c.shutdown()
		return fmt.Errorf("mcp sse endpoint event timeout: %w", waitCtx.Err())
	}

	if err := c.initialize(ctx); err != nil {
		c.shutdown()
		return err
	}
	return nil
}

// openSSE 建立 GET text/event-stream 长连接，真正的读取工作交给后台 goroutine。
func (c *MCPSSEClient) openSSE(ctx context.Context) error {
	target := c.baseURI + c.endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("mcp sse build request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp sse open: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return fmt.Errorf("mcp sse status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	go c.readLoop(resp)
	return nil
}

// readLoop 按 SSE 协议逐行解析 event/data，并在空行处派发一个完整事件。
func (c *MCPSSEClient) readLoop(resp *http.Response) {
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	var event string
	var dataBuf strings.Builder
	dispatch := func() {
		defer func() {
			event = ""
			dataBuf.Reset()
		}()
		data := dataBuf.String()
		if data == "" {
			return
		}
		switch event {
		//""空值第一次由handleEndpoint处理，当postUrl已经有了之后，就路由到handleMessage处理
		case "endpoint", "":
			c.handleEndpoint(data, event)
		case "message":
			c.handleMessage(data)
		}
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				c.signalStreamErr(fmt.Errorf("mcp sse read: %w", err))
			} else {
				c.signalStreamErr(fmt.Errorf("mcp sse stream closed"))
			}
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			dispatch()
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

// handleEndpoint 保存服务端通过 endpoint 事件下发的 JSON-RPC POST 地址。
func (c *MCPSSEClient) handleEndpoint(data, eventName string) {
	if c.postURLAlreadySet() {
		if eventName == "" {
			c.handleMessage(data)
		}
		return
	}
	target := c.resolvePostURL(data)
	c.mu.Lock()
	if c.postURL == "" {
		c.postURL = target
		close(c.endpointSig)
	}
	c.mu.Unlock()
}

// postURLAlreadySet 判断服务端 POST 地址是否已经建立。
func (c *MCPSSEClient) postURLAlreadySet() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.postURL != ""
}

// resolvePostURL 将相对 endpoint 解析成可直接请求的绝对 URL。
func (c *MCPSSEClient) resolvePostURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		base, baseErr := url.Parse(c.baseURI)
		if baseErr == nil {
			ref, refErr := url.Parse(raw)
			if refErr == nil {
				return base.ResolveReference(ref).String()
			}
		}
	}
	return raw
}

// handleMessage 解析 SSE 中的 JSON-RPC 响应，并按 ID 唤醒等待中的 callRPC。
func (c *MCPSSEClient) handleMessage(data string) {
	var resp struct {
		ID     json.Number     `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return
	}
	if resp.ID == "" {
		return
	}
	id, err := resp.ID.Int64()
	if err != nil || id <= 0 {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[uint64(id)]
	//收到结果后把ch从pending里面立刻删除
	if ok {
		delete(c.pending, uint64(id))
	}
	c.mu.Unlock()
	if !ok {
		return
	}
	if resp.Error != nil {
		ch <- mustJSON(map[string]any{"__error__": resp.Error.Message, "code": resp.Error.Code})
		close(ch)
		return
	}
	ch <- resp.Result
	close(ch)
}

// mustJSON 用于把内部错误转换成可通过 pending channel 传递的 JSON。
func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// signalStreamErr 广播 SSE 断流错误，并终止所有尚未完成的 RPC。
func (c *MCPSSEClient) signalStreamErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case c.streamErr <- err:
	default:
	}
	for id, ch := range c.pending {
		ch <- mustJSON(map[string]any{"__error__": err.Error()})
		close(ch)
		delete(c.pending, id)
	}
}

// shutdown 关闭 SSE 上下文并重置客户端启动状态。
func (c *MCPSSEClient) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancelFn != nil {
		c.cancelFn()
	}
	c.started = false
	c.postURL = ""
	c.pending = nil
}

// initialize 完成 MCP 初始化请求与 initialized 通知。
func (c *MCPSSEClient) initialize(ctx context.Context) error {
	_, err := c.callRPC(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    mcpClientName,
			"version": mcpClientVersion,
		},
	})
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("mcp notifications/initialized: %w", err)
	}
	return nil
}

// CallTool 将模型给出的 JSON arguments 转成 MCP tools/call 请求。
func (c *MCPSSEClient) CallTool(ctx context.Context, name, arguments string) (string, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return "", err
	}
	args := map[string]any{}
	trimmed := strings.TrimSpace(arguments)
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return "", fmt.Errorf("mcp tool %q arguments not valid json: %w", name, err)
		}
	}
	result, err := c.callRPC(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", fmt.Errorf("mcp tools/call %q: %w", name, err)
	}
	return extractToolText(result), nil
}

// ListTools 调用 tools/list，返回 MCP Server 暴露的真实工具名。
func (c *MCPSSEClient) ListTools(ctx context.Context) ([]string, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	result, err := c.callRPC(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(parsed.Tools))
	for _, t := range parsed.Tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// toolCallResult 是 MCP tools/call 常见的文本结果结构。
type toolCallResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// extractToolText 优先提取 content 中的 text；无法识别时保留原始 JSON。
func extractToolText(raw json.RawMessage) string {
	var parsed toolCallResult
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(parsed.Content))
	for _, item := range parsed.Content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) == 0 {
		return string(raw)
	}
	return strings.Join(parts, "\n")
}

// callRPC 发送带 ID 的 JSON-RPC 请求，并等待 SSE 通道返回对应 ID 的结果。
func (c *MCPSSEClient) callRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	postURL := c.postURL
	c.pending[id] = ch
	c.mu.Unlock()
	if postURL == "" {
		return nil, fmt.Errorf("mcp post url is not set")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp build post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp post: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp post status %d", resp.StatusCode)
	}

	waitCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("mcp call %s closed unexpectedly", method)
		}
		var probe struct {
			Err string `json:"__error__"`
		}
		if err := json.Unmarshal(msg, &probe); err == nil && probe.Err != "" {
			return nil, fmt.Errorf("%s", probe.Err)
		}
		return msg, nil
	case <-waitCtx.Done():
		return nil, fmt.Errorf("mcp call %s timeout: %w", method, waitCtx.Err())
	}
}

// notify 发送不带 ID、无需等待返回值的 JSON-RPC Notification。
func (c *MCPSSEClient) notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	postURL := c.postURL
	c.mu.Unlock()
	if postURL == "" {
		return fmt.Errorf("mcp post url is not set")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// MCPToolRouter 在装配期发现远程工具，在运行期按工具名路由 tools/call。
type MCPToolRouter struct {
	clients      map[string]*MCPSSEClient
	tools        []ports.Tool
	registered   map[string]struct{}
	listTimeout  time.Duration
	listFailures map[string]error
}

// NewMCPToolRouter 创建进程级共享工具路由器。
func NewMCPToolRouter() *MCPToolRouter {
	return &MCPToolRouter{
		clients:      make(map[string]*MCPSSEClient),
		registered:   make(map[string]struct{}),
		listTimeout:  10 * time.Second,
		listFailures: make(map[string]error),
	}
}

// Register 注册工具；保留该入口以兼容只关心注册、不关心展开结果的调用方。
func (r *MCPToolRouter) Register(tool ports.Tool) {
	r.RegisterAndExpand(tool)
}

// RegisterAndExpand 对 SSE MCP 先执行 tools/list，再把一个服务配置展开成多个模型可见工具。
func (r *MCPToolRouter) RegisterAndExpand(tool ports.Tool) []ports.Tool {
	mcp, ok := tool.(MCPTool)
	if !ok {
		r.tools = append(r.tools, tool)
		return []ports.Tool{tool}
	}
	if mcp.TransportType != "sse" {
		r.tools = append(r.tools, mcp)
		return []ports.Tool{mcp}
	}
	if _, exists := r.clients[mcp.ToolName]; exists {
		return nil
	}
	client := NewMCPSSEClient(mcp.BaseURI, mcp.SSEEndpoint, mcp.RequestTimeout)
	r.clients[mcp.ToolName] = client

	ctx, cancel := context.WithTimeout(context.Background(), r.listTimeout)
	defer cancel()
	names, err := client.ListTools(ctx)
	if err != nil {
		r.listFailures[mcp.ToolName] = err
		r.tools = append(r.tools, mcp)
		return []ports.Tool{mcp}
	}
	expanded := make([]ports.Tool, 0, len(names))
	for _, n := range names {
		r.clients[n] = client
		r.registered[n] = struct{}{}
		t := EinoTool{ToolName: n}
		r.tools = append(r.tools, t)
		expanded = append(expanded, t)
	}
	return expanded
}

// Tools 返回当前路由器已经注册的模型可见工具。
func (r *MCPToolRouter) Tools() []ports.Tool {
	return r.tools
}

// CallTool 根据模型返回的真实工具名找到对应 MCP Client 并执行调用。
func (r *MCPToolRouter) CallTool(ctx context.Context, name, arguments string) (string, error) {
	if client, ok := r.clients[name]; ok {
		return client.CallTool(ctx, name, arguments)
	}
	for _, tool := range r.tools {
		if tool.Name() != name {
			continue
		}
		switch tool.(type) {
		case MCPTool:
			return "", fmt.Errorf("mcp tool %q transport not supported in runtime", name)
		default:
			return "", fmt.Errorf("tool %q is not callable in current runtime", name)
		}
	}
	return "", fmt.Errorf("tool %q is not registered", name)
}
