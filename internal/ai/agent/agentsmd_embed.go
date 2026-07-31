// agentsmd_embed.go — 嵌入 AGENTS.md 内容，确保二进制打包后仍可用
//
// Go embed 不支持 ../ 路径，因此无法直接 embed 项目根的 AGENTS.md。
// 构建时由打包脚本（scripts/build_desktop.py 或 scripts/build_release.py）
// 自动将 AGENTS.md 复制为本目录的 _embedded_agents.md。
//
// 开发环境：运行时优先读取磁盘文件（工作目录下的 AGENTS.md），改完即生效。
// 生产环境：磁盘文件不存在时回退到嵌入内容。

package agent

import (
	"context"
	_ "embed"
	"os"

	"github.com/cloudwego/eino/adk/filesystem"
)

// embeddedAgentsMD 在构建时嵌入 _embedded_agents.md 的内容。
// 打包脚本（build_desktop.py / build_release.py）会在 go build 前自动生成此文件。
//
//go:embed _embedded_agents.md
var embeddedAgentsMD string

// embeddedAgentsMDBackend 是一个内存后端，优先从磁盘读取（允许运行时覆盖），
// 如果磁盘文件不存在则返回嵌入内容。
type embeddedAgentsMDBackend struct {
	diskPath string // 磁盘上的 AGENTS.md 路径（可能不存在）
}

func (b *embeddedAgentsMDBackend) Read(ctx context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	// 优先尝试磁盘文件（支持运行时热更新覆盖）
	if b.diskPath != "" {
		if data, err := os.ReadFile(b.diskPath); err == nil {
			return &filesystem.FileContent{Content: string(data)}, nil
		}
	}

	// 回退到嵌入内容
	return &filesystem.FileContent{Content: embeddedAgentsMD}, nil
}
