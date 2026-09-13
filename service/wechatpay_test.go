package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 后台文本框粘贴 PEM 时换行经常被压成空格，而 pem.Decode 要求 BEGIN 行后紧跟
// 换行，遇到空格直接返回 nil。规整失败时表现为「拉起支付失败」，看不出是格式
// 问题，因此这里直接以「能否被 pem.Decode 解析」为断言。
func TestNormalizePEMAcceptsMangledLineBreaks(t *testing.T) {
	const body = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnSi2GTKHuJsyP1S2" +
		"bWQ+Hqugdotc7WB2qnpJXP7K2LkYEb7ZmW4OIHH+OoQKTlG88+sUAQ/KPiWg"
	wellFormed := "-----BEGIN PUBLIC KEY-----\n" + body[:64] + "\n" + body[64:] + "\n-----END PUBLIC KEY-----\n"

	cases := []struct {
		name  string
		input string
	}{
		{name: "规范格式", input: wellFormed},
		{name: "换行变空格", input: strings.ReplaceAll(wellFormed, "\n", " ")},
		{name: "全部换行丢失", input: strings.ReplaceAll(wellFormed, "\n", "")},
		{name: "CRLF 换行", input: strings.ReplaceAll(wellFormed, "\n", "\r\n")},
		{name: "首尾多余空白", input: "  \n" + wellFormed + "\n  "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			block, _ := pem.Decode([]byte(normalizePEM(tc.input)))
			require.NotNil(t, block, "规整后必须能被 pem.Decode 解析")
			assert.Equal(t, "PUBLIC KEY", block.Type)
		})
	}
}

// 规整必须保留原始字节，否则密钥会被悄悄改坏。
func TestNormalizePEMPreservesKeyMaterial(t *testing.T) {
	key, err := x509.MarshalPKIXPublicKey(testRSAPublicKey(t))
	require.NoError(t, err)
	wellFormed := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: key}))

	mangled := strings.ReplaceAll(wellFormed, "\n", " ")
	block, _ := pem.Decode([]byte(normalizePEM(mangled)))
	require.NotNil(t, block)
	assert.Equal(t, key, block.Bytes)

	// 已经规范的输入应当是恒等变换。
	assert.Equal(t, wellFormed, normalizePEM(wellFormed))
}

// 非 PEM 内容原样返回，交给上游报错，不要在这里吞掉问题。
func TestNormalizePEMLeavesNonPEMAlone(t *testing.T) {
	assert.Equal(t, "not a pem", normalizePEM("  not a pem  "))
	assert.Equal(t, "", normalizePEM("   "))
}

func testRSAPublicKey(t *testing.T) any {
	t.Helper()
	// 仅用于本测试的编解码往返，不参与任何真实签名。
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return &key.PublicKey
}
