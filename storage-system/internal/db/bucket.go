package db

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Bucket struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	AccessType string    `json:"access_type"` // public 或 private
	AccessKey  string    `json:"access_key,omitempty"`
	SecretKey  string    `json:"secret_key,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// 生成随机密钥
func generateKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// 创建桶
func CreateBucket(name, accessType string) (*Bucket, error) {
	var accessKey, secretKey string
	var err error

	// 如果是私有桶，生成访问密钥
	if accessType == "private" {
		accessKey, err = generateKey(16) // 32字符
		if err != nil {
			return nil, err
		}
		secretKey, err = generateKey(32) // 64字符
		if err != nil {
			return nil, err
		}
	}

	query := `INSERT INTO buckets (name, access_type, access_key, secret_key) 
	          VALUES (?, ?, ?, ?)`

	result, err := DB.Exec(query, name, accessType, accessKey, secretKey)
	if err != nil {
		return nil, fmt.Errorf("创建桶失败: %v", err)
	}

	id, _ := result.LastInsertId()

	return &Bucket{
		ID:         int(id),
		Name:       name,
		AccessType: accessType,
		AccessKey:  accessKey,
		SecretKey:  secretKey,
		CreatedAt:  time.Now(),
	}, nil
}

// 获取桶信息
func GetBucket(name string) (*Bucket, error) {
	bucket := &Bucket{}
	query := `SELECT id, name, access_type, access_key, secret_key, created_at, updated_at 
	          FROM buckets WHERE name = ?`

	err := DB.QueryRow(query, name).Scan(
		&bucket.ID, &bucket.Name, &bucket.AccessType,
		&bucket.AccessKey, &bucket.SecretKey,
		&bucket.CreatedAt, &bucket.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("桶不存在: %v", err)
	}

	return bucket, nil
}

// 列出所有桶
func ListBuckets() ([]Bucket, error) {
	query := `SELECT id, name, access_type, created_at FROM buckets ORDER BY created_at DESC`

	rows, err := DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []Bucket
	for rows.Next() {
		var b Bucket
		err := rows.Scan(&b.ID, &b.Name, &b.AccessType, &b.CreatedAt)
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}

	return buckets, nil
}

// 删除桶
func DeleteBucket(name string) error {
	query := `DELETE FROM buckets WHERE name = ?`
	result, err := DB.Exec(query, name)
	if err != nil {
		return fmt.Errorf("删除桶失败: %v", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("桶不存在")
	}

	return nil
}
