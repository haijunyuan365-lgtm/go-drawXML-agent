// Package bootstrap wires application configuration and Gin HTTP routes into
// a single runnable engine. Agent assembly will be added in later stages.
package bootstrap

import (
	"ai-agent-scaffold/internal/app/config"
	"ai-agent-scaffold/internal/domain/agent/model"
	"ai-agent-scaffold/internal/domain/agent/ports"
	"ai-agent-scaffold/internal/domain/agent/service/armory"
	"ai-agent-scaffold/internal/domain/agent/service/armory/factory"
	"ai-agent-scaffold/internal/domain/agent/service/chat"
	"ai-agent-scaffold/internal/infrastructure/adk"
	"ai-agent-scaffold/internal/infrastructure/ai"
	httptrigger "ai-agent-scaffold/internal/trigger/http"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Engine struct {
	Application config.Application
	Router      *gin.Engine
	ChatService *chat.Service
	Registry    ports.AgentRegistry
	Sessions    ports.SessionStore
	Logger      *zap.Logger
}

func Build(ctx context.Context, appCfg config.Application, logger *zap.Logger) (*Engine, error) {
	tables, err := loadAgentTables(
		appCfg.Agent.ConfigPaths,
	)
	if err != nil {
		return nil, err
	}

	if len(tables) == 0 {
		return nil, fmt.Errorf(
			"no agent tables loaded; configure agent.config-paths",
		)
	}

	registry := ports.NewInMemoryAgentRegistry()
	sessions := ports.NewInMemorySessionStore()
	// 把配置字符串（例如 5m）解析为真正的 time.Duration。
	requestTimeout, err := appCfg.LLM.RequestTimeoutDuration()
	if err != nil {
		return nil, err
	}

	modelProvider := ai.NewEinoProvider().WithRequestTimeout(requestTimeout)

	// 同一个 ToolRouter 必须同时交给 ToolFactory 和 AgentFactory：
	// 前者负责注册/发现工具，后者负责执行模型返回的 tool_calls。
	toolRouter := ai.NewMCPToolRouter()
	mcpFactory := ai.NewToolFactory(toolRouter)
	// 使用真实 SkillFactory 扫描本地 SKILL.md。
	skillFactory := ai.NewSkillFactory()

	// 使用真实的 Agent / Runner 运行时 Factory，替换 Day 5 的假实现。
	agentFactory := adk.NewFactory()
	agentFactory.UseToolRouter(toolRouter)

	// adk.Factory 同时实现 ports.AgentFactory 和 ports.RunnerFactory。
	runnerFactory := agentFactory

	armoryFactory := factory.NewDefaultFactory(
		modelProvider,
		mcpFactory,
		skillFactory,
		agentFactory,
		runnerFactory,
		registry,
	)

	armoryService := armory.NewService(
		armoryFactory.ArmoryStrategyHandler(),
	)
	//将tables中的每一个table组装成一个可以调用的agent应用
	if err := armoryService.AcceptArmoryAgents(ctx, tables); err != nil {
		return nil, fmt.Errorf(
			"armory assembly: %w",
			err,
		)
	}

	chatService := chat.NewService(registry, sessions)

	router := newRouter(appCfg.App.Env)

	// 将 HTTP 入站层接到已经装配完成的 ChatService。
	httptrigger.RegisterAgentRoutes(router, chatService)

	if logger != nil {
		logger.Info(
			"agents registered",
			zap.Int(
				"count",
				len(registry.List()),
			),
		)
	}

	return &Engine{
		Application: appCfg,
		Router:      router,
		ChatService: chatService,
		Registry:    registry,
		Sessions:    sessions,
		Logger:      logger,
	}, nil
}

func (e *Engine) Run() error {
	addr := e.Application.Server.Addr
	if addr == "" {
		addr = ":8091"
	}

	if e.Logger != nil {
		e.Logger.Info(
			"http server listening",
			zap.String("addr", addr),
		)
	}

	return e.Router.Run(addr)
}

func newRouter(env string) *gin.Engine {
	//设置gin的调试模式
	if env == "prod" || env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	router.Use(gin.Recovery())

	//生产环境不注册logger
	if env != "prod" && env != "production" {
		router.Use(gin.Logger())
	}

	router.Use(corsMiddleware())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	return router
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		//动态返回 Access-Control-Allow-Origin
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}

		c.Header(
			"Access-Control-Allow-Methods",
			"GET, POST, PUT, DELETE, OPTIONS",
		)

		c.Header(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)

		c.Header(
			"Access-Control-Allow-Credentials",
			"true",
		)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func loadAgentTables(paths []string) (map[string]model.AiAgentConfigTable, error) {
	merged := make(map[string]model.AiAgentConfigTable)

	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		//替换path路径里面的环境变量
		expanded := os.ExpandEnv(path)

		tables, err := config.LoadAgentTablesFile(expanded)
		if err != nil {
			return nil, err
		}

		for name, table := range tables {
			merged[name] = table
		}
	}

	return merged, nil
}
