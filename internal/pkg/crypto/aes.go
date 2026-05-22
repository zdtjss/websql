package crypto

import (
	"encoding/base64"
	"log"

	"github.com/forgoer/openssl"
)

var aesKey = []byte("WERTYUIOPKJHGFDS")

func AESEncode(plaintext string) string {

	if plaintext == "" {
		panic("被加密文本不能为空")
	}

	dst, err := openssl.AesECBEncrypt([]byte(plaintext), aesKey, openssl.PKCS7_PADDING)
	if err != nil {
		log.Printf("[Security] 加密失败 - err=%v\n", err)
		panic("加密失败")
	}
	return base64.StdEncoding.EncodeToString(dst)
}

func AESDecode(ciphertext string) string {
	if ciphertext == "" {
		panic("密文本不能为空")
	}
	decodeString, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		log.Printf("[Security] Base64解码失败 - err=%v\n", err)
		panic("无效密文")
	}
	dst, err := openssl.AesECBDecrypt(decodeString, aesKey, openssl.PKCS7_PADDING)
	if err != nil {
		log.Printf("[Security] 解密失败 - err=%v\n", err)
		panic("解密失败")
	}
	return string(dst)
}