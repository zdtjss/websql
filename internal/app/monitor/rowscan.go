package monitor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 本文件提供「按列名取值」的结果集解析 helper。
//
// 缺陷背景（D8）：SHOW 系列命令的输出列由服务端决定，不同厂商与版本会追加扩展列。
// 例如腾讯云 CDB MySQL 开启动态线程池后，SHOW FULL PROCESSLIST 会追加
// Moved_to_per_thread / Moved_to_thread_pool；MariaDB 会追加 PROGRESS。
// sqlx 的 StructScan 在非 unsafe 模式下遇到未声明列会直接返回
// "missing destination name xxx" 并丢弃整批结果，导致监控页面整体不可用。
//
// 因此对这类结果集统一按列名取值：服务端多出的列被忽略，缺失的列降级为空值，
// 只有「必需列」缺失才返回错误。新增同类代码请遵守同一约束。

// columnPositions 把结果集列名（小写、去空白）映射为下标。
func columnPositions(columns []string) map[string]int {
	idx := make(map[string]int, len(columns))
	for i, c := range columns {
		idx[strings.ToLower(strings.TrimSpace(c))] = i
	}
	return idx
}

// findColumn 按候选别名顺序查找列下标，未找到返回 -1。
func findColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[strings.ToLower(alias)]; ok {
			return i
		}
	}
	return -1
}

// cellAt 安全取值：下标越界返回 nil。
func cellAt(values []any, i int) any {
	if i < 0 || i >= len(values) {
		return nil
	}
	return values[i]
}

// strAt 把单元格转换为字符串，nil/缺失列返回空串。
func strAt(values []any, i int) string {
	switch v := cellAt(values, i).(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// int64At 把单元格转换为整数，无法解析时返回 0。
func int64At(values []any, i int) int64 {
	switch v := cellAt(values, i).(type) {
	case nil:
		return 0
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case uint64:
		if v > uint64(1<<63-1) {
			return 0
		}
		return int64(v)
	case float64:
		return int64(v)
	case []byte:
		return parseInt64(string(v))
	case string:
		return parseInt64(v)
	default:
		return 0
	}
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// scanAllRows 将结果集读成 [][]any，同时返回列名（用于排障，不含数据）。
func scanAllRows(rows *sqlx.Rows) ([][]any, []string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("读取列信息失败: %w", err)
	}

	records := make([][]any, 0, 32)
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, columns, fmt.Errorf("扫描数据行失败: %w", err)
		}
		records = append(records, values)
	}
	if err := rows.Err(); err != nil {
		return nil, columns, fmt.Errorf("遍历结果集失败: %w", err)
	}
	return records, columns, nil
}

// parseProcessRows 解析 SHOW FULL PROCESSLIST 的结果。
// Id 为必需列（无 Id 无法展示会话与 Kill），其余列缺失时降级为空值。
// 返回的 columns 仅用于日志排障。
func parseProcessRows(rows *sqlx.Rows) ([]ProcessInfo, []string, error) {
	records, columns, err := scanAllRows(rows)
	if err != nil {
		return nil, columns, err
	}

	idx := columnPositions(columns)
	idI := findColumn(idx, "id")
	if idI < 0 {
		return nil, columns, fmt.Errorf("结果集缺少 Id 列，无法解析会话列表")
	}
	userI := findColumn(idx, "user")
	hostI := findColumn(idx, "host")
	dbI := findColumn(idx, "db")
	cmdI := findColumn(idx, "command")
	timeI := findColumn(idx, "time")
	stateI := findColumn(idx, "state")
	infoI := findColumn(idx, "info")

	processes := make([]ProcessInfo, 0, len(records))
	for _, values := range records {
		processes = append(processes, ProcessInfo{
			Id:      int(int64At(values, idI)),
			User:    strAt(values, userI),
			Host:    strAt(values, hostI),
			Db:      strAt(values, dbI),
			Command: strAt(values, cmdI),
			Time:    int(int64At(values, timeI)),
			State:   strAt(values, stateI),
			Info:    strAt(values, infoI),
		})
	}
	return processes, columns, nil
}

// parseNameValueRows 解析「名称-值」两列结果（SHOW [SESSION|GLOBAL] VARIABLES 等），
// 忽略服务端追加的额外列。
func parseNameValueRows(rows *sqlx.Rows, nameAliases, valueAliases []string) ([]VarInfo, []string, error) {
	records, columns, err := scanAllRows(rows)
	if err != nil {
		return nil, columns, err
	}

	idx := columnPositions(columns)
	nameI := findColumn(idx, nameAliases...)
	valueI := findColumn(idx, valueAliases...)
	if nameI < 0 || valueI < 0 {
		return nil, columns, fmt.Errorf("结果集缺少名称/值列，无法解析")
	}

	items := make([]VarInfo, 0, len(records))
	for _, values := range records {
		items = append(items, VarInfo{Name: strAt(values, nameI), Value: strAt(values, valueI)})
	}
	return items, columns, nil
}
