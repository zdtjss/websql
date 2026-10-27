package fileparse

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器
	"io"
	"os"
)

func init() {
	Register("go-image", 0, nil, []string{".jpg", ".jpeg", ".png"}, ParserFunc(parseImage))
}

// ImageInfo 图片元信息
type ImageInfo struct {
	MIME   string
	Width  int
	Height int
	Format string
}

// DetectImage 读取图片文件头部并校验尺寸（仅读头，不解码整图）。
//
// 同时供上传阶段的魔数/结构校验使用：上传阶段只做「头信息校验」，
// 不做内容解析（延迟解析原则）。
func DetectImage(path string) (*ImageInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，请重试")
	}
	defer f.Close()
	return DetectImageReader(f)
}

// DetectImageReader 从任意 Reader 读取图片头并校验尺寸。
//
// 供上传阶段在「落盘前」用头部缓冲校验使用，避免先写盘再删盘。
func DetectImageReader(r io.Reader) (*ImageInfo, error) {
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		return nil, fmt.Errorf("图片文件损坏或不是有效的 JPG/PNG 文件")
	}
	switch format {
	case "jpeg", "png":
	default:
		return nil, fmt.Errorf("图片格式不受支持：%s", format)
	}

	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("图片尺寸信息无效")
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return nil, fmt.Errorf("图片尺寸过大（%d×%d），请压缩后重新上传", cfg.Width, cfg.Height)
	}

	mime := "image/jpeg"
	if format == "png" {
		mime = "image/png"
	}
	return &ImageInfo{MIME: mime, Width: cfg.Width, Height: cfg.Height, Format: format}, nil
}

// parseImage 解析图片：仅产出元信息，图片内容由多模态注入链路处理。
func parseImage(_ context.Context, src Source) (*Result, error) {
	info, err := DetectImage(src.Path)
	if err != nil {
		return nil, err
	}
	return &Result{
		Kind: KindImage,
		Meta: map[string]any{
			"mime":   info.MIME,
			"width":  info.Width,
			"height": info.Height,
			"format": info.Format,
			"bytes":  src.Size,
		},
	}, nil
}
