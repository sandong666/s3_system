package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"storage-system/internal/db"
	"storage-system/internal/handler"

	"github.com/gorilla/mux"
)

type Config struct {
	Server struct {
		Port string `json:"port"`
		Host string `json:"host"`
	} `json:"server"`
	Database struct {
		Host     string `json:"host"`
		Port     string `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		Database string `json:"database"`
	} `json:"database"`
	Storage struct {
		DataDir string `json:"data_dir"`
	} `json:"storage"`
}

func main() {
	// 读取配置文件
	configFile, err := os.ReadFile("../config.json")
	if err != nil {
		log.Fatal("读取配置文件失败:", err)
	}

	var config Config
	err = json.Unmarshal(configFile, &config)
	if err != nil {
		log.Fatal("解析配置文件失败:", err)
	}

	// 初始化数据库
	err = db.InitDB(
		config.Database.Host,
		config.Database.Port,
		config.Database.User,
		config.Database.Password,
		config.Database.Database,
	)
	if err != nil {
		log.Fatal("数据库初始化失败:", err)
	}
	defer db.Close()

	// 设置存储目录
	handler.SetDataDir(config.Storage.DataDir)
	os.MkdirAll(config.Storage.DataDir, 0755)

	// 创建路由
	router := mux.NewRouter()

	// 桶操作
	router.HandleFunc("/buckets", handler.ListBucketsHandler).Methods("GET")
	router.HandleFunc("/{bucket}", handler.CreateBucketHandler).Methods("PUT")
	router.HandleFunc("/{bucket}", handler.DeleteBucketHandler).Methods("DELETE")

	// 对象操作
	router.HandleFunc("/{bucket}/objects", handler.ListObjectsHandler).Methods("GET")
	router.HandleFunc("/{bucket}/{object:.*}", handler.PutObjectHandler).Methods("PUT")
	router.HandleFunc("/{bucket}/{object:.*}", handler.GetObjectHandler).Methods("GET")
	router.HandleFunc("/{bucket}/{object:.*}", handler.DeleteObjectHandler).Methods("DELETE")

	// 启动服务器
	addr := fmt.Sprintf("%s:%s", config.Server.Host, config.Server.Port)
	fmt.Printf("\n========================================\n")
	fmt.Printf("✓ 服务器启动成功\n")
	fmt.Printf("✓ 监听地址: http://localhost:%s\n", config.Server.Port)
	fmt.Printf("========================================\n\n")

	log.Fatal(http.ListenAndServe(addr, router))
}
