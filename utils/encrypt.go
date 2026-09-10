package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

func GetPublicKeyEncrypted(publicKey string, text string) (string, error) {
	// 解码公钥
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	// 转换为 RSA 公钥
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("failed to parse RSA public key")
	}

	// 使用 RSA 公钥加密文本
	encryptedBytes, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(text))
	if err != nil {
		return "", err
	}

	// Base64 编码加密后的数据
	encryptedText := base64.StdEncoding.EncodeToString(encryptedBytes)
	return encryptedText, nil
}

func GetPrivateKeyDecrypted(privateKey string, encryptedText string) (string, error) {
	// 解码私钥
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", fmt.Errorf("failed to decode private key")
	}

	// 解析私钥
	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	privKey := priv.(*rsa.PrivateKey)

	// 解码 Base64 编码的加密数据
	encryptedBytes, err := base64.StdEncoding.DecodeString(encryptedText)
	if err != nil {
		return "", err
	}

	// 使用 RSA 私钥解密数据
	decryptedBytes, err := rsa.DecryptPKCS1v15(rand.Reader, privKey, encryptedBytes)
	if err != nil {
		return "", err
	}

	// 将解密后的数据转换为字符串
	decryptedText := string(decryptedBytes)
	return decryptedText, nil
}
