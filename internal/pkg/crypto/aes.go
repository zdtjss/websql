package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"strings"
	"sync"

	"websql/internal/config"

	"github.com/forgoer/openssl"
)

// gcmPrefix 用于区分新版 GCM 密文与旧版 ECB 密文。
// 新加密产物形如 "g1:<base64(nonce||ciphertext||tag)>"。
// 解密时：带前缀走 GCM，否则回退旧 ECB（兼容存量数据）。
const gcmPrefix = "g1:"

// defaultAESKey 仅在配置未提供密钥时使用，用于兼容存量数据，不安全。
var defaultAESKey = []byte("WERTYUIOPKJHGFDS")

var (
	aesKeyOnce sync.Once
	aesKey     []byte
)

// getAESKey 懒加载 AES 密钥：优先从注入配置的 Security.AESKey 读取（base64 解码），
// 缺失或非法时回退到默认密钥并打印告警。
func getAESKey() []byte {
	aesKeyOnce.Do(func() {
		cfg := config.Get()
		if cfg != nil && cfg.Security.AESKey != "" {
			if k, err := base64.StdEncoding.DecodeString(cfg.Security.AESKey); err == nil && (len(k) == 16 || len(k) == 24 || len(k) == 32) {
				aesKey = k
				return
			}
			log.Println("[Security] config.security.aesKey 非法（需 16/24/32 字节密钥的 base64），回退默认密钥")
		}
		aesKey = defaultAESKey
		log.Println("[Security] AES 密钥未配置，使用内置默认密钥（不安全），请通过 config.json security.aesKey 配置 16/24/32 字节密钥的 base64")
	})
	return aesKey
}

// aesGCMEncrypt 使用 AES-GCM 加密，返回 "g1:" + base64(nonce||ciphertext||tag)。
func aesGCMEncrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return gcmPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// aesGCMDecrypt 解密 "g1:" + base64(nonce||ciphertext||tag) 格式的密文。
func aesGCMDecrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, gcmPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("invalid ciphertext length")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// AESEncode 加密明文。新数据统一使用 AES-GCM，密文带 "g1:" 前缀。
// 保留 panic 语义以兼容现有调用方（阶段 1 响应统一时再改为返回 error）。
func AESEncode(plaintext string) string {
	if plaintext == "" {
		panic("被加密文本不能为空")
	}
	key := getAESKey()
	result, err := aesGCMEncrypt(plaintext, key)
	if err != nil {
		log.Printf("[Security] GCM 加密失败 - err=%v\n", err)
		panic("加密失败")
	}
	return result
}

// AESDecode 解密密文。自动识别新版 GCM（"g1:" 前缀）与旧版 ECB（无前缀），
// 旧 ECB 仅用于解密存量数据，不再用于新加密。
// 保留 panic 语义以兼容现有调用方（阶段 1 响应统一时再改为返回 error）。
func AESDecode(ciphertext string) string {
	if ciphertext == "" {
		panic("密文本不能为空")
	}
	key := getAESKey()

	// 新版 GCM 密文
	if strings.HasPrefix(ciphertext, gcmPrefix) {
		plaintext, err := aesGCMDecrypt(ciphertext, key)
		if err != nil {
			log.Printf("[Security] GCM 解密失败 - err=%v\n", err)
			panic("解密失败")
		}
		return plaintext
	}

	// 旧版 ECB 密文（兼容存量数据）
	decodeString, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		log.Printf("[Security] Base64解码失败 - err=%v\n", err)
		panic("无效密文")
	}
	dst, err := openssl.AesECBDecrypt(decodeString, key, openssl.PKCS7_PADDING)
	if err != nil {
		log.Printf("[Security] ECB 解密失败 - err=%v\n", err)
		panic("解密失败")
	}
	return string(dst)
}
