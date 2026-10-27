package fileparse

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

func init() {
	Register("go-mp4", 0, nil, []string{".mp4"}, ParserFunc(parseVideo))
}

// MP4 元数据解析（纯 Go，无 ffmpeg 依赖）。
//
// 覆盖范围：ftyp 品牌、mvhd 时长（timescale/duration）、tkhd 宽高（16.16 定点）、
// stsd 视频编码 fourcc。解析只读少量字节（按 box 跳转），不做全文件扫描。
//
// 说明：视频内容本身不落文本，模型若声明支持视频输入，由多模态注入链路
// 以 base64 传递；本解析器只负责元信息与可注入性判定所需的描述。
const (
	// mp4MaxBoxDepth 嵌套 box 的最大层级（防御性上限，防畸形深度嵌套）
	mp4MaxBoxDepth = 8
	// mp4MaxMoovSize moov box 体积上限，超过则放弃解析（防畸形文件）
	mp4MaxMoovSize int64 = 64 << 20
	// mp4MinHeaderLen 最小 box 头长度
	mp4MinHeaderLen int64 = 8
)

type mp4Box struct {
	typ    string
	offset int64 // payload 起始偏移
	size   int64 // box 总长度（含头）
}

// ProbeMP4 解析 MP4 元数据；返回 map 便于直接写入 Result.Meta。
func ProbeMP4(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，请重试")
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("读取文件失败，请重试")
	}
	fileSize := fi.Size()

	meta := map[string]any{"mime": "video/mp4"}
	hasFtyp := false
	hasMoov := false

	_ = walkBoxes(f, 0, fileSize, func(b mp4Box) error {
		switch b.typ {
		case "ftyp":
			hasFtyp = true
			if brand, err := readFourCC(f, b.offset); err == nil {
				meta["brand"] = brand
			}
		case "moov":
			hasMoov = true
			if b.size > mp4MaxMoovSize {
				return nil
			}
			parseMoov(f, b, meta)
		}
		return nil
	})

	if !hasFtyp {
		return nil, fmt.Errorf("不是有效的 MP4 文件（缺少 ftyp box）")
	}
	if !hasMoov {
		meta["notice"] = "未找到 moov box，可能为未完成的录制文件"
	}
	return meta, nil
}

// parseVideo 解析视频：仅产出元信息，内容由多模态注入链路处理。
func parseVideo(_ context.Context, src Source) (*Result, error) {
	meta, err := ProbeMP4(src.Path)
	if err != nil {
		return nil, err
	}
	if src.Size > 0 {
		meta["bytes"] = src.Size
	}
	var notice string
	if n, ok := meta["notice"].(string); ok && n != "" {
		notice = n
	}
	return &Result{Kind: KindVideo, Meta: meta, Notice: notice}, nil
}

// parseMoov 遍历 moov 子 box，提取 mvhd（时长）与 trak（宽高/编码）。
func parseMoov(r io.ReaderAt, moov mp4Box, meta map[string]any) {
	_ = walkBoxesDepth(r, moov.offset, moov.offset+moov.size-mp4MinHeaderLen, 1, func(b mp4Box) error {
		switch b.typ {
		case "mvhd":
			parseMvhd(r, b, meta)
		case "trak":
			parseTrak(r, b, meta)
		}
		return nil
	})
}

// parseMvhd 解析 mvhd：版本 0 用 32 位时长，版本 1 用 64 位。
func parseMvhd(r io.ReaderAt, b mp4Box, meta map[string]any) {
	buf := make([]byte, 32)
	n, err := r.ReadAt(buf, b.offset)
	if err != nil && n < 20 {
		return
	}
	version := buf[0]
	var timescale uint32
	var duration uint64
	if version == 1 {
		if n < 32 {
			return
		}
		timescale = binary.BigEndian.Uint32(buf[20:24])
		duration = binary.BigEndian.Uint64(buf[24:32])
	} else {
		timescale = binary.BigEndian.Uint32(buf[12:16])
		duration = uint64(binary.BigEndian.Uint32(buf[16:20]))
	}
	if timescale > 0 {
		meta["duration_sec"] = float64(duration) / float64(timescale)
		meta["timescale"] = timescale
	}
}

// parseTrak 解析 trak：tkhd 宽高 + stsd 视频编码。
func parseTrak(r io.ReaderAt, trak mp4Box, meta map[string]any) {
	_ = walkBoxesDepth(r, trak.offset, trak.offset+trak.size-mp4MinHeaderLen, 2, func(b mp4Box) error {
		switch b.typ {
		case "tkhd":
			parseTkhd(r, b, meta)
		case "mdia":
			_ = walkBoxesDepth(r, b.offset, b.offset+b.size-mp4MinHeaderLen, 3, func(md mp4Box) error {
				if md.typ != "minf" {
					return nil
				}
				_ = walkBoxesDepth(r, md.offset, md.offset+md.size-mp4MinHeaderLen, 4, func(mf mp4Box) error {
					if mf.typ != "stbl" {
						return nil
					}
					_ = walkBoxesDepth(r, mf.offset, mf.offset+mf.size-mp4MinHeaderLen, 5, func(sb mp4Box) error {
						if sb.typ == "stsd" {
							parseStsd(r, sb, meta)
						}
						return nil
					})
					return nil
				})
				return nil
			})
		}
		return nil
	})
}

// parseTkhd 解析 tkhd：宽高为 box 末尾 8 字节（16.16 定点），与版本无关。
func parseTkhd(r io.ReaderAt, b mp4Box, meta map[string]any) {
	if _, ok := meta["width"]; ok {
		return // 仅取第一个轨道（视频轨通常在最前）
	}
	payload := b.size - mp4MinHeaderLen
	if payload < 16 || payload > 4096 {
		return
	}
	buf := make([]byte, payload)
	if n, err := r.ReadAt(buf, b.offset); err != nil && int64(n) < payload {
		return
	}
	w := binary.BigEndian.Uint32(buf[payload-8 : payload-4])
	h := binary.BigEndian.Uint32(buf[payload-4 : payload])
	width := int(w >> 16)
	height := int(h >> 16)
	if width > 0 && height > 0 {
		meta["width"] = width
		meta["height"] = height
	}
}

// parseStsd 解析 stsd：取第一个 sample entry 的 fourcc 作为视频编码。
//
// stsd 布局（FullBox）：version/flags(4) + entry_count(4) + 首个 entry(size(4)+fourcc(4)…)
// 因此 fourcc 位于 payload 偏移 12 处。
func parseStsd(r io.ReaderAt, b mp4Box, meta map[string]any) {
	if _, ok := meta["codec"]; ok {
		return
	}
	codec, err := readFourCC(r, b.offset+12)
	if err != nil || codec == "" {
		return
	}
	meta["codec"] = codec
}

// readFourCC 读取 4 字节 fourcc
func readFourCC(r io.ReaderAt, off int64) (string, error) {
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], off); err != nil {
		return "", err
	}
	for _, c := range buf {
		if c < 0x20 || c > 0x7E {
			return "", fmt.Errorf("invalid fourcc")
		}
	}
	return string(buf[:]), nil
}

// walkBoxes 遍历 [start, end) 范围内的同级 box
func walkBoxes(r io.ReaderAt, start, end int64, fn func(b mp4Box) error) error {
	return walkBoxesDepth(r, start, end, 0, fn)
}

// walkBoxesDepth 遍历 box，depth 用于限制嵌套层级。
//
// 容错策略：遇到无法解析的 box（越界/长度非法）直接停止当前层遍历，
// 已解析到的信息仍然有效（部分元数据缺失不影响上层给出可用结论）。
func walkBoxesDepth(r io.ReaderAt, start, end int64, depth int, fn func(b mp4Box) error) error {
	if depth > mp4MaxBoxDepth {
		return nil
	}
	offset := start
	for offset+mp4MinHeaderLen <= end {
		var hdr [8]byte
		if _, err := r.ReadAt(hdr[:], offset); err != nil {
			return nil
		}
		size := int64(binary.BigEndian.Uint32(hdr[0:4]))
		typ := string(hdr[4:8])
		headerLen := mp4MinHeaderLen
		switch size {
		case 0:
			// 延伸至末尾
			size = end - offset
		case 1:
			var ext [8]byte
			if _, err := r.ReadAt(ext[:], offset+8); err != nil {
				return nil
			}
			size = int64(binary.BigEndian.Uint64(ext[:]))
			headerLen = 16
		}
		if size < headerLen || offset+size > end {
			return nil // 长度非法：停止遍历，保留已解析结果
		}
		if err := fn(mp4Box{typ: typ, offset: offset + headerLen, size: size}); err != nil {
			return err
		}
		offset += size
	}
	return nil
}
