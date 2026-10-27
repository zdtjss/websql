package conn

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
)

// 只读连接与临时写解锁。
//
// 目的：生产库连接默认设为只读，误操作（多选后按 F9、AI 生成的 UPDATE）会被直接拒绝；
// 确需写入时由用户显式解锁一个 1/5 分钟的时间窗，窗口外依然只读。
//
// 存储：只读标记落在管理库 t_system_config（key = conn.readOnly.<connId>），
// 这样无需给 t_conn 加列，SQLite 与 MySQL 管理库都无需迁移脚本。
// 写解锁窗口只在内存中保留，进程重启即失效——避免“忘了关”留下长期可写入口。

const readOnlyKeyPrefix = "conn.readOnly."

// 写解锁时长（秒），与前端选项一致
const (
	WriteUnlockOneMinuteSecs   = 60
	WriteUnlockFiveMinutesSecs = 300
)

var writeUnlockWindows = struct {
	sync.Mutex
	m map[string]time.Time
}{m: make(map[string]time.Time)}

func readOnlyKey(connID string) string { return readOnlyKeyPrefix + connID }

// IsReadOnly 查询连接是否被标记为只读。管理库不可用时按可写处理（fail-open），
// 避免因为配置表读不到就把所有人锁死。
func IsReadOnly(connID string) bool {
	if connID == "" {
		return false
	}
	db := getDBOrNil()
	if db == nil {
		return false
	}
	var v string
	if err := db.Get(&v, "select config_value from t_system_config where config_key = ?", readOnlyKey(connID)); err != nil {
		return false
	}
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true"
}

// SetReadOnly 持久化只读标记。
func SetReadOnly(connID string, readOnly bool) error {
	if connID == "" {
		return fmt.Errorf("连接 ID 为空")
	}
	db := getDB()
	if db == nil {
		return fmt.Errorf("管理库未初始化")
	}
	value := "0"
	if readOnly {
		value = "1"
	}
	return database.RetryOnBusy(func() error {
		if _, err := db.Exec("delete from t_system_config where config_key = ?", readOnlyKey(connID)); err != nil {
			return err
		}
		_, err := db.Exec(
			"insert into t_system_config (id, config_key, config_value, config_type, remark) values (?, ?, ?, ?, ?)",
			idgen.RandomStr(), readOnlyKey(connID), value, "conn", "连接只读标记",
		)
		return err
	}, 3, 50*time.Millisecond)
}

// DeleteReadOnlyFlag 删除连接时清理标记。
func DeleteReadOnlyFlag(connID string) {
	if connID == "" {
		return
	}
	db := getDBOrNil()
	if db == nil {
		return
	}
	if _, err := db.Exec("delete from t_system_config where config_key = ?", readOnlyKey(connID)); err != nil {
		logger.PrintErrf("清理连接只读标记失败: %s", err, connID)
	}
}

// UnlockWrite 为连接开启写窗口（1 或 5 分钟）。
func UnlockWrite(connID string, durationSecs int) (int, error) {
	if connID == "" {
		return 0, fmt.Errorf("连接 ID 为空")
	}
	switch durationSecs {
	case WriteUnlockOneMinuteSecs, WriteUnlockFiveMinutesSecs:
	default:
		return 0, fmt.Errorf("解锁时长仅支持 %d 或 %d 秒", WriteUnlockOneMinuteSecs, WriteUnlockFiveMinutesSecs)
	}
	expireAt := time.Now().Add(time.Duration(durationSecs) * time.Second)
	writeUnlockWindows.Lock()
	writeUnlockWindows.m[connID] = expireAt
	writeUnlockWindows.Unlock()
	return durationSecs, nil
}

// HasWriteUnlock 判断连接当前是否处于解锁窗口内。
func HasWriteUnlock(connID string) bool {
	if connID == "" {
		return false
	}
	writeUnlockWindows.Lock()
	expireAt, ok := writeUnlockWindows.m[connID]
	if ok && time.Now().After(expireAt) {
		delete(writeUnlockWindows.m, connID)
		ok = false
	}
	writeUnlockWindows.Unlock()
	return ok
}

// WriteUnlockRemainingSecs 返回解锁剩余秒数（未解锁返回 0）。
func WriteUnlockRemainingSecs(connID string) int {
	writeUnlockWindows.Lock()
	defer writeUnlockWindows.Unlock()
	expireAt, ok := writeUnlockWindows.m[connID]
	if !ok {
		return 0
	}
	remaining := int(time.Until(expireAt).Seconds())
	if remaining <= 0 {
		delete(writeUnlockWindows.m, connID)
		return 0
	}
	return remaining
}

// CheckWritable 返回连接是否允许写入：非只读，或处于解锁窗口内。
func CheckWritable(connID string) (allowed bool, reason string) {
	if !IsReadOnly(connID) {
		return true, ""
	}
	if HasWriteUnlock(connID) {
		return true, ""
	}
	return false, "当前连接为只读，如需写入请在「连接管理」中临时解锁（" +
		strconv.Itoa(WriteUnlockOneMinuteSecs) + " 或 " + strconv.Itoa(WriteUnlockFiveMinutesSecs) + " 秒）"
}
