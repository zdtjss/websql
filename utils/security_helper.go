package utils

import (
	"encoding/base64"

	"github.com/forgoer/openssl"
)

var aesKey = []byte("WERTYUIOPKJHGFDS")

func AESEncode(plaintext string) string {

	if plaintext == "" {
		panic("被加密文本不能为空")
	}

	dst, err := openssl.AesECBEncrypt([]byte(plaintext), aesKey, openssl.PKCS7_PADDING)
	if err != nil {
		panic("加密失败，" + err.Error())
	}
	return base64.StdEncoding.EncodeToString(dst)
}

func AESDecode(ciphertext string) string {
	if ciphertext == "" {
		panic("密文本不能为空")
	}
	decodeString, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		var msg any = "无效密文," + err.Error()
		panic(msg)
	}
	dst, err := openssl.AesECBDecrypt(decodeString, aesKey, openssl.PKCS7_PADDING)
	if err != nil {
		var msg any = "解密失败," + err.Error()
		panic(msg)
	}
	return string(dst)
}
