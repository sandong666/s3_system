package db

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

// 初始化数据库连接
func InitDB(host, port, user, password, database string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4",
		user, password, host, port, database)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}

	// 测试连接
	err = DB.Ping()
	if err != nil {
		return fmt.Errorf("连接数据库失败: %v", err)
	}

	// 设置连接池参数
	DB.SetMaxOpenConns(100)
	DB.SetMaxIdleConns(10)

	fmt.Println("✓ 数据库连接成功")
	return nil
}

// 关闭数据库连接
func Close() {
	if DB != nil {
		DB.Close()
	}
}
