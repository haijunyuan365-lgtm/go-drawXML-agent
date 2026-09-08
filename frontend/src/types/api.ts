// 与 Go 后端统一响应 Envelope 对应；泛型 T 表示不同接口的 data 类型。
export interface Response<T> {
    code: string;
    info: string;
    data: T;
}

// GET /query_ai_agent_config_list 返回的单个 Agent 配置摘要。
export interface AiAgentConfigResponseDTO {
    agentId: string;
    agentName: string;
    agentDesc: string;
}

// 创建后端会话时需要的参数。当前 agentApi 直接接收两个字符串，因此该类型暂未直接使用。
export interface CreateSessionRequestDTO {
    agentId: string;
    userId: string;
}

// 后端 Runner 创建会话后返回的真正 sessionId。
export interface CreateSessionResponseDTO {
    sessionId: string;
}

// POST /chat 的请求体，字段名必须与 Go DTO 的 json 标签完全一致。
export interface ChatRequestDTO {
    agentId: string;
    userId: string;
    sessionId: string;
    message: string;
}

// 同步聊天结果。content 既可能是普通文本，也可能是 draw.io XML。
export interface ChatResponseDTO {
    content: string;
}
