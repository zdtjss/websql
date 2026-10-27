package fileparse

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// 文本编码探测与转换。
//
// 背景：Windows 环境下 Excel 导出的 CSV、记事本保存的 TXT 常为 GBK/GB18030，
// 直接按 UTF-8 读取会得到乱码；UTF-16（带 BOM）同样需要识别。
// 顺序：BOM 判定 → UTF-8 合法性校验 → GB18030 → 兜底按 UTF-8 宽松替换。

// utf8BOM UTF-8 字节序标记
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decodeText 将原始字节解码为 UTF-8 文本。
//
// 返回值第二项为探测到的编码名（用于日志/Notice）。
func decodeText(raw []byte) (string, string) {
	// 1. BOM 判定
	switch {
	case bytes.HasPrefix(raw, utf8BOM):
		return string(raw[len(utf8BOM):]), "utf-8-bom"
	case bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		if s, err := transformBytes(raw[2:], unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder()); err == nil {
			return s, "utf-16le"
		}
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}):
		if s, err := transformBytes(raw[2:], unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder()); err == nil {
			return s, "utf-16be"
		}
	}

	// 2. 合法 UTF-8 直接使用
	if utf8.Valid(raw) {
		return string(raw), "utf-8"
	}

	// 3. 尝试 GB18030（GBK 超集）：解码后若仍含替换字符，视为不可信
	if s, err := transformBytes(raw, simplifiedchinese.GB18030.NewDecoder()); err == nil && !strings.ContainsRune(s, utf8.RuneError) {
		return s, "gb18030"
	}

	// 4. 兜底：按 UTF-8 宽松替换，保证不返回损坏的字节序列
	return strings.ToValidUTF8(string(raw), "\uFFFD"), "utf-8-lossy"
}

// transformBytes 使用指定 transformer 转换字节序列。
func transformBytes(raw []byte, t transform.Transformer) (string, error) {
	out, _, err := transform.Bytes(t, raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
