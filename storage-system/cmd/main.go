package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"storage-system/internal/handler"
	"storage-system/internal/s3api"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
)

func main() {
	// 数据库连接（用于 S3 API）
	dsn := "root:123456@tcp(127.0.0.1:3306)/storage_db?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("数据库连接失败:", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatal("数据库 Ping 失败:", err)
	}

	// 设置数据目录
	dataDir := "data"
	handler.SetDataDir(dataDir)

	// 设置 S3 API 的数据目录和数据库
	s3api.SetDataDir(dataDir)
	s3api.SetDB(db)

	// 创建路由
	r := mux.NewRouter()

	// ========== S3 API 路由（新增）==========
	s3Router := r.PathPrefix("/s3").Subrouter()
	s3api.RegisterRoutes(s3Router)

	// ========== 原有 API 路由 ==========
	// 桶管理
	r.HandleFunc("/{bucket}", handler.CreateBucketHandler).Methods("PUT")
	r.HandleFunc("/{bucket}", handler.DeleteBucketHandler).Methods("DELETE")
	r.HandleFunc("/", handler.ListBucketsHandler).Methods("GET")

	// 对象管理
	r.HandleFunc("/{bucket}/{object:.+}", handler.PutObjectHandler).Methods("PUT")
	r.HandleFunc("/{bucket}/{object:.+}", handler.GetObjectHandler).Methods("GET")
	r.HandleFunc("/{bucket}/{object:.+}", handler.DeleteObjectHandler).Methods("DELETE")
	r.HandleFunc("/{bucket}/", handler.ListObjectsHandler).Methods("GET")

	// 启动服务器
	fmt.Println("========================================")
	fmt.Println("服务器启动成功！")
	fmt.Println("原有 API: http://localhost:8080")
	fmt.Println("S3 API:   http://localhost:8080/s3")
	fmt.Println("========================================")
	log.Fatal(http.ListenAndServe(":8080", r))
}
