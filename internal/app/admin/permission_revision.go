package admin

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"websql/internal/config"

	"github.com/jmoiron/sqlx"
)

// 权限修订号：角色保存/删除、用户角色变更、连接授权变更必须在同一管理库事务内
// 单调递增该值，使「权限数据 + 修订号」原子提交。
//
// 消费方（AI Agent 运行链路）在每个安全边界比较修订号：
//   - 单进程：权限写提交后立即同步本地值；
//   - 多实例：每个实例每 500ms 轮询管理库，并配合 1s 短租约判断快照是否可信。
//
// 租约过期或读取失败时，远程 Profile 必须 fail-closed（拒绝数据工具与数据型 SSE），
// 不允许沿用旧 Scope 完成「最后一次查询」。
const (
	// PermissionRevisionPollInterval 多实例对账周期
	PermissionRevisionPollInterval = 500 * time.Millisecond
	// PermissionRevisionLeaseTTL 快照租约：超过该时长未成功对账即视为不可信
	PermissionRevisionLeaseTTL = time.Second
	// permissionRevisionSyncTimeout 单次对账的数据库超时
	permissionRevisionSyncTimeout = 800 * time.Millisecond
)

// ErrPermissionRevisionUnavailable 管理库不可用，或实例尚未完成首次对账。
var ErrPermissionRevisionUnavailable = errors.New("权限修订号不可用")

var (
	permRevisionValue    atomic.Uint64 // 最近一次成功对账的修订号
	permRevisionSyncedAt atomic.Int64  // 最近一次成功对账时间（UnixNano）
	permRevisionLoaded   atomic.Bool   // 是否至少成功对账过一次

	permRevisionWatcherOnce sync.Once
	permRevisionStopCh      chan struct{}
	permRevisionStopOnce    sync.Once
)

// CurrentPermissionRevision 返回本实例最近一次成功对账的修订号。
// 未完成首次对账时返回 0；调用方需要结合 PermissionRevisionFresh 判断可信度。
func CurrentPermissionRevision() uint64 {
	return permRevisionValue.Load()
}

// PermissionRevisionFresh 判断本地修订号快照是否仍在租约内。
func PermissionRevisionFresh() bool {
	if !permRevisionLoaded.Load() {
		return false
	}
	syncedAt := permRevisionSyncedAt.Load()
	if syncedAt <= 0 {
		return false
	}
	return time.Since(time.Unix(0, syncedAt)) <= PermissionRevisionLeaseTTL
}

// SnapshotPermissionRevision 原子返回「修订号 + 是否在租约内」。
func SnapshotPermissionRevision() (uint64, bool) {
	return permRevisionValue.Load(), PermissionRevisionFresh()
}

// SyncPermissionRevision 从管理库读取当前修订号并刷新本地快照。
func SyncPermissionRevision() error {
	db := getDBOrNil()
	if db == nil {
		return ErrPermissionRevisionUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), permissionRevisionSyncTimeout)
	defer cancel()

	var revision uint64
	if err := db.QueryRowContext(ctx,
		"SELECT revision FROM t_permission_revision WHERE id = 1").Scan(&revision); err != nil {
		return err
	}
	permRevisionValue.Store(revision)
	permRevisionSyncedAt.Store(time.Now().UnixNano())
	permRevisionLoaded.Store(true)
	return nil
}

// BumpPermissionRevisionTx 在给定事务内单调递增修订号。
//
// 必须与权限数据写入处于同一事务：进程崩溃时要么都生效、要么都不生效，
// 避免其他实例看不到已提交的权限回收。
func BumpPermissionRevisionTx(tx *sqlx.Tx) error {
	if tx == nil {
		return errors.New("权限修订号递增失败：事务为空")
	}
	now := time.Now()
	res, err := tx.Exec(
		"UPDATE t_permission_revision SET revision = revision + 1, updated_at = ? WHERE id = 1", now)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected >= 1 {
		return nil
	}

	// 存量库尚未执行 0003 迁移：先补建单行再递增。
	// 并发插入冲突说明其他事务已完成建行，回退为再次 UPDATE。
	permRevisionEnsureOnce.Do(func() {
		log.Printf("[PermRevision] t_permission_revision 无初始行，尝试补建")
	})
	if _, insErr := tx.Exec(
		"INSERT INTO t_permission_revision (id, revision, updated_at) VALUES (1, 1, ?)", now); insErr != nil {
		res, updErr := tx.Exec(
			"UPDATE t_permission_revision SET revision = revision + 1, updated_at = ? WHERE id = 1", now)
		if updErr != nil {
			return updErr
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return insErr
		}
	}
	return nil
}

var permRevisionEnsureOnce sync.Once

// StartPermissionRevisionWatcher 启动多实例对账。桌面/本地 Profile 不启动。
//
// 启动时先同步一次，使首个请求即可拿到可信修订号；失败仅告警，
// 由后续轮询继续重试（期间远程数据工具按租约过期 fail-closed）。
func StartPermissionRevisionWatcher() {
	if config.IsLocalMode() {
		log.Printf("[PermRevision] 本地模式，跳过权限修订号轮询")
		return
	}
	permRevisionWatcherOnce.Do(func() {
		if err := SyncPermissionRevision(); err != nil {
			log.Printf("[PermRevision] 首次对账失败，将在轮询中重试 - err=%v", err)
		}
		permRevisionStopCh = make(chan struct{})
		go permRevisionLoop(permRevisionStopCh)
		log.Printf("[PermRevision] 权限修订号轮询已启动 - interval=%v, lease=%v",
			PermissionRevisionPollInterval, PermissionRevisionLeaseTTL)
	})
}

// StopPermissionRevisionWatcher 停止对账轮询（可重复调用）。
func StopPermissionRevisionWatcher() {
	permRevisionStopOnce.Do(func() {
		if permRevisionStopCh != nil {
			close(permRevisionStopCh)
		}
	})
}

func permRevisionLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(PermissionRevisionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			log.Printf("[PermRevision] 权限修订号轮询已停止")
			return
		case <-ticker.C:
			if err := SyncPermissionRevision(); err != nil {
				// 只告警不中断：租约过期后由消费方 fail-closed
				log.Printf("[PermRevision] 对账失败 - err=%v", err)
			}
		}
	}
}

// MarkPermissionRevisionStale 供测试与故障注入使用：强制让本地快照过期。
func MarkPermissionRevisionStale() {
	permRevisionSyncedAt.Store(time.Now().Add(-2 * PermissionRevisionLeaseTTL).UnixNano())
}
