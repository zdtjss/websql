// Package apperror 提供面向用户的错误分类与稳定错误码。
//
// 背景（缺陷：错误契约缺失）：原先数据库驱动错误被原文透传给前端，
// 既可能泄露主机、库名、SQL 片段，用户也拿不到「下一步该做什么」。
// 现在统一经 FromDatabase 分类为：
//   - Code：稳定错误码（WEBSQL-DB-xxxx），前端可按码做引导；
//   - Msg：可直接展示的中文描述（不含驱动原文）；
//   - Hint：建议动作；
//   - Cause：原始错误，只写服务端日志。
package apperror

import (
	"errors"
	"fmt"
	"strings"
)

// 数据库错误码。新增分类时只追加，不复用旧码。
const (
	CodeDBConnRefused  = "WEBSQL-DB-1001" // 无法连接主机
	CodeDBAuth         = "WEBSQL-DB-1002" // 认证失败
	CodeDBUnknownDB    = "WEBSQL-DB-1003" // 库不存在
	CodeDBTimeout      = "WEBSQL-DB-1004" // 超时 / 被取消
	CodeDBSyntax       = "WEBSQL-DB-1005" // 语法错误
	CodeDBPermission   = "WEBSQL-DB-1006" // 权限不足
	CodeDBObjectAbsent = "WEBSQL-DB-1007" // 表/列/对象不存在
	CodeDBLockWait     = "WEBSQL-DB-1008" // 锁等待 / 死锁
	CodeDBDuplicate    = "WEBSQL-DB-1009" // 唯一键冲突
	CodeDBDataRange    = "WEBSQL-DB-1010" // 数据超长 / 越界
	CodeDBUnsupported  = "WEBSQL-DB-1011" // 方言/版本不支持
	CodeDBUnknown      = "WEBSQL-DB-1099" // 未分类
)

// Error 统一的应用错误。
type Error struct {
	Code  string
	Msg   string
	Hint  string
	Cause error
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.Cause }

// New 构造一个应用错误。
func New(code, msg, hint string, cause error) *Error {
	return &Error{Code: code, Msg: msg, Hint: hint, Cause: cause}
}

// dbErrRule 一条分类规则：命中任一 needle 即归入该类。
type dbErrRule struct {
	code   string
	needles []string
	msg    string
	hint   string
}

// 规则顺序即优先级：越具体越靠前。
var dbErrRules = []dbErrRule{
	{
		code:    CodeDBTimeout,
		needles: []string{"context deadline exceeded", "timeout", "canceling statement", "query execution was interrupted", "cancelled", "canceled", "ora-01013"},
		msg:     "语句执行超时或已被取消",
		hint:    "可缩小查询范围、加过滤条件，或调大连接配置的语句超时后重试",
	},
	{
		code:    CodeDBAuth,
		needles: []string{"access denied for user", "1045", "ora-01017", "authentication failed", "password authentication failed"},
		msg:     "数据库认证失败",
		hint:    "请检查连接配置中的用户名与口令",
	},
	{
		code:    CodeDBUnknownDB,
		needles: []string{"unknown database", "1049", "ora-12505", "database \"", "does not exist"},
		msg:     "目标数据库不存在",
		hint:    "请确认库名或在连接配置中调整默认 Schema",
	},
	{
		code:    CodeDBPermission,
		needles: []string{"command denied", "1142", "ora-01031", "permission denied", "insufficient privilege", "not authorized"},
		msg:     "当前账号权限不足",
		hint:    "请让 DBA 为该账号授予所需对象权限，或在系统里调整数据权限",
	},
	{
		code:    CodeDBLockWait,
		needles: []string{"deadlock", "1213", "1205", "lock wait timeout", "ora-00060", "database is locked", "sqlite_busy"},
		msg:     "存在锁等待或死锁",
		hint:    "请稍后重试；若持续出现，检查是否有长事务或未提交的连接",
	},
	{
		code:    CodeDBDuplicate,
		needles: []string{"duplicate entry", "1062", "ora-00001", "unique constraint"},
		msg:     "违反唯一约束（数据已存在）",
		hint:    "请修改主键/唯一键值后重试",
	},
	{
		code:    CodeDBObjectAbsent,
		needles: []string{"doesn't exist", "1146", "1054", "ora-00942", "ora-00904", "no such table", "no such column", "unknown column"},
		msg:     "表或列不存在",
		hint:    "请刷新表结构后重试；若刚改过结构，确认变更已生效",
	},
	{
		code:    CodeDBDataRange,
		needles: []string{"data too long", "1406", "out of range value", "1264", "ora-01438", "ora-12899", "string or blob too big"},
		msg:     "数据超长或超出字段范围",
		hint:    "请缩短输入值或调整字段长度/精度",
	},
	{
		code:    CodeDBConnRefused,
		needles: []string{"connection refused", "no such host", "dial tcp", "i/o timeout", "network is unreachable", "ora-12541", "can't connect"},
		msg:     "无法连接数据库主机",
		hint:    "请检查地址、端口、防火墙与数据库是否已启动",
	},
	{
		code:    CodeDBSyntax,
		needles: []string{"syntax error", "1064", "ora-00933", "ora-00936", "you have an error in your sql", "unrecognized token"},
		msg:     "SQL 语法错误",
		hint:    "请检查语句拼写与方言差异（如 LIMIT / ROWNUM）",
	},
	{
		code:    CodeDBUnsupported,
		needles: []string{"not supported", "unsupported", "does not support", "ora-03001"},
		msg:     "当前数据库不支持该操作",
		hint:    "请改用等价的 SQL 或联系管理员确认数据库版本",
	},
}

// FromDatabase 把数据库驱动返回的错误分类为应用错误。
// 未命中任何规则时归入 CodeDBUnknown，只给出通用提示。
func FromDatabase(err error) *Error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	for _, rule := range dbErrRules {
		for _, needle := range rule.needles {
			if strings.Contains(msg, needle) {
				return &Error{Code: rule.code, Msg: rule.msg, Hint: rule.hint, Cause: err}
			}
		}
	}
	return &Error{
		Code:  CodeDBUnknown,
		Msg:   "数据库执行失败",
		Hint:  "请查看服务端日志中的详细信息，或联系管理员",
		Cause: err,
	}
}

// As 从错误链中取回应用错误。
func As(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// Wrap 在保留错误码的前提下追加上下文。
func Wrap(code, msg, hint string, cause error) *Error {
	if cause == nil {
		return New(code, msg, hint, nil)
	}
	return New(code, msg, hint, fmt.Errorf("%s: %w", msg, cause))
}
