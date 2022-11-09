package https

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const PemName, KeyName = "nway.pem", "nway.key"

func InitCertificateFile() {

	exec, err := os.Executable()
	if err != nil {
		fmt.Println(err)
	}

	permPath := filepath.Join(filepath.Dir(exec), PemName)
	keyPath := filepath.Join(filepath.Dir(exec), KeyName)

	// 如果不需要创建 则直接退出
	if isValid(permPath, keyPath) {
		return
	}

	createCertificateFile(permPath, keyPath)
}

// 创建证书文件
func createCertificateFile(permPath, keyPath string) {

	pair, key, _ := generateKeyPair(time.Hour * 24 * 365 * 10)

	err := os.WriteFile(permPath, pair, os.ModePerm)
	if err != nil {
		println(err)
	}

	err = os.WriteFile(keyPath, key, os.ModePerm)
	if err != nil {
		println(err)
	}
}

// 检查证书是否过期
func isValid(permPath, keyPath string) bool {

	// 证书文件不存在返回false
	if !exists(permPath) || !exists(keyPath) {
		return false
	}

	cer, err := tls.LoadX509KeyPair(PemName, KeyName)
	if err != nil {
		fmt.Println(err)
	}
	cerx509, err := x509.ParseCertificate(cer.Certificate[0])
	if err != nil {
		fmt.Println(err)
	}
	// 是否过期
	return cerx509.NotAfter.After(time.Now())
}

func genCertificate() (cert tls.Certificate, err error) {
	rawCert, rawKey, err := generateKeyPair(time.Hour * 24 * 365 * 10)
	if err != nil {
		return
	}
	return tls.X509KeyPair(rawCert, rawKey)
}

/*
生成密钥对

validFor有效期
*/
func generateKeyPair(validFor time.Duration) (rawCert, rawKey []byte, err error) {

	// Create private key and self-signed certificate
	// Adapted from https://golang.org/src/crypto/tls/generate_cert.go

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	// 有效期
	//validFor := time.Hour * 24 * 365 * 10 // ten years
	notBefore := time.Now()
	notAfter := notBefore.Add(validFor)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, _ := rand.Int(rand.Reader, serialNumberLimit)
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Nway"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		fmt.Println(err)
		return
	}

	rawCert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	rawKey = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return
}

// 判断所给路径文件/文件夹是否存在

func exists(path string) bool {
	_, err := os.Stat(path) //os.Stat获取文件信息
	if err != nil {
		return os.IsExist(err)
	}
	return true
}
