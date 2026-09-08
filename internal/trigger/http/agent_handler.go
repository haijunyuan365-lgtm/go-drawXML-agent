package http

import (
	"errors"
	"net/http"
	"strings"

	"ai-agent-scaffold/internal/api/dto"
	"ai-agent-scaffold/internal/api/response"
	"ai-agent-scaffold/internal/domain/agent/service/chat"
	"ai-agent-scaffold/pkg/types"

	"github.com/gin-gonic/gin"
)

// RegisterAgentRoutes 集中注册 Agent 相关 HTTP 路由。
func RegisterAgentRoutes(router gin.IRouter, service *chat.Service) {
	group := router.Group("/api/v1")
	group.GET("/query_ai_agent_config_list", queryAgentConfigList(service))
	group.POST("/create_session", createSession(service))
	group.GET("/create_session", createSessionQuery(service))
	group.POST("/chat", chatMessage(service))
	group.POST("/chat_stream", chatStream(service))
}

// queryAgentConfigList 把领域层 AgentSummary 转换成 HTTP DTO。
func queryAgentConfigList(service *chat.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		agents := service.QueryAgentConfigList()
		responses := make([]dto.AiAgentConfigResponse, 0, len(agents))

		for _, agent := range agents {
			responses = append(responses, dto.AiAgentConfigResponse{
				AgentID:   agent.AgentID,
				AgentName: agent.AgentName,
				AgentDesc: agent.AgentDesc,
			})
		}

		c.JSON(http.StatusOK, response.Success(responses))
	}
}

// createSession 接收 JSON 参数并创建或复用会话。
func createSession(service *chat.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request dto.CreateSessionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, types.NewAppError(types.CodeIllegalParameter, err.Error()))
			return
		}

		sessionID, err := service.CreateSession(
			request.AgentID,
			request.UserID,
		)
		if err != nil {
			writeError(c, err)
			return
		}

		c.JSON(http.StatusOK, response.Success(
			dto.CreateSessionResponse{
				SessionID: sessionID,
			},
		))
	}
}

// createSessionQuery 提供使用 query 参数创建会话的兼容入口。
func createSessionQuery(service *chat.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := service.CreateSession(
			c.Query("agentId"),
			c.Query("userId"),
		)
		if err != nil {
			writeError(c, err)
			return
		}

		c.JSON(http.StatusOK, response.Success(
			dto.CreateSessionResponse{
				SessionID: sessionID,
			},
		))
	}
}

// chatMessage 把 HTTP 请求转换为领域服务的同步聊天调用。
func chatMessage(service *chat.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request dto.ChatRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, types.NewAppError(
				types.CodeIllegalParameter,
				err.Error(),
			))
			return
		}

		outputs, err := service.HandleMessage(
			request.AgentID,
			request.UserID,
			request.SessionID,
			request.Message,
		)
		if err != nil {
			writeError(c, err)
			return
		}

		c.JSON(http.StatusOK, response.Success(
			dto.ChatResponse{
				Content: strings.Join(outputs, "\n"),
			},
		))
	}
}

// chatStream 以 SSE 事件持续向客户端发送模型输出。
func chatStream(service *chat.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request dto.ChatRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			writeError(c, types.NewAppError(
				types.CodeIllegalParameter,
				err.Error(),
			))
			return
		}

		outputs, errs := service.HandleMessageStream(
			request.AgentID,
			request.UserID,
			request.SessionID,
			request.Message,
		)

		c.Header("Content-Type", "text/event-stream")

		for output := range outputs {
			c.SSEvent("message", output)
			c.Writer.Flush()
		}

		if err, ok := <-errs; ok && err != nil {
			c.SSEvent("error", err.Error())
			c.Writer.Flush()
		}
	}
}

// writeError 把领域错误统一转换成项目约定的 Envelope。
func writeError(c *gin.Context, err error) {
	var appErr *types.AppError
	if errors.As(err, &appErr) {
		c.JSON(
			http.StatusOK,
			response.Failure(appErr.Code, appErr.Info),
		)
		return
	}

	c.JSON(
		http.StatusOK,
		response.Failure(
			types.CodeUnknownError,
			err.Error(),
		),
	)
}
