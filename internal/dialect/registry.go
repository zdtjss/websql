package dialect

import (
	"fmt"
	"sort"
	"strings"
)

// 方言注册表。
//
// 现有实现是三个平行的全局 map（SQL_DIALECT / ConvertColHandler / ParseValHandler），
// 新增一种数据库要同时改四处（三个 map + database.makeDsn），且取值未命中时会拿到
// nil 函数并直接 panic（见 database.GetResultRows）。这里把它们收敛成统一的 Dialect 接口：
//   - 新方言通过 Register 一次性注册；
//   - 三张 map 保留为兼容视图（由 dialect 对象提供），现有调用点零改动；
//   - 取值统一走 ConvertValue/ParseValue，未注册时安全降级而不是 panic。
type Dialect interface {
	// Name 方言标识，与连接配置的 dbType 一致。
	Name() string
	// Template 返回元数据查询模板；ok=false 表示该方言不支持这条查询。
	Template(key string) (sql string, ok bool)
	// Convert 把驱动返回值归一化为 JSON 友好的类型。
	Convert(colType *string, val *any, overSign bool) *any
	// Parse 把用户输入解析为驱动参数。
	Parse(colType *string, val *string) *any
	// SupportsPaging 是否支持按 offset 分页。
	SupportsPaging() bool
	// WrapPage 生成分页 SQL 与参数。
	WrapPage(sql string, limit, offset int) (string, []any)
}

var registry = map[string]Dialect{}

// Register 注册方言；重复注册会覆盖（便于测试替换）。
func Register(d Dialect) {
	if d == nil || d.Name() == "" {
		return
	}
	registry[normalizeName(d.Name())] = d
}

// Get 按 dbType 取方言。
func Get(dbType string) (Dialect, bool) {
	d, ok := registry[normalizeName(dbType)]
	return d, ok
}

// Names 返回已注册的方言标识（升序），用于自检与文档。
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func normalizeName(dbType string) string {
	return strings.ToLower(strings.TrimSpace(dbType))
}

// ConvertValue 安全的值转换：方言或转换器缺失时原样返回，不会 panic。
func ConvertValue(dbType string, colType *string, val *any, overSign bool) *any {
	if val == nil {
		return nil
	}
	if d, ok := Get(dbType); ok {
		if out := d.Convert(colType, val, overSign); out != nil {
			return out
		}
	}
	return val
}

// ParseValue 安全的参数解析：方言或解析器缺失时原样返回。
func ParseValue(dbType string, colType *string, val *string) *any {
	if val == nil {
		return nil
	}
	if d, ok := Get(dbType); ok {
		if out := d.Parse(colType, val); out != nil {
			return out
		}
	}
	var raw any = *val
	return &raw
}

// Template 取元数据 SQL 模板（未注册或未定义时 ok=false）。
func Template(dbType, key string) (string, bool) {
	if d, ok := Get(dbType); ok {
		return d.Template(key)
	}
	if m, ok := SQL_DIALECT[dbType]; ok {
		sql, exists := m[key]
		return sql, exists
	}
	return "", false
}

// mapDialect 以现有的三张 map 为数据源的方言实现，
// 保证重构前后行为完全一致（现有调用点无需改动）。
type mapDialect struct {
	name   string
	paging bool
}

func (d mapDialect) Name() string { return d.name }

func (d mapDialect) Template(key string) (string, bool) {
	m, ok := SQL_DIALECT[d.name]
	if !ok {
		return "", false
	}
	sql, exists := m[key]
	return sql, exists
}

func (d mapDialect) Convert(colType *string, val *any, overSign bool) *any {
	if fn, ok := ConvertColHandler[d.name]; ok && fn != nil {
		return fn(colType, val, overSign)
	}
	return val
}

func (d mapDialect) Parse(colType *string, val *string) *any {
	if fn, ok := ParseValHandler[d.name]; ok && fn != nil {
		return fn(colType, val)
	}
	if val == nil {
		return nil
	}
	var raw any = *val
	return &raw
}

func (d mapDialect) SupportsPaging() bool { return d.paging }

func (d mapDialect) WrapPage(sql string, limit, offset int) (string, []any) {
	if !d.paging {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(sql), ";")), nil
	}
	return WrapPage(d.name, sql, limit, offset)
}

func init() {
	// 受管数据库：全部基于现有 map 注册，行为与重构前一致
	for _, name := range []string{"mysql", "mariadb", "sqlite", "sqlite3", "oracle"} {
		Register(mapDialect{name: name, paging: SupportsPaging(name)})
	}
}

// Describe 返回方言能力摘要，用于 /api/dialect 与自检日志。
func Describe(dbType string) (map[string]any, error) {
	d, ok := Get(dbType)
	if !ok {
		return nil, fmt.Errorf("未注册的方言: %s", dbType)
	}
	return map[string]any{
		"name":           d.Name(),
		"supportsPaging": d.SupportsPaging(),
		"templateCount":  len(SQL_DIALECT[d.Name()]),
	}, nil
}
