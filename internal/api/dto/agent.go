package dto

// AiAgentConfigResponse 是查询 Agent 列表时返回给前端的精简视图。
type AiAgentConfigResponse struct {
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	AgentDesc string `json:"agentDesc"`
}

// CreateSessionRequest 描述创建会话接口需要的请求参数。
type CreateSessionRequest struct {
	AgentID string `json:"agentId"`
	UserID  string `json:"userId"`
}

// CreateSessionResponse 只向调用方返回新建或复用的 sessionId。
type CreateSessionResponse struct {
	SessionID string `json:"sessionId"`
}

// ChatRequest 描述同步聊天和流式聊天共用的请求结构。
type ChatRequest struct {
	AgentID   string `json:"agentId"`
	UserID    string `json:"userId"`
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

// ChatResponse 是非流式聊天接口的响应数据。
type ChatResponse struct {
	Content string `json:"content"`
}
