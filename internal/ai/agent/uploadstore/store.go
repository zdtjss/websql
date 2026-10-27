// Package uploadstore 上传文件暂存域。
//
// 职责：文件的磁盘落盘位置由调用方决定，本包只负责
//   - 元数据登记（含归属信息，防跨用户读取）；
//   - TTL 生命周期（过期/淘汰时同步删除磁盘文件）；
//   - 归属校验（IsOwner）。
//
// 设计约束：本包不做任何内容解析（延迟解析由 fileparse 在按需读取时完成），
// 也不依赖 Agent 包，便于单测与复用。
package uploadstore

import (
	"log"
	"os"
	"time"

	"websql/internal/pkg/ttlcache"
)

// FileMeta 上传文件元数据
type FileMeta struct {
	ID       string // 加密安全随机 fileId（不可枚举）
	UserID   string // 上传者用户 ID（归属校验，空表示本地/桌面模式免登录）
	FileName string // 展示名（不参与磁盘路径拼接，防路径穿越）
	DiskPath string // 磁盘绝对/相对路径
	Ext      string // 小写扩展名，含点
	Size     int64
	MIME     string
	Kind     string // 内容类别（fileparse.Kind 的字符串值：table/text/doc/image/video）
	// FileType 兼容字段：legacy /uploadExcel 响应与前端既有判断使用的类型名
	// （excel|csv|markdown|docx|pdf|txt|image|video）
	FileType  string
	CreatedAt time.Time
	ExpireAt  time.Time
}

// Store 上传文件暂存库
type Store struct {
	dir   string
	ttl   time.Duration
	cache *ttlcache.TTLCache[*FileMeta]
}

// NewStore 创建暂存库。
//
// dir 为落盘目录（不存在时自动创建，创建失败仅记录日志：
// 真正落盘失败会在上传时以明确错误返回）；
// ttl 为文件有效期；cleanInterval 为后台清理周期；maxEntries 为容量上限（0 表示不限）。
func NewStore(dir string, ttl, cleanInterval time.Duration, maxEntries int) *Store {
	s := &Store{dir: dir, ttl: ttl}
	s.cache = ttlcache.New[*FileMeta](ttl, cleanInterval, func(key string, meta *FileMeta) {
		if meta == nil {
			return
		}
		if meta.DiskPath != "" {
			if err := os.Remove(meta.DiskPath); err != nil && !os.IsNotExist(err) {
				log.Printf("[UploadStore] 删除暂存文件失败 - id=%s, path=%s, err=%v\n", key, meta.DiskPath, err)
			}
		}
		log.Printf("[UploadStore] 暂存文件已清理 - id=%s, name=%s, size=%d\n", key, meta.FileName, meta.Size)
	})
	if maxEntries > 0 {
		s.cache.SetMaxEntries(maxEntries)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[UploadStore] 创建暂存目录失败 - dir=%s, err=%v\n", dir, err)
	}
	return s
}

// Dir 返回暂存目录
func (s *Store) Dir() string { return s.dir }

// TTL 返回文件有效期
func (s *Store) TTL() time.Duration { return s.ttl }

// Save 登记文件元数据（ExpireAt 由本方法按 TTL 统一计算）
func (s *Store) Save(m *FileMeta) {
	if m == nil || m.ID == "" {
		return
	}
	m.CreatedAt = time.Now()
	m.ExpireAt = m.CreatedAt.Add(s.ttl)
	s.cache.Set(m.ID, m)
}

// Get 读取文件元数据；文件不存在或已过期返回 false
func (s *Store) Get(id string) (*FileMeta, bool) {
	if id == "" {
		return nil, false
	}
	return s.cache.Get(id)
}

// Delete 删除文件（元数据 + 磁盘文件），并触发清理日志
func (s *Store) Delete(id string) {
	if id == "" {
		return
	}
	s.cache.Delete(id)
}

// IsOwner 判断文件是否属于指定用户。
//
// 语义与既有实现保持一致：未记录归属（本地/桌面免登录模式）视为可访问；
// 文件不存在或已过期返回 false（避免向提示词暴露已失效文件）。
func (s *Store) IsOwner(id, userID string) bool {
	meta, ok := s.Get(id)
	if !ok {
		return false
	}
	return meta.UserID == "" || meta.UserID == userID
}

// Count 返回当前暂存文件数量
func (s *Store) Count() int { return s.cache.Len() }

// Close 停止后台清理 goroutine（进程退出/测试清理时调用）
func (s *Store) Close() { s.cache.Close() }
