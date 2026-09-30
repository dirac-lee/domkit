package mysql

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 打开 GORM/MySQL 连接并做连接池调优。
// dsn 需自带 parseTime=true 等参数，例如：
//
//	order:order@tcp(127.0.0.1:3306)/order?charset=utf8mb4&parseTime=True&loc=Local
func Open(dsn string) (*gorm.DB, error) {
	// 日志只打印慢查询/错误，避免示例正常流程被大量 SQL 日志淹没。
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("mysql: open gorm failed: %w", err)
	}

	// 底层 *sql.DB 连接池设置：限制连接数与连接寿命，避免长连接被 MySQL 静默断开。
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("mysql: obtain sql.DB failed: %w", err)
	}
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

// Migrate 依据 PO 自动建表/补列。
// 仅用于示例快速起步；生产环境建议改用版本化迁移工具（如 golang-migrate）。
func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&OrderPO{}, &OrderSummaryPO{}); err != nil {
		return fmt.Errorf("mysql: migrate core tables failed: %w", err)
	}
	if err := db.AutoMigrate(&OutboxMessagePO{}, &BroadcastRecordPO{}); err != nil {
		return fmt.Errorf("mysql: migrate infra tables failed: %w", err)
	}
	return nil
}
