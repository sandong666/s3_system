package db

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

var db *sql.DB

// SetDB 设置数据库连接（用于外部传入）
func SetDB(database *sql.DB) {
	db = database
}

// GetDB 获取数据库连接
func GetDB() *sql.DB {
	return db
}
