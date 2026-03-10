package s3api

import (
	"crypto/md5"
	"crypto/rand" // ← 新增
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"storage-system/internal/auth"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var (
	dataDir string
	db      *sql.DB
)

// SetDataDir 设置数据目录
func SetDataDir(dir string) {
	dataDir = dir
}

// SetDB 设置数据库连接
func SetDB(database *sql.DB) {
	db = database
}

// RegisterRoutes 注册 S3 API 路由
func RegisterRoutes(r *mux.Router) {
	// 列出所有桶
	r.HandleFunc("/", handleListBuckets).Methods("GET")

	// 桶操作
	r.HandleFunc("/{bucket}", handleBucketOperation).Methods("GET", "PUT", "DELETE", "HEAD")

	// 对象操作
	r.HandleFunc("/{bucket}/{object:.+}", handleObjectOperation).Methods("GET", "PUT", "DELETE", "HEAD")
}

// dbBucket 数据库桶结构（内部使用）
type dbBucket struct {
	ID         int
	Name       string
	AccessType string
	AccessKey  string
	SecretKey  string
	CreatedAt  time.Time
}

// dbObject 数据库对象结构（内部使用）
type dbObject struct {
	ID          int
	BucketName  string
	ObjectKey   string
	FilePath    string
	Size        int64
	ContentType string
	ETag        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// handleListBuckets 列出所有桶
func handleListBuckets(w http.ResponseWriter, r *http.Request) {
	query := `SELECT id, name, access_type, access_key, secret_key, created_at FROM buckets`
	rows, err := db.Query(query)
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), r.URL.Path, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := ListAllMyBucketsResult{
		Owner: Owner{
			ID:          "admin",
			DisplayName: "admin",
		},
		Buckets: Buckets{
			Bucket: make([]Bucket, 0),
		},
	}

	for rows.Next() {
		var bucket dbBucket
		err := rows.Scan(&bucket.ID, &bucket.Name, &bucket.AccessType,
			&bucket.AccessKey, &bucket.SecretKey, &bucket.CreatedAt)
		if err != nil {
			continue
		}
		result.Buckets.Bucket = append(result.Buckets.Bucket, Bucket{
			Name:         bucket.Name,
			CreationDate: bucket.CreatedAt,
		})
	}

	writeXMLResponse(w, result, http.StatusOK)
}

// handleBucketOperation 处理桶操作
func handleBucketOperation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	switch r.Method {
	case "GET":
		handleListObjects(w, r, bucketName)
	case "PUT":
		handleCreateBucket(w, r, bucketName)
	case "DELETE":
		handleDeleteBucket(w, r, bucketName)
	case "HEAD":
		handleHeadBucket(w, r, bucketName)
	}
}

// handleCreateBucket 创建桶
func handleCreateBucket(w http.ResponseWriter, r *http.Request, bucketName string) {
	// 默认创建私有桶
	accessType := "private"

	// 可以通过请求头指定访问类型
	if r.Header.Get("x-amz-acl") == "public-read" {
		accessType = "public"
	}

	var accessKey, secretKey string
	if accessType == "private" {
		accessKey = generateAccessKey() // ← 修改
		secretKey = generateSecretKey() // ← 修改
	}

	query := `INSERT INTO buckets (name, access_type, access_key, secret_key) VALUES (?, ?, ?, ?)`
	_, err := db.Exec(query, bucketName, accessType, accessKey, secretKey)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			writeS3Error(w, "BucketAlreadyExists", "桶已存在", "/"+bucketName, http.StatusConflict)
		} else {
			writeS3Error(w, "InternalError", err.Error(), "/"+bucketName, http.StatusInternalServerError)
		}
		return
	}

	// 创建桶的物理目录
	bucketPath := filepath.Join(dataDir, bucketName)
	os.MkdirAll(bucketPath, 0755)

	fmt.Printf("[S3API] 桶已创建: %s (类型: %s)\n", bucketName, accessType)

	// 如果是私有桶，在响应头中返回密钥
	if accessType == "private" {
		w.Header().Set("x-amz-access-key", accessKey)
		w.Header().Set("x-amz-secret-key", secretKey)
	}

	w.WriteHeader(http.StatusOK)
}

// handleDeleteBucket 删除桶
func handleDeleteBucket(w http.ResponseWriter, r *http.Request, bucketName string) {
	// 先删除桶中的所有对象
	db.Exec(`DELETE FROM objects WHERE bucket_name = ?`, bucketName)

	// 删除桶
	_, err := db.Exec(`DELETE FROM buckets WHERE name = ?`, bucketName)
	if err != nil {
		writeS3Error(w, "NoSuchBucket", "桶不存在", "/"+bucketName, http.StatusNotFound)
		return
	}

	// 删除桶的物理目录
	bucketPath := filepath.Join(dataDir, bucketName)
	os.RemoveAll(bucketPath)

	fmt.Printf("[S3API] 桶已删除: %s\n", bucketName)
	w.WriteHeader(http.StatusNoContent)
}

// handleHeadBucket 检查桶是否存在
func handleHeadBucket(w http.ResponseWriter, r *http.Request, bucketName string) {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM buckets WHERE name = ?`, bucketName).Scan(&exists)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleListObjects 列出桶中的对象
func handleListObjects(w http.ResponseWriter, r *http.Request, bucketName string) {
	// 检查桶是否存在
	var exists int
	err := db.QueryRow(`SELECT 1 FROM buckets WHERE name = ?`, bucketName).Scan(&exists)
	if err != nil {
		writeS3Error(w, "NoSuchBucket", "桶不存在", "/"+bucketName, http.StatusNotFound)
		return
	}

	query := `SELECT id, bucket_name, object_key, file_path, size, content_type, etag, created_at, updated_at 
			  FROM objects WHERE bucket_name = ?`
	rows, err := db.Query(query, bucketName)
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), "/"+bucketName, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := ListBucketResult{
		Name:        bucketName,
		Prefix:      r.URL.Query().Get("prefix"),
		MaxKeys:     1000,
		IsTruncated: false,
		Contents:    make([]Contents, 0),
	}

	for rows.Next() {
		var obj dbObject
		err := rows.Scan(&obj.ID, &obj.BucketName, &obj.ObjectKey, &obj.FilePath, &obj.Size,
			&obj.ContentType, &obj.ETag, &obj.CreatedAt, &obj.UpdatedAt)
		if err != nil {
			continue
		}
		result.Contents = append(result.Contents, Contents{
			Key:          obj.ObjectKey,
			LastModified: obj.UpdatedAt,
			ETag:         `"` + obj.ETag + `"`,
			Size:         obj.Size,
			StorageClass: "STANDARD",
		})
	}

	writeXMLResponse(w, result, http.StatusOK)
}

// handleObjectOperation 处理对象操作
func handleObjectOperation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	switch r.Method {
	case "GET":
		handleGetObject(w, r, bucketName, objectKey)
	case "PUT":
		handlePutObject(w, r, bucketName, objectKey)
	case "DELETE":
		handleDeleteObject(w, r, bucketName, objectKey)
	case "HEAD":
		handleHeadObject(w, r, bucketName, objectKey)
	}
}

// handlePutObject 上传对象
func handlePutObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	// 获取桶信息
	var bucket dbBucket
	query := `SELECT id, name, access_type, access_key, secret_key FROM buckets WHERE name = ?`
	err := db.QueryRow(query, bucketName).Scan(&bucket.ID, &bucket.Name, &bucket.AccessType,
		&bucket.AccessKey, &bucket.SecretKey)
	if err != nil {
		writeS3Error(w, "NoSuchBucket", "桶不存在", "/"+bucketName, http.StatusNotFound)
		return
	}

	// 读取请求体
	bodyData, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, "InternalError", "读取请求体失败", "/"+bucketName+"/"+objectKey, http.StatusInternalServerError)
		return
	}

	// 如果是私有桶,验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			writeS3Error(w, "AccessDenied", "访问密钥不匹配", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}

		if !auth.VerifySignature(r, bucket.SecretKey, bodyData) {
			writeS3Error(w, "SignatureDoesNotMatch", "签名验证失败", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}
	}

	// 创建文件路径
	objectPath := filepath.Join(dataDir, bucketName, objectKey)
	os.MkdirAll(filepath.Dir(objectPath), 0755)

	// 保存文件
	err = os.WriteFile(objectPath, bodyData, 0644)
	if err != nil {
		writeS3Error(w, "InternalError", "写入文件失败", "/"+bucketName+"/"+objectKey, http.StatusInternalServerError)
		return
	}

	// 计算 MD5
	hash := md5.New()
	hash.Write(bodyData)
	etag := hex.EncodeToString(hash.Sum(nil))

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 检查对象是否已存在
	var existingID int
	err = db.QueryRow(`SELECT id FROM objects WHERE bucket_name = ? AND object_key = ?`,
		bucketName, objectKey).Scan(&existingID)

	if err == nil {
		// 对象已存在，更新它
		updateQuery := `UPDATE objects SET file_path = ?, size = ?, content_type = ?, etag = ?, updated_at = NOW() 
					   WHERE bucket_name = ? AND object_key = ?`
		_, err = db.Exec(updateQuery, objectPath, int64(len(bodyData)), contentType, etag, bucketName, objectKey)
		if err != nil {
			writeS3Error(w, "InternalError", "更新对象失败", "/"+bucketName+"/"+objectKey, http.StatusInternalServerError)
			return
		}
		fmt.Printf("[S3API] 对象已更新: %s/%s (ETag: %s)\n", bucketName, objectKey, etag)
	} else {
		// 对象不存在，创建新的
		insertQuery := `INSERT INTO objects (bucket_name, object_key, file_path, size, content_type, etag) 
					   VALUES (?, ?, ?, ?, ?, ?)`
		_, err = db.Exec(insertQuery, bucketName, objectKey, objectPath, int64(len(bodyData)), contentType, etag)
		if err != nil {
			writeS3Error(w, "InternalError", "保存对象失败", "/"+bucketName+"/"+objectKey, http.StatusInternalServerError)
			return
		}
		fmt.Printf("[S3API] 对象已创建: %s/%s (ETag: %s)\n", bucketName, objectKey, etag)
	}

	// 返回 ETag
	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
}

// handleGetObject 下载对象
func handleGetObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	// 获取桶信息
	var bucket dbBucket
	query := `SELECT id, name, access_type, access_key, secret_key FROM buckets WHERE name = ?`
	err := db.QueryRow(query, bucketName).Scan(&bucket.ID, &bucket.Name, &bucket.AccessType,
		&bucket.AccessKey, &bucket.SecretKey)
	if err != nil {
		writeS3Error(w, "NoSuchBucket", "桶不存在", "/"+bucketName, http.StatusNotFound)
		return
	}

	// 如果是私有桶，验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			writeS3Error(w, "AccessDenied", "访问密钥不匹配", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}

		if !auth.VerifySignature(r, bucket.SecretKey, []byte{}) {
			writeS3Error(w, "SignatureDoesNotMatch", "签名验证失败", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}
	}

	// 获取对象信息
	var obj dbObject
	objQuery := `SELECT id, bucket_name, object_key, file_path, size, content_type, etag, created_at, updated_at 
				 FROM objects WHERE bucket_name = ? AND object_key = ?`
	err = db.QueryRow(objQuery, bucketName, objectKey).Scan(
		&obj.ID, &obj.BucketName, &obj.ObjectKey, &obj.FilePath, &obj.Size,
		&obj.ContentType, &obj.ETag, &obj.CreatedAt, &obj.UpdatedAt)
	if err != nil {
		writeS3Error(w, "NoSuchKey", "对象不存在", "/"+bucketName+"/"+objectKey, http.StatusNotFound)
		return
	}

	// 打开文件
	file, err := os.Open(obj.FilePath)
	if err != nil {
		writeS3Error(w, "InternalError", "读取文件失败", "/"+bucketName+"/"+objectKey, http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 设置响应头
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", `"`+obj.ETag+`"`)
	w.Header().Set("Last-Modified", obj.UpdatedAt.UTC().Format(http.TimeFormat))

	// 发送文件内容
	io.Copy(w, file)

	fmt.Printf("[S3API] 对象已下载: %s/%s\n", bucketName, objectKey)
}

// handleDeleteObject 删除对象
func handleDeleteObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	// 获取桶信息
	var bucket dbBucket
	query := `SELECT id, name, access_type, access_key, secret_key FROM buckets WHERE name = ?`
	err := db.QueryRow(query, bucketName).Scan(&bucket.ID, &bucket.Name, &bucket.AccessType,
		&bucket.AccessKey, &bucket.SecretKey)
	if err != nil {
		// S3 的删除操作即使桶不存在也返回成功
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 如果是私有桶，验证签名
	if bucket.AccessType == "private" {
		accessKey := auth.ExtractAccessKey(r)
		if accessKey != bucket.AccessKey {
			writeS3Error(w, "AccessDenied", "访问密钥不匹配", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}

		if !auth.VerifySignature(r, bucket.SecretKey, []byte{}) {
			writeS3Error(w, "SignatureDoesNotMatch", "签名验证失败", "/"+bucketName+"/"+objectKey, http.StatusForbidden)
			return
		}
	}

	// 获取对象信息
	var filePath string
	err = db.QueryRow(`SELECT file_path FROM objects WHERE bucket_name = ? AND object_key = ?`,
		bucketName, objectKey).Scan(&filePath)
	if err == nil {
		// 删除数据库记录
		db.Exec(`DELETE FROM objects WHERE bucket_name = ? AND object_key = ?`, bucketName, objectKey)
		// 删除物理文件
		os.Remove(filePath)
		fmt.Printf("[S3API] 对象已删除: %s/%s\n", bucketName, objectKey)
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleHeadObject 获取对象元数据
func handleHeadObject(w http.ResponseWriter, r *http.Request, bucketName, objectKey string) {
	var obj dbObject
	query := `SELECT size, content_type, etag, updated_at FROM objects WHERE bucket_name = ? AND object_key = ?`
	err := db.QueryRow(query, bucketName, objectKey).Scan(&obj.Size, &obj.ContentType, &obj.ETag, &obj.UpdatedAt)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", `"`+obj.ETag+`"`)
	w.Header().Set("Last-Modified", obj.UpdatedAt.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
}

// writeXMLResponse 写入 XML 响应
func writeXMLResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)

	w.Write([]byte(xml.Header))
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	encoder.Encode(data)
}

// writeS3Error 写入 S3 错误响应
func writeS3Error(w http.ResponseWriter, code, message, resource string, statusCode int) {
	err := Error{
		Code:      code,
		Message:   message,
		Resource:  resource,
		RequestId: "request-id",
	}
	writeXMLResponse(w, err, statusCode)
}

// generateAccessKey 生成 Access Key
func generateAccessKey() string {
	timestamp := time.Now().Format("20060102150405")
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("AK-%s-%s", timestamp, randomStr)
}

// generateSecretKey 生成 Secret Key
func generateSecretKey() string {
	timestamp := time.Now().Format("20060102150405")
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("SK-%s-%s", timestamp, randomStr)
}

/* generateKey 生成密钥（保留用于兼容性）
func generateKey() string {
return time.Now().Format("20060102150405") + "-key"
}
*/
