// dialect_check.go — SQL 方言兼容性预检器
//
// 解决 a.txt 中的核心问题：LLM 生成 PostgreSQL/Oracle 专有语法（如
// PERCENTILE_CONT、STRING_AGG、LISTAGG）在 MySQL 上执行失败。
//
// 在 SQL 实际执行前进行静态检查，发现方言不兼容语法时立即返回详细错误，
// 包含正确的替代写法，让 LLM 能快速修正重试，避免浪费一次数据库往返。
package sqlutil

import (
	"fmt"
	"regexp"
	"strings"
)

// DialectError 描述一个方言不兼容问题
type DialectError struct {
	Feature    string // 不兼容的特性名，如 "PERCENTILE_CONT"
	Reason     string // 为什么不兼容
	Alternative string // 正确的替代写法
	Position   string // 在 SQL 中的大致位置（用于错误定位）
}

func (e *DialectError) Error() string {
	msg := fmt.Sprintf("方言不兼容：%s 在当前数据库不支持。%s", e.Feature, e.Reason)
	if e.Alternative != "" {
		msg += fmt.Sprintf(" 替代方案：%s", e.Alternative)
	}
	return msg
}

// 方言不兼容模式定义
type dialectPattern struct {
	regex       *regexp.Regexp
	feature     string
	reason      string
	alternative string
}

// MySQL 不兼容的模式（PostgreSQL/Oracle 专有语法）
var mysqlIncompatiblePatterns = []dialectPattern{
	{
		regex:       regexp.MustCompile(`(?i)\bPERCENTILE_CONT\s*\(`),
		feature:     "PERCENTILE_CONT",
		reason:      "MySQL 不支持 PERCENTILE_CONT（PostgreSQL/Oracle 专有）",
		alternative: "用子查询计算分位数：SELECT AVG(x) FROM (SELECT x FROM t ORDER BY x LIMIT 1 OFFSET n) tmp。中位数：SELECT AVG(x) FROM (SELECT x FROM t ORDER BY x LIMIT 2 OFFSET (COUNT(*)-1)/2) tmp（偶数行取中间两值平均）",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bWITHIN\s+GROUP\s*\(`),
		feature:     "WITHIN GROUP (ORDER BY ...)",
		reason:      "MySQL 不支持 WITHIN GROUP 语法（PostgreSQL/Oracle 专有）",
		alternative: "用子查询或窗口函数替代。PERCENT_RANK() 窗口函数（MySQL 8.0+）可计算百分位",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bSTRING_AGG\s*\(`),
		feature:     "STRING_AGG",
		reason:      "MySQL 不支持 STRING_AGG（PostgreSQL 专有）",
		alternative: "用 GROUP_CONCAT(col SEPARATOR ',') 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bLISTAGG\s*\(`),
		feature:     "LISTAGG",
		reason:      "MySQL 不支持 LISTAGG（Oracle 专有）",
		alternative: "用 GROUP_CONCAT(col SEPARATOR ',') 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bMEDIAN\s*\(`),
		feature:     "MEDIAN()",
		reason:      "MySQL 不支持 MEDIAN() 函数（Oracle 专有）",
		alternative: "用子查询：SELECT AVG(x) FROM (SELECT x FROM t ORDER BY x LIMIT 2 OFFSET (cnt-1)/2) tmp",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bARRAY_AGG\s*\(`),
		feature:     "ARRAY_AGG",
		reason:      "MySQL 不支持 ARRAY_AGG（PostgreSQL 专有）",
		alternative: "用 GROUP_CONCAT 或 JSON_ARRAYAGG 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bGENERATE_SERIES\s*\(`),
		feature:     "GENERATE_SERIES",
		reason:      "MySQL 不支持 GENERATE_SERIES（PostgreSQL 专有）",
		alternative: "用 WITH RECURSIVE 生成序列，或使用临时数字表",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bRETURNING\s+\w`),
		feature:     "RETURNING",
		reason:      "MySQL 不支持 RETURNING 子句（PostgreSQL/Oracle 专有）",
		alternative: "MySQL 需要 SELECT LAST_INSERT_ID() 或单独的 SELECT 查询获取数据",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bFETCH\s+(?:FIRST|NEXT)\s+\d+\s+ROWS?\s+ONLY`),
		feature:     "FETCH FIRST/NEXT n ROWS ONLY",
		reason:      "MySQL 不支持 FETCH FIRST/NEXT 语法（SQL:2008 标准，Oracle/PostgreSQL 支持）",
		alternative: "用 LIMIT n 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bOFFSET\s+\d+\s+ROWS?\s+FETCH\s+(?:FIRST|NEXT)\s+\d+\s+ROWS?\s+ONLY`),
		feature:     "OFFSET ... FETCH ... ROWS ONLY",
		reason:      "MySQL 不支持 OFFSET ... FETCH 语法",
		alternative: "用 LIMIT count OFFSET offset 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bEXTRACT\s*\(\s*(?:EPOCH|CENTURY|DECADE|MILLENNIUM)\s+FROM`),
		feature:     "EXTRACT(EPOCH/CENTURY/... FROM ...)",
		reason:      "MySQL 的 EXTRACT 不支持 EPOCH/CENTURY/DECADE/MILLENNIUM 字段（PostgreSQL 专有）",
		alternative: "用 UNIX_TIMESTAMP() 替代 EPOCH，其他用 DATE_FORMAT 计算",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bDATE_TRUNC\s*\(`),
		feature:     "DATE_TRUNC",
		reason:      "MySQL 不支持 DATE_TRUNC（PostgreSQL 专有）",
		alternative: "用 DATE_FORMAT(date, '%Y-%m-%d') 等替代按天/月/年截断",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bAGE\s*\(`),
		feature:     "AGE()",
		reason:      "MySQL 不支持 AGE() 函数（PostgreSQL 专有）",
		alternative: "用 TIMESTAMPDIFF() 或 DATEDIFF() 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bINTERVAL\s+['"]?\d`),
		feature:     "INTERVAL 'n' unit",
		reason:      "MySQL 的 INTERVAL 语法与 PostgreSQL 不同",
		alternative: "MySQL 用 INTERVAL n unit（如 INTERVAL 1 DAY）或 DATE_ADD/DATE_SUB",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bILIKE\s`),
		feature:     "ILIKE",
		reason:      "MySQL 不支持 ILIKE（PostgreSQL 专有，大小写不敏感 LIKE）",
		alternative: "用 LOWER(col) LIKE LOWER('pattern') 或 collation 不区分大小写",
	},
	{
		regex:       regexp.MustCompile(`(?i)\b~\s*[*]?\s*'`),
		feature:     "~ / ~* (正则匹配)",
		reason:      "MySQL 不支持 ~ 运算符（PostgreSQL 正则匹配）",
		alternative: "用 REGEXP 或 RLIKE 运算符替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bFILTER\s*\(\s*WHERE\s`),
		feature:     "FILTER (WHERE ...)",
		reason:      "MySQL 不支持 FILTER 子句（PostgreSQL/SQL:2003 专有）",
		alternative: "用 CASE WHEN ... THEN ... END 包裹聚合函数",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bRANK\s*\(\s*\)\s+OVER\s*\(\s*PARTITION\s+BY`),
		feature:     "RANK() OVER (PARTITION BY ...)",
		reason:      "MySQL 8.0+ 才支持窗口函数，低版本不支持",
		alternative: "确认 MySQL 版本 >= 8.0。低版本用变量模拟 ROW_NUMBER",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bBOOL\s+(?:AND|OR)\b`),
		feature:     "BOOL AND/OR",
		reason:      "MySQL 不支持 BOOL AND/OR 运算符（PostgreSQL 专有）",
		alternative: "用逻辑运算符 AND/OR 即可，MySQL 自动处理布尔值",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bLIMIT\s+\d+\s+OFFSET\s+\d+\s*;\s*$`),
		feature:     "",
		reason:      "",
		alternative: "",
	},
}

// Oracle 不兼容的模式
var oracleIncompatiblePatterns = []dialectPattern{
	{
		regex:       regexp.MustCompile("`"),
		feature:     "反引号标识符",
		reason:      "Oracle 不支持反引号包裹标识符",
		alternative: "用双引号包裹标识符：\"column_name\"",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bGROUP_CONCAT\s*\(`),
		feature:     "GROUP_CONCAT",
		reason:      "Oracle 不支持 GROUP_CONCAT（MySQL 专有）",
		alternative: "用 LISTAGG(col, ',') WITHIN GROUP (ORDER BY col) 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bIFNULL\s*\(`),
		feature:     "IFNULL()",
		reason:      "Oracle 不支持 IFNULL()",
		alternative: "用 NVL() 或 COALESCE() 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bDATE_FORMAT\s*\(`),
		feature:     "DATE_FORMAT",
		reason:      "Oracle 不支持 DATE_FORMAT",
		alternative: "用 TO_CHAR(date, 'YYYY-MM-DD') 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bLIMIT\s+\d+`),
		feature:     "LIMIT",
		reason:      "Oracle 不支持 LIMIT（12c 之前）",
		alternative: "用 ROWNUM <= n（12c 之前）或 OFFSET n ROWS FETCH NEXT m ROWS ONLY（12c+）",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bAUTO_INCREMENT\b`),
		feature:     "AUTO_INCREMENT",
		reason:      "Oracle 不支持 AUTO_INCREMENT",
		alternative: "用 SEQUENCE + TRIGGER 或 IDENTITY 列（12c+）",
	},
}

// SQLite 不兼容的模式
var sqliteIncompatiblePatterns = []dialectPattern{
	{
		regex:       regexp.MustCompile(`(?i)\bPERCENTILE_CONT\s*\(`),
		feature:     "PERCENTILE_CONT",
		reason:      "SQLite 不支持 PERCENTILE_CONT",
		alternative: "用子查询计算分位数",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bSTRING_AGG\s*\(`),
		feature:     "STRING_AGG",
		reason:      "SQLite 不支持 STRING_AGG",
		alternative: "用 GROUP_CONCAT() 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bDATE_FORMAT\s*\(`),
		feature:     "DATE_FORMAT",
		reason:      "SQLite 不支持 DATE_FORMAT",
		alternative: "用 strftime(format, date) 替代",
	},
	{
		regex:       regexp.MustCompile(`(?i)\bIFNULL\s*\(`),
		feature:     "",
		reason:      "",
		alternative: "",
	},
}

// CheckDialectCompatibility 检查 SQL 是否与目标数据库方言兼容
//
// 参数：
//   - sql: 待执行的 SQL 语句
//   - dbType: 数据库类型（mysql/mariadb/oracle/sqlite）
//
// 返回：
//   - []*DialectError: 发现的不兼容问题列表（空表示无问题）
func CheckDialectCompatibility(sql, dbType string) []*DialectError {
	cleaned := StripSQLComments(sql)
	var patterns []dialectPattern

	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		patterns = mysqlIncompatiblePatterns
	case "oracle":
		patterns = oracleIncompatiblePatterns
	case "sqlite":
		patterns = sqliteIncompatiblePatterns
	default:
		return nil
	}

	var errors []*DialectError
	for _, p := range patterns {
		if p.feature == "" {
			continue
		}
		if loc := p.regex.FindStringIndex(cleaned); loc != nil {
			errors = append(errors, &DialectError{
				Feature:     p.feature,
				Reason:      p.reason,
				Alternative: p.alternative,
				Position:    fmt.Sprintf("位置 %d-%d", loc[0], loc[1]),
			})
		}
	}

	return errors
}

// FormatDialectErrors 将方言错误列表格式化为对 LLM 友好的错误消息
func FormatDialectErrors(errors []*DialectError) string {
	if len(errors) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SQL 方言不兼容（共 %d 处问题）：\n", len(errors)))
	for i, e := range errors {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, e.Error()))
	}
	sb.WriteString("\n请使用当前数据库兼容的语法重写 SQL 后重试。")
	return sb.String()
}
