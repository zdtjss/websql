// Package ddl 提供表结构变更的「计划 → 预览 → 执行」能力。
//
// 为什么把 DDL 生成放到服务端（缺陷 D2）：
//   - 前端拼 SQL 无法按方言表达完整列定义：MySQL MODIFY 丢 NOT NULL、
//     默认值一律加引号（CURRENT_TIMESTAMP 被写成 'CURRENT_TIMESTAMP'）；
//   - 生成与执行分离后，可先把 SQL 返回给用户确认，再按计划 ID 执行，
//     避免「点一下保存就直接改库」，也让失败原因能回到界面。
//
// 约定：
//   - 只生成 DDL，不拼任何用户数据；
//   - 标识符必须转义（支持中文列名，不做 ASCII 白名单限制）；
//   - 方言未覆盖的变更返回 Unsupported，由前端提示改用 SQL 编辑器。
package ddl

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ChangeKind 结构变更类型。
type ChangeKind string

const (
	KindAddColumn    ChangeKind = "add_column"
	KindDropColumn   ChangeKind = "drop_column"
	KindModifyColumn ChangeKind = "modify_column"
	KindRenameColumn ChangeKind = "rename_column"
	KindCreateIndex  ChangeKind = "create_index"
	KindDropIndex    ChangeKind = "drop_index"
)

// 风险级别，用于前端提示（不参与权限判定）
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// ColumnDef 列的完整定义。
type ColumnDef struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable *bool   `json:"nullable,omitempty"` // nil=不改动可空性
	Default  *string `json:"default,omitempty"`  // 默认值原值（可能为 CURRENT_TIMESTAMP 等表达式）
	Comment  string  `json:"comment,omitempty"`
	After    string  `json:"after,omitempty"` // 仅 MySQL 有效：放在指定列之后
	First    bool    `json:"first,omitempty"` // 仅 MySQL 有效：放到第一列（After 为空时生效）
}

// IndexDef 索引定义。
type IndexDef struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

// Change 单条结构变更意图。
type Change struct {
	Kind    ChangeKind `json:"kind"`
	Column  *ColumnDef `json:"column,omitempty"`
	Name    string     `json:"name,omitempty"`    // 新列名 / 被删列名 / 索引名
	OldName string     `json:"oldName,omitempty"` // 重命名前的列名
	Index   *IndexDef  `json:"index,omitempty"`
}

// PlanRequest 生成变更计划的入参。
type PlanRequest struct {
	ConnId  string   `json:"connId"`
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Changes []Change `json:"changes"`
}

// ApplyRequest 执行变更计划的入参。
type ApplyRequest struct {
	PlanId string `json:"planId"`
	ConnId string `json:"connId"`
	Schema string `json:"schema"`
}

// PlannedStatement 单条待执行语句。
type PlannedStatement struct {
	SQL     string `json:"sql"`
	Risk    string `json:"risk"`
	Message string `json:"message,omitempty"`
}

// Plan 结构变更计划。提交后由服务端缓存，Apply 时按 PlanId 取回并复核。
type Plan struct {
	PlanId      string             `json:"planId"`
	DbType      string             `json:"dbType"`
	Table       string             `json:"table"`
	Statements  []PlannedStatement `json:"statements"`
	Warnings    []string           `json:"warnings,omitempty"`
	Unsupported []string           `json:"unsupported,omitempty"`
}

var (
	defaultExprRe   = regexp.MustCompile(`(?i)^(current_timestamp(\s*\(\s*\d*\s*\))?|current_date|current_time|now\(\)|curdate\(\)|curtime\(\)|sysdate|systimestamp|uuid\(\)|null|true|false)$`)
	quotedLiteralRe = regexp.MustCompile(`(?s)^'.*'$`)
	numberLiteralRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	hexBitLiteralRe = regexp.MustCompile(`(?i)^(b'[01]+'|x'[0-9a-f]+'|0x[0-9a-f]+)$`)
)

// quoteIdent 按方言转义标识符。
// 不使用 sanitize.ValidateIdentifier 的 ASCII 白名单：中文列名是合法且常见的，
// 一刀切拒绝会让「修改中文列」直接不可用；这里改为转义引号并拒绝控制字符。
func quoteIdent(name, dbType string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("标识符不能为空")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return "", fmt.Errorf("标识符包含非法控制字符")
	}
	if utf8.RuneCountInString(name) > 128 {
		return "", fmt.Errorf("标识符超过 128 个字符")
	}
	if dbType == "oracle" {
		// 保持原始大小写：数据字典返回的是真实存储名，大写转换在引号列上会失配
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`", nil
}

// quoteString 转义字符串字面量。
func quoteString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `''`)
	return "'" + s + "'"
}

// formatDefault 把默认值原值转成可拼进 DDL 的片段。
// 表达式 / 数字 / 十六进制字面量不加引号，其余按字符串字面量处理。
func formatDefault(v *string, dbType string) string {
	if v == nil {
		return "NULL"
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return "NULL"
	}
	if defaultExprRe.MatchString(s) {
		if strings.EqualFold(s, "null") {
			return "NULL"
		}
		return s
	}
	if quotedLiteralRe.MatchString(s) || numberLiteralRe.MatchString(s) || hexBitLiteralRe.MatchString(s) {
		return s
	}
	_ = dbType
	return quoteString(s)
}

// positionClause 生成 MySQL 的列位置子句（AFTER x / FIRST），其他方言返回空。
func positionClause(col ColumnDef, dbType string) (string, error) {
	if dbType != "mysql" && dbType != "mariadb" {
		return "", nil
	}
	if col.After != "" {
		afterIdent, err := quoteIdent(col.After, dbType)
		if err != nil {
			return "", fmt.Errorf("AFTER 列名非法: %w", err)
		}
		return " AFTER " + afterIdent, nil
	}
	if col.First {
		return " FIRST", nil
	}
	return "", nil
}

// mysqlColumnDefinition 生成 MySQL/MariaDB 的完整列定义（NOT NULL 在前、COMMENT 内联）。
func mysqlColumnDefinition(col ColumnDef) (string, error) {
	ident, err := quoteIdent(col.Name, "mysql")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(col.Type) == "" {
		return "", fmt.Errorf("列 %q 缺少类型", col.Name)
	}
	parts := []string{ident + " " + strings.TrimSpace(col.Type)}
	if col.Nullable != nil {
		if *col.Nullable {
			parts = append(parts, "NULL")
		} else {
			parts = append(parts, "NOT NULL")
		}
	}
	parts = append(parts, "DEFAULT "+formatDefault(col.Default, "mysql"))
	if col.Comment != "" {
		parts = append(parts, "COMMENT "+quoteString(col.Comment))
	}
	return strings.Join(parts, " "), nil
}

// oracleColumnDefinition 生成 Oracle 的列定义（DEFAULT 必须在 NOT NULL 之前，不支持内联注释）。
func oracleColumnDefinition(col ColumnDef) (string, error) {
	ident, err := quoteIdent(col.Name, "oracle")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(col.Type) == "" {
		return "", fmt.Errorf("列 %q 缺少类型", col.Name)
	}
	parts := []string{ident + " " + strings.TrimSpace(col.Type)}
	parts = append(parts, "DEFAULT "+formatDefault(col.Default, "oracle"))
	if col.Nullable != nil && !*col.Nullable {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " "), nil
}

// buildStatements 把变更意图翻译成方言 SQL。
// 返回 (语句列表, 额外语句如 Oracle 的 COMMENT ON, 警告, 不支持的变更说明, 错误)。
func buildStatements(dbType, table string, changes []Change) ([]PlannedStatement, []string, []string, error) {
	if len(changes) == 0 {
		return nil, nil, nil, fmt.Errorf("变更列表为空")
	}
	tableIdent, err := quoteIdent(table, dbType)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("表名非法: %w", err)
	}

	stmts := make([]PlannedStatement, 0, len(changes))
	warnings := make([]string, 0)
	unsupported := make([]string, 0)

	for _, ch := range changes {
		switch ch.Kind {
		case KindAddColumn:
			if ch.Column == nil {
				return nil, nil, nil, fmt.Errorf("新增列缺少列定义")
			}
			col := *ch.Column
			switch dbType {
			case "mysql", "mariadb":
				def, err := mysqlColumnDefinition(col)
				if err != nil {
					return nil, nil, nil, err
				}
				sql := fmt.Sprintf("ALTER TABLE %s ADD %s", tableIdent, def)
				pos, err := positionClause(col, dbType)
				if err != nil {
					return nil, nil, nil, err
				}
				sql += pos
				risk := RiskLow
				if col.Nullable != nil && !*col.Nullable && (col.Default == nil || strings.TrimSpace(*col.Default) == "") {
					risk = RiskMedium
					warnings = append(warnings, fmt.Sprintf("列 %s 为 NOT NULL 且无默认值，若表内已有数据会执行失败", col.Name))
				}
				stmts = append(stmts, PlannedStatement{SQL: sql, Risk: risk})
			case "oracle":
				def, err := oracleColumnDefinition(col)
				if err != nil {
					return nil, nil, nil, err
				}
				stmts = append(stmts, PlannedStatement{SQL: fmt.Sprintf("ALTER TABLE %s ADD (%s)", tableIdent, def), Risk: RiskMedium})
				if col.Comment != "" {
					colIdent, _ := quoteIdent(col.Name, dbType)
					stmts = append(stmts, PlannedStatement{
						SQL:  fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tableIdent, colIdent, quoteString(col.Comment)),
						Risk: RiskLow,
					})
				}
			case "sqlite":
				colIdent, err := quoteIdent(col.Name, dbType)
				if err != nil {
					return nil, nil, nil, err
				}
				parts := []string{colIdent + " " + strings.TrimSpace(col.Type)}
				if col.Nullable != nil && !*col.Nullable {
					parts = append(parts, "NOT NULL")
				}
				if def := formatDefault(col.Default, dbType); def != "NULL" {
					parts = append(parts, "DEFAULT "+def)
				}
				stmts = append(stmts, PlannedStatement{
					SQL:  fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableIdent, strings.Join(parts, " ")),
					Risk: RiskLow,
				})
			default:
				unsupported = append(unsupported, string(ch.Kind))
			}

		case KindDropColumn:
			colIdent, err := quoteIdent(ch.Name, dbType)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("被删除列名非法: %w", err)
			}
			stmts = append(stmts, PlannedStatement{
				SQL:     fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableIdent, colIdent),
				Risk:    RiskHigh,
				Message: "删除列会永久丢失该列数据",
			})

		case KindModifyColumn:
			if ch.Column == nil {
				return nil, nil, nil, fmt.Errorf("修改列缺少列定义")
			}
			col := *ch.Column
			switch dbType {
			case "mysql", "mariadb":
				def, err := mysqlColumnDefinition(col)
				if err != nil {
					return nil, nil, nil, err
				}
				sql := fmt.Sprintf("ALTER TABLE %s MODIFY %s", tableIdent, def)
				// 字段排序也走这条路径（AFTER/FIRST）
				pos, err := positionClause(col, dbType)
				if err != nil {
					return nil, nil, nil, err
				}
				sql += pos
				stmts = append(stmts, PlannedStatement{
					SQL:     sql,
					Risk:    RiskMedium,
					Message: "修改类型可能截断已有数据，建议先备份",
				})
			case "oracle":
				def, err := oracleColumnDefinition(col)
				if err != nil {
					return nil, nil, nil, err
				}
				stmts = append(stmts, PlannedStatement{
					SQL:     fmt.Sprintf("ALTER TABLE %s MODIFY (%s)", tableIdent, def),
					Risk:    RiskMedium,
					Message: "修改类型可能截断已有数据，建议先备份",
				})
				if col.Comment != "" {
					colIdent, _ := quoteIdent(col.Name, dbType)
					stmts = append(stmts, PlannedStatement{
						SQL:  fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tableIdent, colIdent, quoteString(col.Comment)),
						Risk: RiskLow,
					})
				}
			default:
				// SQLite 不支持修改列定义，需重建表
				unsupported = append(unsupported, string(ch.Kind))
			}

		case KindRenameColumn:
			if strings.TrimSpace(ch.Name) == "" {
				return nil, nil, nil, fmt.Errorf("新列名不能为空")
			}
			oldIdent, err := quoteIdent(ch.OldName, dbType)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("原列名非法: %w", err)
			}
			newIdent, err := quoteIdent(ch.Name, dbType)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("新列名非法: %w", err)
			}
			switch dbType {
			case "mysql", "mariadb":
				// CHANGE 必须重述完整列定义，否则可空性/默认值/注释会被重置
				if ch.Column == nil {
					return nil, nil, nil, fmt.Errorf("MySQL 重命名列需要完整列定义")
				}
				col := *ch.Column
				col.Name = ch.Name
				def, err := mysqlColumnDefinition(col)
				if err != nil {
					return nil, nil, nil, err
				}
				stmts = append(stmts, PlannedStatement{
					SQL:  fmt.Sprintf("ALTER TABLE %s CHANGE %s %s", tableIdent, oldIdent, def),
					Risk: RiskMedium,
				})
			default:
				stmts = append(stmts, PlannedStatement{
					SQL:  fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tableIdent, oldIdent, newIdent),
					Risk: RiskLow,
				})
			}

		case KindCreateIndex:
			if ch.Index == nil || len(ch.Index.Columns) == 0 {
				return nil, nil, nil, fmt.Errorf("索引缺少名称或列")
			}
			idxIdent, err := quoteIdent(ch.Index.Name, dbType)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("索引名非法: %w", err)
			}
			cols := make([]string, 0, len(ch.Index.Columns))
			for _, c := range ch.Index.Columns {
				ci, err := quoteIdent(c, dbType)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("索引列名非法: %w", err)
				}
				cols = append(cols, ci)
			}
			prefix := "CREATE INDEX"
			if ch.Index.Unique {
				prefix = "CREATE UNIQUE INDEX"
			}
			sql := fmt.Sprintf("%s %s ON %s (%s)", prefix, idxIdent, tableIdent, strings.Join(cols, ", "))
			if dbType == "oracle" && ch.Index.Name == "" {
				return nil, nil, nil, fmt.Errorf("Oracle 创建索引必须显式指定索引名")
			}
			stmts = append(stmts, PlannedStatement{SQL: sql, Risk: RiskLow})

		case KindDropIndex:
			idxIdent, err := quoteIdent(ch.Name, dbType)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("索引名非法: %w", err)
			}
			switch dbType {
			case "mysql", "mariadb":
				stmts = append(stmts, PlannedStatement{
					SQL:  fmt.Sprintf("DROP INDEX %s ON %s", idxIdent, tableIdent),
					Risk: RiskMedium,
				})
			default:
				stmts = append(stmts, PlannedStatement{
					SQL:  fmt.Sprintf("DROP INDEX %s", idxIdent),
					Risk: RiskMedium,
				})
			}

		default:
			return nil, nil, nil, fmt.Errorf("未知的变更类型: %s", ch.Kind)
		}
	}

	return stmts, warnings, unsupported, nil
}
