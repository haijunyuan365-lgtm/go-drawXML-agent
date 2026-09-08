package persistence

import (
	"fmt"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MySQLConfig 是 persistence 适配器自己的最小连接配置。
// Required=true 时，DSN 不能为空；Required=false 且 DSN 为空时允许跳过连接。
type MySQLConfig struct {
	Required bool
	DSN      string
}

// OpenMySQL 创建 GORM 的 MySQL 客户端，并通过 Ping 验证数据库真实可用。
func OpenMySQL(config MySQLConfig) (*gorm.DB, error) {
	if strings.TrimSpace(config.DSN) == "" {
		if config.Required {
			return nil, fmt.Errorf("mysql dsn is required")
		}

		// MySQL 是可选依赖，并且没有配置 DSN，直接跳过。
		return nil, nil
	}

	// gorm.Open 负责创建 GORM 上层对象。
	db, err := gorm.Open(
		mysql.Open(config.DSN),
		&gorm.Config{},
	)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	// 取出底层 *sql.DB。
	// 只有执行 Ping，才能确认网络、账号密码和数据库名称都真实可用。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("mysql db handle: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}
