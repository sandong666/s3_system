package main

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"
)

const (
	Algorithm   = "AWS4-HMAC-SHA256"
	Service     = "s3"
	Region      = "us-east-1"
	RequestType = "aws4_request"
)

func main() {
	// 命令行参数
	method := flag.String("method", "PUT", "HTTP方法(GET, PUT, DELETE)")
	bucket := flag.String("bucket", "", "桶名称")
	object := flag.String("object", "", "对象名称")
	accessKey := flag.String("ak", "", "Access Key")
	secretKey := flag.String("sk", "", "Secret Key")
	file := flag.String("file", "", "要上传的文件路径(仅PUT方法需要)")

	flag.Parse()

	if *bucket == "" || *accessKey == "" || *secretKey == "" {
		fmt.Println("使用方法:")
		fmt.Println("  生成PUT签名: go run sign_tool_windows.go -method=PUT -bucket=桶名 -object=对象名 -ak=访问密钥 -sk=秘密密钥 -file=文件路径")
		fmt.Println("  生成GET签名: go run sign_tool_windows.go -method=GET -bucket=桶名 -object=对象名 -ak=访问密钥 -sk=秘密密钥")
		fmt.Println("  生成DELETE签名: go run sign_tool_windows.go -method=DELETE -bucket=桶名 -object=对象名 -ak=访问密钥 -sk=秘密密钥")
		os.Exit(1)
	}

	// 生成时间戳
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	// 构建URI
	uri := fmt.Sprintf("/%s", *bucket)
	if *object != "" {
		uri = fmt.Sprintf("/%s/%s", *bucket, *object)
	}

	// 读取文件内容(如果是PUT方法)
	var payloadHash string
	if *method == "PUT" && *file != "" {
		fileData, err := os.ReadFile(*file)
		if err != nil {
			fmt.Printf("读取文件失败: %v\n", err)
			os.Exit(1)
		}
		payloadHash = sha256Hash(fileData)
	} else {
		payloadHash = sha256Hash([]byte{})
	}

	// 1. 创建规范请求
	canonicalRequest := fmt.Sprintf("%s\n%s\n\nhost:localhost:8080\nx-amz-date:%s\n\nhost;x-amz-date\n%s",
		*method,
		uri,
		amzDate,
		payloadHash,
	)

	// 2. 创建待签名字符串
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, Region, Service, RequestType)
	hashedCanonicalRequest := sha256Hash([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		Algorithm,
		amzDate,
		credentialScope,
		hashedCanonicalRequest,
	)

	// 3. 计算签名
	signature := calculateSignatureV4(*secretKey, dateStamp, stringToSign)

	// 4. 构建Authorization头
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=host;x-amz-date, Signature=%s",
		Algorithm,
		*accessKey,
		credentialScope,
		signature,
	)

	// 输出curl命令
	fmt.Println("\n========== AWS S3 签名信息 ==========")
	fmt.Printf("时间戳: %s\n", amzDate)
	fmt.Printf("Authorization: %s\n", authorization)

	fmt.Println("\n========== Windows CMD 命令（单行，直接复制执行）==========")

	if *method == "PUT" && *file != "" {
		// 计算文件MD5
		fileData, _ := os.ReadFile(*file)
		md5Hash := md5.Sum(fileData)
		md5Hex := hex.EncodeToString(md5Hash[:])

		fmt.Printf("curl -X %s http://localhost:8080%s -H \"Host: localhost:8080\" -H \"x-amz-date: %s\" -H \"Authorization: %s\" -H \"Content-Type: application/octet-stream\" -H \"Content-MD5: %s\" --data-binary \"@%s\"\n",
			*method, uri, amzDate, authorization, md5Hex, *file)
	} else if *method == "GET" {
		fmt.Printf("curl -X %s http://localhost:8080%s -H \"Host: localhost:8080\" -H \"x-amz-date: %s\" -H \"Authorization: %s\"\n",
			*method, uri, amzDate, authorization)
	} else if *method == "DELETE" {
		fmt.Printf("curl -X %s http://localhost:8080%s -H \"Host: localhost:8080\" -H \"x-amz-date: %s\" -H \"Authorization: %s\"\n",
			*method, uri, amzDate, authorization)
	}

	fmt.Println("\n========================================")
}

func calculateSignatureV4(secretKey, dateStamp, stringToSign string) string {
	kSecret := []byte("AWS4" + secretKey)
	kDate := hmacSHA256(kSecret, []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(Region))
	kService := hmacSHA256(kRegion, []byte(Service))
	kSigning := hmacSHA256(kService, []byte(RequestType))
	signature := hmacSHA256(kSigning, []byte(stringToSign))
	return hex.EncodeToString(signature)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256Hash(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
