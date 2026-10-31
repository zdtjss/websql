package agent

import (
	"log"
	"sync/atomic"

	"websql/internal/pkg/dbaccess"

	"github.com/jmoiron/sqlx"
)

var holder = &dbaccess.Holder{}

// dbGeneration 记录管理库句柄的注入次数。
// 会话 debounce 保存是延迟触发的后台任务，若调度后句柄已被替换（测试重建库、
// 运行期重新注入），继续沿用新句柄会把上一代会话写到不相干的库里；
// 因此调度时捕获代号，触发时代号不一致则丢弃该次保存。
var dbGeneration atomic.Uint64

func Init(db *sqlx.DB) {
	holder.Init(db)
	dbGeneration.Add(1)
}

// currentDBGeneration 返回当前管理库句柄代号。
func currentDBGeneration() uint64 { return dbGeneration.Load() }

func getDB() *sqlx.DB { return holder.Get() }

// getDBOrNil 安全获取管理库连接：未初始化时返回 nil 而非 panic。
// dbaccess.Holder.Get 在两者皆为空时会 panic（用于在启动阶段暴露初始化错误），
// 但后台异步任务（会话 debounce 保存等）不应因此崩溃进程，故在此做兜底。
func getDBOrNil() (db *sqlx.DB) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Agent] 管理库未初始化，跳过本次 DB 操作 - %v\n", r)
			db = nil
		}
	}()
	return holder.Get()
}
