package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	Algorithm   = "AWS4-HMAC-SHA256"
	Service     = "s3"
	Region      = "us-east-1"
	RequestType = "aws4_request"
	// 时间窗口：15分钟
	TimeWindow = 15 * time.Minute
)

// 验证签名（修复Host头问题）
func VerifySignature(r *http.Request, secretKey string, requestBody []byte) bool {
	fmt.Println("\n========== 签名验证调试信息 ==========")

	authHeader := r.Header.Get("Authorization")
	fmt.Printf("1. Authorization头: %s\n", authHeader)

	if authHeader == "" {
		fmt.Println("❌ Authorization头为空")
		return false
	}

	// 解析 Authorization 头
	authParts := parseAuthHeader(authHeader)
	if authParts == nil {
		fmt.Println("❌ 解析Authorization头失败")
		return false
	}

	providedSignature := authParts["Signature"]
	signedHeaders := authParts["SignedHeaders"]
	credential := authParts["Credential"]

	fmt.Printf("2. 提供的签名: %s\n", providedSignature)
	fmt.Printf("3. 已签名头: %s\n", signedHeaders)
	fmt.Printf("4. 凭证: %s\n", credential)

	// 从凭证中提取日期
	credParts := strings.Split(credential, "/")
	if len(credParts) < 2 {
		fmt.Println("❌ 凭证格式错误")
		return false
	}
	dateStamp := credParts[1]
	fmt.Printf("5. 日期戳: %s\n", dateStamp)

	// 获取请求时间
	amzDate := r.Header.Get("x-amz-date")
	fmt.Printf("6. x-amz-date: %s\n", amzDate)

	if amzDate == "" {
		dateHeader := r.Header.Get("Date")
		if dateHeader == "" {
			fmt.Println("❌ 缺少时间戳")
			return false
		}
		t, err := time.Parse(time.RFC1123, dateHeader)
		if err != nil {
			fmt.Printf("❌ 解析Date头失败: %v\n", err)
			return false
		}
		amzDate = t.Format("20060102T150405Z")
	}

	// 验证时间戳
	requestTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		fmt.Printf("❌ 解析时间戳失败: %v\n", err)
		return false
	}

	now := time.Now().UTC()
	timeDiff := now.Sub(requestTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	fmt.Printf("7. 当前服务器时间: %s\n", now.Format("20060102T150405Z"))
	fmt.Printf("8. 请求时间: %s\n", amzDate)
	fmt.Printf("9. 时间差: %.2f 秒\n", timeDiff.Seconds())

	if timeDiff > TimeWindow {
		fmt.Printf("❌ 签名已过期（超过 %.0f 分钟）\n", TimeWindow.Minutes())
		return false
	}

	// 1. 创建规范请求
	canonicalRequest := createCanonicalRequest(r, signedHeaders, requestBody)
	fmt.Printf("10. 规范请求:\n---开始---\n%s\n---结束---\n", canonicalRequest)

	// 2. 创建待签名字符串
	stringToSign := createStringToSign(amzDate, dateStamp, canonicalRequest)
	fmt.Printf("11. 待签名字符串:\n---开始---\n%s\n---结束---\n", stringToSign)

	// 3. 计算签名
	expectedSignature := calculateSignatureV4(secretKey, dateStamp, stringToSign)
	fmt.Printf("12. 期望的签名: %s\n", expectedSignature)
	fmt.Printf("13. 提供的签名: %s\n", providedSignature)

	// 4. 比较签名
	match := hmac.Equal([]byte(expectedSignature), []byte(providedSignature))
	if match {
		fmt.Println("✅ 签名验证成功")
	} else {
		fmt.Println("❌ 签名不匹配")
	}
	fmt.Println("========================================\n")

	return match
}

// 解析 Authorization 头
func parseAuthHeader(authHeader string) map[string]string {
	result := make(map[string]string)

	if !strings.HasPrefix(authHeader, Algorithm) {
		return nil
	}

	authHeader = strings.TrimPrefix(authHeader, Algorithm+" ")
	parts := strings.Split(authHeader, ", ")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}

	return result
}

// 创建规范请求
func createCanonicalRequest(r *http.Request, signedHeaders string, requestBody []byte) string {
	method := r.Method

	canonicalURI := r.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalQueryString := createCanonicalQueryString(r)
	canonicalHeaders := createCanonicalHeaders(r, signedHeaders)
	signedHeadersList := signedHeaders
	payloadHash := sha256Hash(requestBody)

	canonicalRequest := method + "\n" +
		canonicalURI + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeadersList + "\n" +
		payloadHash

	return canonicalRequest
}

// 创建规范查询字符串
func createCanonicalQueryString(r *http.Request) string {
	query := r.URL.Query()
	if len(query) == 0 {
		return ""
	}

	var keys []string
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		values := query[k]
		sort.Strings(values)
		for _, v := range values {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return strings.Join(parts, "&")
}

// 创建规范头（修复版本 - 正确处理Host头）
func createCanonicalHeaders(r *http.Request, signedHeaders string) string {
	headers := strings.Split(signedHeaders, ";")
	var canonicalHeaders []string

	for _, h := range headers {
		h = strings.TrimSpace(strings.ToLower(h))

		var value string
		// 特殊处理Host头 - 从r.Host获取而不是r.Header
		if h == "host" {
			value = r.Host
			if value == "" {
				value = r.Header.Get("Host")
			}
		} else {
			value = r.Header.Get(h)
		}

		// 规范化值
		value = strings.TrimSpace(value)
		value = strings.Join(strings.Fields(value), " ")
		canonicalHeaders = append(canonicalHeaders, fmt.Sprintf("%s:%s", h, value))
	}

	return strings.Join(canonicalHeaders, "\n") + "\n"
}

// 创建待签名字符串
func createStringToSign(amzDate, dateStamp, canonicalRequest string) string {
	credentialScope := fmt.Sprintf("%s/%s/%s/%s", dateStamp, Region, Service, RequestType)
	hashedCanonicalRequest := sha256Hash([]byte(canonicalRequest))

	stringToSign := Algorithm + "\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		hashedCanonicalRequest

	return stringToSign
}

// 计算 AWS 签名 V4
func calculateSignatureV4(secretKey, dateStamp, stringToSign string) string {
	kSecret := []byte("AWS4" + secretKey)
	kDate := hmacSHA256(kSecret, []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(Region))
	kService := hmacSHA256(kRegion, []byte(Service))
	kSigning := hmacSHA256(kService, []byte(RequestType))
	signature := hmacSHA256(kSigning, []byte(stringToSign))
	return hex.EncodeToString(signature)
}

// HMAC-SHA256 辅助函数
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// SHA256 哈希
func sha256Hash(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// 从Authorization头提取AccessKey
func ExtractAccessKey(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	credIndex := strings.Index(authHeader, "Credential=")
	if credIndex == -1 {
		return ""
	}

	credPart := authHeader[credIndex+len("Credential="):]
	accessKey := strings.Split(credPart, "/")[0]

	return accessKey
}
