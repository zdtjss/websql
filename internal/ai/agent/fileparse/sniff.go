package fileparse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// 文件头（魔数）校验。
//
// 目的：拦截「扩展名伪装」的明显错误文件（如把 .exe 改名为 .png），
// 在上传阶段就给出明确错误，而不是等到解析时才失败。
// 注意：仅读取头部少量字节，不做内容解析（延迟解析原则）。

const sniffHeaderLen = 16

// Sniff 按扩展名校验磁盘文件头；返回 nil 表示通过。
func Sniff(ext, path string) error {
	head, err := readHead(path)
	if err != nil {
		return fmt.Errorf("读取文件失败，请重试")
	}
	return SniffBytes(ext, head)
}

// SniffBytes 按扩展名校验「文件头字节」；返回 nil 表示通过（文本类无魔数，直接通过）。
//
// 供上传阶段在落盘前校验使用（避免先写盘再删盘）。
func SniffBytes(ext string, head []byte) error {
	ext = NormalizeExt(ext)
	if len(head) == 0 {
		return fmt.Errorf("文件内容为空")
	}

	switch ext {
	case ".docx", ".xlsx":
		if !bytes.HasPrefix(head, []byte("PK\x03\x04")) && !bytes.HasPrefix(head, []byte("PK\x05\x06")) {
			return fmt.Errorf("文件内容与扩展名不符：%s 应为 OOXML（zip）格式，请确认文件未被改名或损坏", ext)
		}
	case ".xls":
		// 旧版 Excel 为 CFB 复合文档
		if !bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
			return fmt.Errorf("文件内容与扩展名不符：%s 应为旧版 Excel 复合文档", ext)
		}
	case ".pdf":
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return fmt.Errorf("文件内容与扩展名不符：不是有效的 PDF 文件")
		}
	case ".jpg", ".jpeg":
		if !bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}) {
			return fmt.Errorf("文件内容与扩展名不符：不是有效的 JPG 图片")
		}
	case ".png":
		if !bytes.HasPrefix(head, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
			return fmt.Errorf("文件内容与扩展名不符：不是有效的 PNG 图片")
		}
	case ".mp4":
		if len(head) < 12 || string(head[4:8]) != "ftyp" {
			return fmt.Errorf("文件内容与扩展名不符：不是有效的 MP4 文件")
		}
	case ".csv", ".txt", ".md", ".markdown":
		// 文本类无固定魔数，跳过
	default:
		// 未登记类型由上层白名单校验拦截
	}
	return nil
}

// readHead 读取文件头部字节（最多 sniffHeaderLen 字节）
func readHead(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, sniffHeaderLen)
	n, err := f.Read(buf)
	if n == 0 && err != nil {
		// 空文件：读出 0 字节属正常情况，交由上层按「内容为空」处理
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	return buf[:n], nil
}
