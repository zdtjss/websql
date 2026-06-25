package backup

import (
	"sync"
	"time"

	"websql/internal/pkg/safego"
)

// BackupProgress 描述一次备份任务的实时进度信息。
// 进度数据仅保存在内存中（不持久化），任务完成或失败后 30 秒自动清理。
type BackupProgress struct {
	TaskId          string         `json:"taskId"`          // 任务唯一标识（雪花 ID）
	ConnId          string         `json:"connId"`          // 连接 ID
	Schema          string         `json:"schema"`          // 数据库 schema
	Status          string         `json:"status"`          // running / completed / failed
	TotalTables     int            `json:"totalTables"`     // 待备份表总数
	ProcessedTables int            `json:"processedTables"` // 已处理表数
	CurrentTable    string         `json:"currentTable"`    // 当前正在处理的表名
	ExportedBytes   int64          `json:"exportedBytes"`   // 已导出字节数
	StartedAt       int64          `json:"startedAt"`       // 开始时间（unix 毫秒）
	FinishedAt      int64          `json:"finishedAt"`      // 结束时间（unix 毫秒，0 表示未结束）
	Error           string         `json:"error,omitempty"` // 失败时的错误信息
	Result          map[string]any `json:"result,omitempty"`// 完成时的最终结果，便于前端轮询直接拿到
}

// progressStore 进度存储。使用 sync.Map 保证并发安全，
// key 为 taskId（全局唯一雪花 ID），value 为 *BackupProgress。
var progressStore sync.Map

// SetBackupProgress 写入/更新指定任务的进度。
func SetBackupProgress(taskId string, p BackupProgress) {
	if taskId == "" {
		return
	}
	progressStore.Store(taskId, p)
}

// FetchBackupProgress 读取指定任务的进度。ok=false 表示不存在（已被清理或未创建）。
// 注意：函数名不使用 GetBackupProgress，以避免与 handler 包级函数 GetBackupProgress 冲突。
func FetchBackupProgress(taskId string) (BackupProgress, bool) {
	if taskId == "" {
		return BackupProgress{}, false
	}
	v, ok := progressStore.Load(taskId)
	if !ok {
		return BackupProgress{}, false
	}
	p, _ := v.(BackupProgress)
	return p, true
}

// DeleteBackupProgress 立即删除指定任务的进度。
func DeleteBackupProgress(taskId string) {
	if taskId == "" {
		return
	}
	progressStore.Delete(taskId)
}

// scheduleProgressCleanup 在延迟 delay 后删除指定任务的进度。
// 用于任务完成或失败后保留一段时间供前端读取最终状态，再自动回收内存。
func scheduleProgressCleanup(taskId string, delay time.Duration) {
	if taskId == "" {
		return
	}
	safego.GoWithName("backup-progress-cleanup", func() {
		time.Sleep(delay)
		progressStore.Delete(taskId)
	})
}
