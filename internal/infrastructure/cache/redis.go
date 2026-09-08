package cache

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// RedisConfig 是 cache 适配器需要的最小 Redis 连接配置。
type RedisConfig struct {
	Required bool
	Addr     string
	Password string
	DB       int
}

// OpenRedis 创建 Redis 客户端，并通过 Ping 验证服务真实可用。
func OpenRedis(config RedisConfig) (*redis.Client, error) {
	if strings.TrimSpace(config.Addr) == "" {
		if config.Required {
			return nil, fmt.Errorf("redis addr is required")
		}

		// Redis 是可选依赖，并且没有配置地址，直接跳过。
		return nil, nil
	}

	// NewClient 只创建客户端对象，本身不会证明 Redis 已经连通。
	client := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
	})

	// Ping 才会真正访问 Redis。
	// 失败时及时关闭客户端，避免资源泄漏。
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
