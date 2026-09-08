package main

import (
	"ai-agent-scaffold/internal/app/config"
	"context"
	"flag"
	"log"
	"os"

	"ai-agent-scaffold/internal/app/bootstrap"
	"ai-agent-scaffold/internal/infrastructure/logging"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	envPath := flag.String(
		"env",                              //参数名称,
		defaultEnv("APP_ENV_FILE", ".env"), //默认值,
		"path to dotenv file (use empty string to skip)", //参数说明,
	)

	configPath := flag.String(
		"config",
		defaultEnv("APP_CONFIG", "configs/application.yaml"),
		"path to application.yaml",
	)

	flag.Parse()

	envLoaded := loadDotenv(*envPath)

	appCfg, err := config.LoadApplication(*configPath)
	if err != nil {
		log.Fatalf("load application config: %v", err)
	}

	logger, err := logging.New(appCfg.App.Env)
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	//程序退出前调用 Sync()，可以降低最后几条日志还没写完就退出的风险
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info(
		"ai-agent-scaffold-go bootstrap",
		zap.String("config", *configPath),
		zap.String("env", appCfg.App.Env),
		zap.String("addr", appCfg.Server.Addr),
		zap.String("env_file", envLoaded),
	)

	engine, err := bootstrap.Build(context.Background(), appCfg, logger)
	if err != nil {
		logger.Fatal(
			"bootstrap failed",
			zap.Error(err),
		)
	}
	//启动gin
	if err := engine.Run(); err != nil {
		logger.Fatal(
			"http server stopped",
			zap.Error(err),
		)
	}
}

func defaultEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

// loadDotenv loads variables from a dotenv file if it exists. It returns the
// resolved path (or "" if no file was loaded). Existing process environment
// variables always win over file values, so callers can override locally.
func loadDotenv(path string) string {
	if path == "" {
		return ""
	}

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ""
		}

		log.Fatalf("stat dotenv %s: %v", path, err)
	}

	pairs, err := godotenv.Read(path)
	if err != nil {
		log.Fatalf("read dotenv %s: %v", path, err)
	}

	//如果操作系统已经有这个环境变量就不使用 .env 中的值覆盖它
	for key, value := range pairs {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		if err := os.Setenv(key, value); err != nil {
			log.Fatalf("set env %s: %v", key, err)
		}
	}

	return path
}
