package handler

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"storage-system/internal/auth"
	"storage-system/internal/db"
	"strings"

	"github.com/gorilla/mux"
)

var dataDir string

func SetDataDir(dir string) {
	dataDir = dir
}

// 创建桶
func CreateBucketHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	var req struct {
		AccessType string `json:"access_type"` // public 或 private
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求体", http.StatusBadRequest)
		return
	}

	if req.AccessType != "public" && req.AccessType != "private" {
		req.AccessType = "private"
	}

	bucket, err := db.CreateBucket(bucketName, req.AccessType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 创建桶的物理目录
	bucketPath := filepath.Join(dataDir, bucketName)
	os.MkdirAll(bucketPath, 0755)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(bucket)
}

// 列出桶
func ListBucketsHandler(w http.ResponseWriter, r *http.Request) {
	buckets, err := db.ListBuckets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buckets)
}

// 删除桶
func DeleteBucketHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	err := db.DeleteBucket(bucketName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// 删除桶的物理目录
	bucketPath := filepath.Join(dataDir, bucketName)
	os.RemoveAll(bucketPath)

	w.WriteHeader(http.StatusNoContent)
}

// 上传对象
func PutObjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// 获取桶信息
	bucket, err := db.GetBucket(bucketName)
	if err != nil {
		http.Error(w, "桶不存在", http.StatusNotFound)
		return
	}

	// 读取请求体
	bodyData, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求体失败", http.StatusInternalServerError)
		return
	}

	// 如果是私有桶,验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			http.Error(w, "访问密钥不匹配", http.StatusForbidden)
			return
		}

		// 使用新的签名验证方法
		if !auth.VerifySignature(r, bucket.SecretKey, bodyData) {
			http.Error(w, "签名验证失败", http.StatusForbidden)
			return
		}
	}

	// 创建文件路径
	objectPath := filepath.Join(dataDir, bucketName, objectKey)
	os.MkdirAll(filepath.Dir(objectPath), 0755)

	// 保存文件
	file, err := os.Create(objectPath)
	if err != nil {
		http.Error(w, "创建文件失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 计算MD5并写入文件
	hash := md5.New()
	size, err := io.Copy(file, io.MultiReader(strings.NewReader(string(bodyData)), io.TeeReader(strings.NewReader(string(bodyData)), hash)))
	if err != nil {
		// 直接写入
		size = int64(len(bodyData))
		file.Write(bodyData)
	} else {
		file.Write(bodyData)
	}

	// 重新计算MD5
	hash = md5.New()
	hash.Write(bodyData)
	etag := hex.EncodeToString(hash.Sum(nil))

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 保存元数据
	err = db.CreateObject(bucketName, objectKey, objectPath, size, contentType, etag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "对象上传成功")
}

// 下载对象
func GetObjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// 获取桶信息
	bucket, err := db.GetBucket(bucketName)
	if err != nil {
		http.Error(w, "桶不存在", http.StatusNotFound)
		return
	}

	// 如果是私有桶,验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			http.Error(w, "访问密钥不匹配", http.StatusForbidden)
			return
		}

		// GET请求没有body,传递空slice
		if !auth.VerifySignature(r, bucket.SecretKey, []byte{}) {
			http.Error(w, "签名验证失败", http.StatusForbidden)
			return
		}
	}

	// 获取对象信息
	obj, err := db.GetObject(bucketName, objectKey)
	if err != nil {
		http.Error(w, "对象不存在", http.StatusNotFound)
		return
	}

	// 打开文件
	file, err := os.Open(obj.FilePath)
	if err != nil {
		http.Error(w, "读取文件失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 设置响应头
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", obj.ETag)

	// 发送文件内容
	io.Copy(w, file)
}

// 列出对象
func ListObjectsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	objects, err := db.ListObjects(bucketName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(objects)
}

// 删除对象
func DeleteObjectHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// 获取桶信息
	bucket, err := db.GetBucket(bucketName)
	if err != nil {
		http.Error(w, "桶不存在", http.StatusNotFound)
		return
	}

	// 如果是私有桶,验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			http.Error(w, "访问密钥不匹配", http.StatusForbidden)
			return
		}

		// DELETE请求没有body,传递空slice
		if !auth.VerifySignature(r, bucket.SecretKey, []byte{}) {
			http.Error(w, "签名验证失败", http.StatusForbidden)
			return
		}
	}

	obj, err := db.DeleteObject(bucketName, objectKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// 删除物理文件
	os.Remove(obj.FilePath)

	w.WriteHeader(http.StatusNoContent)
}
