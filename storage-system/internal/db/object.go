package db

import (
	"fmt"
	"time"
)

type Object struct {
	ID          int64     `json:"id"`
	BucketName  string    `json:"bucket_name"`
	ObjectKey   string    `json:"object_key"`
	FilePath    string    `json:"file_path"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	ETag        string    `json:"etag"`
	CreatedAt   time.Time `json:"created_at"`
}

// 保存对象元数据
func CreateObject(bucketName, objectKey, filePath string, size int64, contentType, etag string) error {
	query := `INSERT INTO objects (bucket_name, object_key, file_path, size, content_type, etag) 
	          VALUES (?, ?, ?, ?, ?, ?)`

	_, err := DB.Exec(query, bucketName, objectKey, filePath, size, contentType, etag)
	if err != nil {
		return fmt.Errorf("保存对象失败: %v", err)
	}

	return nil
}

// 获取对象信息
func GetObject(bucketName, objectKey string) (*Object, error) {
	obj := &Object{}
	query := `SELECT id, bucket_name, object_key, file_path, size, content_type, etag, created_at 
	          FROM objects WHERE bucket_name = ? AND object_key = ?`

	err := DB.QueryRow(query, bucketName, objectKey).Scan(
		&obj.ID, &obj.BucketName, &obj.ObjectKey, &obj.FilePath,
		&obj.Size, &obj.ContentType, &obj.ETag, &obj.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("对象不存在: %v", err)
	}

	return obj, nil
}

// 列出桶内对象
func ListObjects(bucketName string) ([]Object, error) {
	query := `SELECT id, bucket_name, object_key, size, content_type, created_at 
	          FROM objects WHERE bucket_name = ? ORDER BY created_at DESC`

	rows, err := DB.Query(query, bucketName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var objects []Object
	for rows.Next() {
		var obj Object
		err := rows.Scan(&obj.ID, &obj.BucketName, &obj.ObjectKey, &obj.Size, &obj.ContentType, &obj.CreatedAt)
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
	}

	return objects, nil
}

// 删除对象
func DeleteObject(bucketName, objectKey string) (*Object, error) {
	// 先获取对象信息，用于返回文件路径以便删除物理文件
	obj, err := GetObject(bucketName, objectKey)
	if err != nil {
		return nil, err
	}

	query := `DELETE FROM objects WHERE bucket_name = ? AND object_key = ?`
	_, err = DB.Exec(query, bucketName, objectKey)
	if err != nil {
		return nil, fmt.Errorf("删除对象失败: %v", err)
	}

	return obj, nil
}
