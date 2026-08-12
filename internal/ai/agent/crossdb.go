package agent

// crossdb.go — cross_db_join 工具：服务端跨库 Hash Join。
//
// 设计目标（替代旧 analyze.py 直连脚本 + 减少 Agent 上下文负担）：
//   - 两侧表数据在服务端内存中完成 Hash Join，仅返回紧凑统计结果与少量样本，
//     大量明细数据不进入 LLM 上下文（避免 reduction offload 与 token 浪费）
//   - 复用项目既有安全体系：GetConn 校验连接权限、PermissionMiddleware 校验表/列权限、
//     audit 记录、方言 LIMIT（applyRowLimit）与标识符引号（quoteIdent）处理
//   - 纯只读：内部仅构造 SELECT 查询，不执行任何写操作
//
// 输入契约（JSON）：
//
//	{
//	  "leftConnId": "Schema_A | 连接ID（可省略=默认连接）",
//	  "leftTable": "orders | schema.orders",
//	  "leftKey": "user_id",
//	  "leftSelect": ["id", "user_id", "amount"],   // 可选，默认全列
//	  "rightConnId": "...",
//	  "rightTable": "users",
//	  "rightKey": "id",
//	  "rightSelect": ["id", "name"],
//	  "joinType": "inner",                          // inner|left|right|full，默认 inner
//	  "metrics": [{"func":"sum","column":"left.amount"}],  // 可选：对匹配行聚合
//	  "limit": 10000,                               // 每侧取数上限，默认 10000，最大 50000
//	  "sampleLimit": 30                             // 样本行数，默认 30，最大 100
//	}
//
// 输出说明：
//   - matchedRows/leftOnlyRows/rightOnlyRows：各 join 类型的行数统计
//   - sample：匹配行的紧凑样本（列加 left_/right_ 前缀，长文本截断、数值保留 4 位）
//   - metrics：对匹配行计算的聚合指标（count/sum/avg/min/max）
//   - 局限：每侧仅取前 limit 行，结果属于抽样关联，不代表全量

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"websql/internal/ai/agent/sqlutil"
	"websql/internal/database"

	"github.com/jmoiron/sqlx"
)

const (
	crossDBJoinDefaultLimit   = 10000
	crossDBJoinMaxLimit       = 50000
	crossDBJoinDefaultSample  = 30
	crossDBJoinMaxSample      = 100
	crossDBJoinTimeout        = 120 * time.Second
	crossDBJoinMaxSampleBytes = 28 * 1024 // 样本序列化上限，低于 reduction 30k 字符阈值
	crossDBJoinCompactLen     = 60        // 样本中字符串/文本字段截断长度
	// crossDBJoinMaxMatched 匹配行内存上限：防止低基数 key（如性别/状态码）
	// 导致笛卡尔积爆炸（如 5万×5万 同 key = 25亿行）撑爆服务端内存。
	// 超过上限时停止保留明细行，但匹配计数仍完整统计。
	crossDBJoinMaxMatched = 1_000_000
)

var crossDBIdentifierPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validateIdentifier 校验标识符（表名/列名/join key），仅允许字母数字下划线，
// 防止注入恶意 SQL 片段。
func validateIdentifier(name, kind string) error {
	if !crossDBIdentifierPattern.MatchString(name) {
		return fmt.Errorf("cross_db_join %s 非法（仅允许字母/数字/下划线）: %s", kind, name)
	}
	return nil
}

// whereAllowedKeywords WHERE 片段允许的关键字白名单（防注入）
var whereAllowedKeywords = map[string]bool{
	"AND": true, "OR": true, "NOT": true, "IN": true, "BETWEEN": true,
	"LIKE": true, "IS": true, "NULL": true, "TRUE": true, "FALSE": true,
}

// whereBlockedKeywords WHERE 片段中无合法用途的 SQL 关键字黑名单：
// 这些关键字只能出现在子查询/DDL/DML 中，出现即拒绝（防 "IN (SELECT ...)" 等绕过）。
var whereBlockedKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "UNION": true, "EXCEPT": true,
	"INTERSECT": true, "UPDATE": true, "DELETE": true, "INSERT": true, "DROP": true,
	"ALTER": true, "CREATE": true, "TRUNCATE": true, "MERGE": true, "REPLACE": true,
	"EXISTS": true, "CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"CAST": true, "CONVERT": true, "HAVING": true, "GROUP": true, "ORDER": true,
	"BY": true, "AS": true, "DISTINCT": true, "ALL": true, "ANY": true, "SOME": true,
	"JOIN": true, "ON": true, "USING": true, "LIMIT": true, "OFFSET": true,
	"WITH": true, "VALUES": true, "SET": true, "INTO": true, "TABLE": true,
	"CALL": true, "EXEC": true, "EXECUTE": true, "DECLARE": true, "GRANT": true,
	"REVOKE": true, "PROCEDURE": true, "FUNCTION": true, "TRIGGER": true,
	"INDEX": true, "VIEW": true, "PRIMARY": true, "FOREIGN": true, "KEY": true,
	// Oracle FETCH 语法：防 "WHERE ... FETCH NEXT n ROWS ONLY" 使 applyRowLimit
	// 误判"已有 LIMIT"不追加，从而绕过行数上限
	"FETCH": true, "NEXT": true, "ROWS": true, "ONLY": true, "TOP": true,
}

// whereKeywordPattern 匹配 WHERE 片段中的标识符 token
var whereKeywordPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// validateWhereClause 校验 where 过滤条件（两侧通用）。
// 安全边界：仅允许「列 操作符 字面量」组合与白名单关键字，
// 禁止注释/函数调用/子查询/多语句/SQL 关键字，防止注入与方言陷阱。
func validateWhereClause(where string) error {
	if strings.TrimSpace(where) == "" {
		return nil
	}
	// 1. 禁止注释：剥离后必须与原文一致（校验与执行拼接必须基于同一文本）
	stripped := sqlutil.StripSQLComments(where)
	if stripped != where {
		return errors.New("cross_db_join where 条件不允许包含注释（-- / # / /* */）")
	}
	// 2. 多语句检测（字符串字面量内的分号不会误判）
	if sqlutil.ContainsMultipleStatements(stripped) {
		return errors.New("cross_db_join where 条件不允许包含多条语句（分号分隔）")
	}
	// 3. 函数调用检测：非白名单关键字标识符紧跟 "(" 即拒绝（如 DATE(...)）
	funcRe := regexp.MustCompile(`(?i)\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	for _, m := range funcRe.FindAllStringSubmatch(stripped, -1) {
		upper := strings.ToUpper(m[1])
		if !whereAllowedKeywords[upper] {
			return errors.New("cross_db_join where 条件不支持函数调用或子查询（如 DATE(x)），请使用「列 操作符 字面量」形式，例如: status = 'ACTIVE' AND created_at >= '2024-01-01'")
		}
	}
	// 4. token 级白名单扫描：仅允许标识符/数字/字符串/运算符/括号/逗号/点号
	tokenRe := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*|'[^']*'|\d+(?:\.\d+)?|[=<>!+\-*/%(),.]`)
	rest := tokenRe.ReplaceAllString(stripped, " ")
	if strings.TrimSpace(rest) != "" {
		return errors.New("cross_db_join where 条件包含非法字符，仅支持: 列名/数字/字符串/比较运算符/AND/OR/NOT/IN/BETWEEN/LIKE/IS NULL/括号/逗号")
	}
	// 5. 标识符检查：白名单关键字放行，SQL 关键字黑名单拒绝，其余视为列名
	for _, m := range whereKeywordPattern.FindAllString(stripped, -1) {
		upper := strings.ToUpper(m)
		if whereAllowedKeywords[upper] {
			continue
		}
		if whereBlockedKeywords[upper] {
			return fmt.Errorf("cross_db_join where 条件不允许使用 SQL 关键字: %s（仅支持简单比较表达式）", upper)
		}
		if !crossDBIdentifierPattern.MatchString(m) {
			return fmt.Errorf("cross_db_join where 条件含非法标识符: %s", m)
		}
	}
	return nil
}

// extractWhereColumns 提取 where 条件中引用的列名（字符串字面量先占位，避免误提取）。
// 仅用于列级权限校验。
func extractWhereColumns(where string) []string {
	if strings.TrimSpace(where) == "" {
		return nil
	}
	stripped := sqlutil.StripSQLComments(where)
	// 字符串字面量替换为占位符，防止字面量内的单词被误判为列名
	strRe := regexp.MustCompile(`'[^']*'`)
	stripped = strRe.ReplaceAllString(stripped, "''")
	seen := make(map[string]bool)
	var cols []string
	for _, m := range whereKeywordPattern.FindAllString(stripped, -1) {
		upper := strings.ToUpper(m)
		if whereAllowedKeywords[upper] {
			continue
		}
		if !seen[upper] {
			seen[upper] = true
			cols = append(cols, m)
		}
	}
	return cols
}

// resolveJoinKeys 解析 join key 列表：优先多列 keys，回退单列 key。
func resolveJoinKeys(keys []string, single string) ([]string, error) {
	if len(keys) > 0 {
		for _, k := range keys {
			if err := validateIdentifier(k, "join key"); err != nil {
				return nil, err
			}
		}
		return keys, nil
	}
	if single == "" {
		return nil, errors.New("cross_db_join 参数缺失: 需提供 leftKey/leftKeys 与 rightKey/rightKeys")
	}
	if err := validateIdentifier(single, "join key"); err != nil {
		return nil, err
	}
	return []string{single}, nil
}

type CrossDBJoinInput struct {
	LeftConnID  string       `json:"leftConnId,omitempty"`
	LeftTable   string       `json:"leftTable" jsonschema:"required"`
	LeftKey     string       `json:"leftKey,omitempty"`   // 单列 join key（与 leftKeys 二选一，优先 leftKeys）
	LeftKeys    []string     `json:"leftKeys,omitempty"`  // 复合 join key（多列联合匹配）
	LeftWhere   string       `json:"leftWhere,omitempty"` // 左侧过滤条件（仅支持简单表达式，见 validateWhereClause）
	LeftSelect  []string     `json:"leftSelect,omitempty"`
	RightConnID string       `json:"rightConnId,omitempty"`
	RightTable  string       `json:"rightTable" jsonschema:"required"`
	RightKey    string       `json:"rightKey,omitempty"`
	RightKeys   []string     `json:"rightKeys,omitempty"`
	RightWhere  string       `json:"rightWhere,omitempty"`
	RightSelect []string     `json:"rightSelect,omitempty"`
	JoinType    string       `json:"joinType,omitempty"` // inner|left|right|full
	Metrics     []JoinMetric `json:"metrics,omitempty"`
	Limit       int          `json:"limit,omitempty"`
	SampleLimit int          `json:"sampleLimit,omitempty"`
}

type JoinMetric struct {
	Func   string `json:"func,omitempty"`   // count|sum|avg|min|max
	Column string `json:"column,omitempty"` // "left.amount" / "right.amount"（count 可省略）
	Alias  string `json:"alias,omitempty"`
}

type JoinMetricResult struct {
	Func   string  `json:"func"`
	Column string  `json:"column,omitempty"`
	Alias  string  `json:"alias"`
	Value  any     `json:"value"` // count 为 int，其余为 float64（无有效值时 null）
	Valid  int     `json:"valid"` // 参与计算的非 NULL 数值行数
	Ratio  float64 `json:"ratio"` // 匹配行中有效数值占比 0~1
}

type CrossDBJoinOutput struct {
	JoinType         string             `json:"joinType"` // cross-hash-inner 等
	LeftConnID       string             `json:"leftConnId"`
	RightConnID      string             `json:"rightConnId"`
	LeftTable        string             `json:"leftTable"`
	RightTable       string             `json:"rightTable"`
	LeftRowsFetched  int                `json:"leftRowsFetched"`
	RightRowsFetched int                `json:"rightRowsFetched"`
	MatchedRows      int                `json:"matchedRows"`
	LeftOnlyRows     int                `json:"leftOnlyRows"`
	RightOnlyRows    int                `json:"rightOnlyRows"`
	Truncated        bool               `json:"truncated"`
	Metrics          []JoinMetricResult `json:"metrics,omitempty"`
	Sample           []map[string]any   `json:"sample"`
	ExecutionTimeMs  int                `json:"executionTimeMs"`
}

// ── 工具入口 ────────────────────────────────────────────────────────────────

func NewCrossDBJoinFunc(defaultConnID string, schemas []SchemaRef, auditCtx *ExecAuditCtx, userId string) func(ctx context.Context, input *CrossDBJoinInput) (*CrossDBJoinOutput, error) {
	connLookup := buildConnLookup(schemas)
	return func(ctx context.Context, input *CrossDBJoinInput) (*CrossDBJoinOutput, error) {
		start := time.Now()

		// 主逻辑包一层闭包：无论成功失败都统一走审计出口（失败路径同样记录 failed）
		out, runErr := func() (*CrossDBJoinOutput, error) {
			// 1. 输入校验（标识符白名单 + 取值范围）
			if err := validateCrossDBJoinInput(input); err != nil {
				return nil, err
			}

			joinType := strings.ToLower(input.JoinType)
			if joinType == "" {
				joinType = "inner"
			}
			limit := input.Limit
			if limit <= 0 {
				limit = crossDBJoinDefaultLimit
			}
			if limit > crossDBJoinMaxLimit {
				limit = crossDBJoinMaxLimit
			}
			sampleLimit := input.SampleLimit
			if sampleLimit <= 0 {
				sampleLimit = crossDBJoinDefaultSample
			}
			if sampleLimit > crossDBJoinMaxSample {
				sampleLimit = crossDBJoinMaxSample
			}

			// 2. 解析连接（Schema 名自动路由 / 连接 ID 直用 / 空 = 默认连接）
			leftConnID := resolveConnID(defaultConnID, input.LeftConnID, connLookup)
			rightConnID := resolveConnID(defaultConnID, input.RightConnID, connLookup)
			// 表前缀 schema 与 connId 路由一致性校验（提前拦截参数矛盾）
			if err := checkSchemaConnConsistency(input.LeftTable, leftConnID, connLookup); err != nil {
				return nil, err
			}
			if err := checkSchemaConnConsistency(input.RightTable, rightConnID, connLookup); err != nil {
				return nil, err
			}
			lconn, ltype := GetConn(leftConnID, userId)
			rconn, rtype := GetConn(rightConnID, userId)
			if lconn == nil {
				return nil, fmt.Errorf("cross_db_join 左侧连接无效: %s（可用 schema 名作为 connId，或直接传连接 ID）", input.LeftConnID)
			}
			if rconn == nil {
				return nil, fmt.Errorf("cross_db_join 右侧连接无效: %s（可用 schema 名作为 connId，或直接传连接 ID）", input.RightConnID)
			}

			queryCtx, cancel := context.WithTimeout(ctx, crossDBJoinTimeout)
			defer cancel()

			// 3. 两侧取数（服务端，方言 LIMIT 由 applyRowLimit 统一处理；
			//    取 limit+1 行用于精确判断是否发生截断）
			leftRows, leftTruncated, err := fetchJoinRows(queryCtx, lconn, ltype, input.LeftTable, input.LeftSelect, input.LeftWhere, limit)
			if err != nil {
				return nil, fmt.Errorf("左侧表查询失败 (%s): %w", input.LeftTable, err)
			}
			rightRows, rightTruncated, err := fetchJoinRows(queryCtx, rconn, rtype, input.RightTable, input.RightSelect, input.RightWhere, limit)
			if err != nil {
				return nil, fmt.Errorf("右侧表查询失败 (%s): %w", input.RightTable, err)
			}

			// 4. 服务端 Hash Join（key 类型归一化，NULL 不参与匹配；
			//    匹配计数完整统计，明细行超过上限时停止保留并标记 truncated）
			leftKeys, err := resolveJoinKeys(input.LeftKeys, input.LeftKey)
			if err != nil {
				return nil, err
			}
			rightKeys, err := resolveJoinKeys(input.RightKeys, input.RightKey)
			if err != nil {
				return nil, err
			}
			joined := hashJoinRows(leftRows, rightRows, leftKeys, rightKeys, joinType)

			// 5. 可选聚合指标
			var metricResults []JoinMetricResult
			if len(input.Metrics) > 0 {
				metricResults = computeJoinMetrics(joined.matched, input.Metrics)
			}

			// 6. 紧凑样本（截断长文本/精度 + 行数/字节双重限制）
			sample := buildCompactSample(joined.matched, sampleLimit)

			return &CrossDBJoinOutput{
				JoinType:         fmt.Sprintf("cross-hash-%s-join", joinType),
				LeftConnID:       leftConnID,
				RightConnID:      rightConnID,
				LeftTable:        input.LeftTable,
				RightTable:       input.RightTable,
				LeftRowsFetched:  len(leftRows),
				RightRowsFetched: len(rightRows),
				MatchedRows:      joined.matchedCount,
				LeftOnlyRows:     len(joined.leftOnly),
				RightOnlyRows:    len(joined.rightOnly),
				Truncated:        leftTruncated || rightTruncated || joined.truncated,
				Metrics:          metricResults,
				Sample:           sample,
				ExecutionTimeMs:  int(time.Since(start).Milliseconds()),
			}, nil
		}()

		// 7. 统一审计出口（成功/失败都记录，复用 query_data 审计链路）
		recordCrossDBJoinAudit(auditCtx, out, input, runErr, start)
		return out, runErr
	}
}

// ── 输入校验 ────────────────────────────────────────────────────────────────

func validateCrossDBJoinInput(input *CrossDBJoinInput) error {
	if input.LeftTable == "" || input.RightTable == "" {
		return errors.New("cross_db_join 参数缺失: leftTable/rightTable 必填")
	}
	if input.LeftKey == "" && len(input.LeftKeys) == 0 {
		return errors.New("cross_db_join 参数缺失: 需提供 leftKey 或 leftKeys")
	}
	if input.RightKey == "" && len(input.RightKeys) == 0 {
		return errors.New("cross_db_join 参数缺失: 需提供 rightKey 或 rightKeys")
	}
	if err := validateTableRef(input.LeftTable, "leftTable"); err != nil {
		return err
	}
	if err := validateTableRef(input.RightTable, "rightTable"); err != nil {
		return err
	}
	// join key：多列 keys 优先，否则单列 key；统一标识符校验
	if _, err := resolveJoinKeys(input.LeftKeys, input.LeftKey); err != nil {
		return err
	}
	if _, err := resolveJoinKeys(input.RightKeys, input.RightKey); err != nil {
		return err
	}
	if err := validateJoinColumns(input.LeftSelect, "leftSelect"); err != nil {
		return err
	}
	if err := validateJoinColumns(input.RightSelect, "rightSelect"); err != nil {
		return err
	}
	// where 过滤条件安全校验（两侧）
	if err := validateWhereClause(input.LeftWhere); err != nil {
		return fmt.Errorf("leftWhere 非法: %w", err)
	}
	if err := validateWhereClause(input.RightWhere); err != nil {
		return fmt.Errorf("rightWhere 非法: %w", err)
	}
	joinType := strings.ToLower(input.JoinType)
	switch joinType {
	case "", "inner", "left", "right", "full":
	default:
		return fmt.Errorf("cross_db_join joinType 仅支持 inner/left/right/full，收到: %s", input.JoinType)
	}
	for i, mt := range input.Metrics {
		if err := validateJoinMetric(mt, i); err != nil {
			return err
		}
	}
	if input.Limit < 0 || input.Limit > crossDBJoinMaxLimit {
		return fmt.Errorf("cross_db_join limit 范围 1~%d，收到: %d", crossDBJoinMaxLimit, input.Limit)
	}
	if input.SampleLimit < 0 || input.SampleLimit > crossDBJoinMaxSample {
		return fmt.Errorf("cross_db_join sampleLimit 范围 1~%d，收到: %d", crossDBJoinMaxSample, input.SampleLimit)
	}
	return nil
}

// checkSchemaConnConsistency 校验表前缀 schema 与 connId 路由结果的一致性。
// 场景：leftTable="Schema_B.orders" + leftConnId="Schema_A" —— 连接是 A 的、
// 表引用却是 B 的，属于参数矛盾，提前拦截给出清晰错误，避免数据库报晦涩错误。
// 仅当表前缀 schema 在 connLookup 中可路由时校验（直连模式不拦截）。
func checkSchemaConnConsistency(tableRef, resolvedConnID string, connLookup map[string]string) error {
	schema, _ := splitSchemaTable(tableRef, "")
	if schema == "" {
		return nil
	}
	if mappedConnID, ok := connLookup[strings.ToUpper(schema)]; ok && mappedConnID != resolvedConnID {
		return fmt.Errorf(
			"cross_db_join 表引用 schema 与连接不一致: 表 %s 的 schema 前缀 %s 路由到连接 %s，"+
				"但 connId 解析到连接 %s。请统一（表不带 schema 前缀时使用 connId 对应的默认库）",
			tableRef, schema, mappedConnID, resolvedConnID)
	}
	return nil
}

func validateTableRef(tableRef, kind string) error {
	schema, table := splitSchemaTable(tableRef, "")
	if schema != "" {
		if err := validateIdentifier(schema, kind+" schema"); err != nil {
			return err
		}
	}
	return validateIdentifier(table, kind)
}

func validateJoinColumns(cols []string, kind string) error {
	for _, c := range cols {
		if c == "*" {
			continue
		}
		if err := validateIdentifier(c, kind); err != nil {
			return err
		}
	}
	return nil
}

func validateJoinMetric(mt JoinMetric, idx int) error {
	funcName := strings.ToLower(mt.Func)
	if funcName == "" {
		funcName = "count"
	}
	switch funcName {
	case "count", "sum", "avg", "min", "max":
	default:
		return fmt.Errorf("cross_db_join metrics[%d].func 仅支持 count/sum/avg/min/max，收到: %s", idx, mt.Func)
	}
	if funcName != "count" {
		prefix, col := splitMetricColumn(mt.Column)
		if prefix != "left" && prefix != "right" {
			return fmt.Errorf("cross_db_join metrics[%d].column 需以 left./right. 前缀指定来源，收到: %s", idx, mt.Column)
		}
		if err := validateIdentifier(col, "metrics column"); err != nil {
			return err
		}
	}
	return nil
}

func splitMetricColumn(column string) (prefix, col string) {
	dotIdx := strings.IndexByte(column, '.')
	if dotIdx > 0 && dotIdx < len(column)-1 {
		return column[:dotIdx], column[dotIdx+1:]
	}
	return "", column
}

// ── 取数（服务端执行，仅 SELECT） ───────────────────────────────────────────

// fetchJoinRows 取一侧数据。取 limit+1 行以精确判断是否被截断：
// 返回 truncated=true 表示表内行数超过 limit（结果属抽样）。
// where 为可选过滤条件（已由 validateWhereClause 校验，直接拼接）。
func fetchJoinRows(ctx context.Context, conn *sqlx.DB, dbType, tableRef string, cols []string, where string, limit int) ([]map[string]any, bool, error) {
	schema, table := splitSchemaTable(tableRef, "")
	colList := "*"
	if len(cols) > 0 {
		parts := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == "*" {
				parts = append(parts, "*")
				continue
			}
			parts = append(parts, quoteIdent(dbType, c))
		}
		colList = strings.Join(parts, ", ")
	}
	tbl := quoteTableRef(dbType, schema, table)
	sql := fmt.Sprintf("SELECT %s FROM %s", colList, tbl)
	if strings.TrimSpace(where) != "" {
		sql += " WHERE " + where
	}
	// 方言 LIMIT：MySQL/SQLite 追加 LIMIT n，Oracle 12c+ 追加 FETCH FIRST n ROWS ONLY
	sql = applyRowLimit(sql, dbType, limit+1)

	rows, err := conn.QueryxContext(ctx, sql)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	data, err := database.GetResultRows(dbType, rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
	}
	return data, truncated, nil
}

// ── Hash Join（服务端内存） ─────────────────────────────────────────────────

type joinedRows struct {
	matched      []map[string]any
	leftOnly     []map[string]any
	rightOnly    []map[string]any
	matchedCount int  // 完整匹配行数（可能超过 matched 切片长度，用于精确统计）
	truncated    bool // 匹配明细行因超过 crossDBJoinMaxMatched 被截断
}

// joinKey 将任意类型的 join key 归一化为字符串（跨库类型不一致时仍可匹配），
// 返回 ok=false 表示 NULL（SQL JOIN 语义中 NULL 不参与匹配）。
func joinKey(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		// 兼容大数保护标记 "s:..."（ConvertColHandler 对 >2^53 的值加前缀防 JSON 丢精度）：
		// MySQL DOUBLE 走 "s:%f"（123.000000），Oracle NUMBER/BIGINT 走 "s:%d"（123），
		// 规范化后两侧才能匹配。
		if strings.HasPrefix(t, "s:") {
			return normalizeBigNumMarker(t[2:]), true
		}
		return t, true
	case []byte:
		return string(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case int32:
		return strconv.FormatInt(int64(t), 10), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32), true
	case bool:
		return strconv.FormatBool(t), true
	default:
		return fmt.Sprintf("%v", t), true
	}
}

// normalizeBigNumMarker 规范化大数保护标记内的数值文本：
// "123.000000" → "123"，"123.500000" → "123.5"，"123" → "123"。
// 使 MySQL 的 %f 格式与 Oracle 的 %d 格式在整数场景下一致。
func normalizeBigNumMarker(s string) string {
	if dotIdx := strings.IndexByte(s, '.'); dotIdx >= 0 {
		trimmed := strings.TrimRight(s[dotIdx:], "0")
		if trimmed == "." {
			return s[:dotIdx]
		}
		return s[:dotIdx] + trimmed
	}
	return s
}

// rowKey 将行的多列 join key 归一化为复合字符串（跨库类型不一致时仍可匹配）。
// 任一列为 NULL 返回 ok=false（SQL JOIN 语义中 NULL 不参与匹配）。
// 单 key 保持原值；多 key 用不可见分隔符拼接，避免 "a|b" 与 "a\x1fb" 混淆。
func rowKey(row map[string]any, keys []string) (string, bool) {
	if len(keys) == 1 {
		return joinKey(row[keys[0]])
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s, ok := joinKey(row[k])
		if !ok {
			return "", false
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "\x1f"), true
}

func hashJoinRows(leftRows, rightRows []map[string]any, leftKeys, rightKeys []string, joinType string) joinedRows {
	// 右表构建 Hash 索引
	rightIndex := make(map[string][]map[string]any)
	for _, r := range rightRows {
		if k, ok := rowKey(r, rightKeys); ok {
			rightIndex[k] = append(rightIndex[k], r)
		}
	}

	leftMatchedKeys := make(map[string]bool)
	var matched, leftOnly []map[string]any
	j := joinedRows{}

	// 遍历左表匹配（含 left/full 的左侧独有行）
	for _, l := range leftRows {
		k, ok := rowKey(l, leftKeys)
		if !ok {
			if joinType == "left" || joinType == "full" {
				leftOnly = append(leftOnly, l)
			}
			continue
		}
		rs := rightIndex[k]
		if len(rs) > 0 {
			leftMatchedKeys[k] = true
			for _, r := range rs {
				j.matchedCount++
				if len(matched) < crossDBJoinMaxMatched {
					matched = append(matched, mergeJoinedRow(l, r))
				} else {
					// 笛卡尔积防护：明细行停止保留（统计计数不受影响）
					j.truncated = true
				}
			}
		} else if joinType == "left" || joinType == "full" {
			leftOnly = append(leftOnly, l)
		}
	}

	// right/full 的右侧独有行（key 在左表中从未匹配；
	// 注意 leftMatchedKeys 在截断时仍完整记录，右侧独有统计不受影响）
	var rightOnly []map[string]any
	if joinType == "right" || joinType == "full" {
		for _, r := range rightRows {
			k, ok := rowKey(r, rightKeys)
			if !ok || !leftMatchedKeys[k] {
				rightOnly = append(rightOnly, r)
			}
		}
	}

	j.matched = matched
	j.leftOnly = leftOnly
	j.rightOnly = rightOnly
	return j
}

// mergeJoinedRow 合并左右行，列加 left_/right_ 前缀避免冲突
func mergeJoinedRow(l, r map[string]any) map[string]any {
	merged := make(map[string]any, len(l)+len(r))
	for k, v := range l {
		merged["left_"+k] = v
	}
	for k, v := range r {
		merged["right_"+k] = v
	}
	return merged
}

// ── 聚合指标 ────────────────────────────────────────────────────────────────

func computeJoinMetrics(matched []map[string]any, metrics []JoinMetric) []JoinMetricResult {
	results := make([]JoinMetricResult, 0, len(metrics))
	for _, mt := range metrics {
		funcName := strings.ToLower(mt.Func)
		if funcName == "" {
			funcName = "count"
		}
		column := mt.Column
		alias := mt.Alias
		if alias == "" {
			alias = funcName + "_" + strings.ReplaceAll(column, ".", "_")
			if column == "" {
				alias = funcName + "_count"
			}
		}

		res := JoinMetricResult{Func: funcName, Column: column, Alias: alias, Value: nil}
		total := len(matched)

		if funcName == "count" {
			res.Value = total
			res.Valid = total
			if total > 0 {
				res.Ratio = 1.0
			}
			results = append(results, res)
			continue
		}

		prefix, col := splitMetricColumn(column)
		var sum float64
		minV, maxV := math.Inf(1), math.Inf(-1)
		valid := 0
		for _, row := range matched {
			var v any
			if prefix == "left" {
				v = row["left_"+col]
			} else {
				v = row["right_"+col]
			}
			f, ok := toFloat64(v)
			if !ok {
				continue
			}
			valid++
			sum += f
			if f < minV {
				minV = f
			}
			if f > maxV {
				maxV = f
			}
		}
		switch funcName {
		case "sum":
			res.Value = round4(sum)
		case "avg":
			if valid > 0 {
				res.Value = round4(sum / float64(valid))
			}
		case "min":
			if !math.IsInf(minV, 1) {
				res.Value = round4(minV)
			}
		case "max":
			if !math.IsInf(maxV, -1) {
				res.Value = round4(maxV)
			}
		}
		res.Valid = valid
		if total > 0 {
			res.Ratio = math.Round(float64(valid)/float64(total)*10000) / 10000
		}
		results = append(results, res)
	}
	return results
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	case []byte:
		f, err := strconv.ParseFloat(strings.TrimSpace(string(t)), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}

// ── 紧凑样本 ────────────────────────────────────────────────────────────────

func buildCompactSample(rows []map[string]any, sampleLimit int) []map[string]any {
	if len(rows) > sampleLimit {
		rows = rows[:sampleLimit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		compact := make(map[string]any, len(row))
		for k, v := range row {
			compact[k] = compactValue(v)
		}
		out = append(out, compact)
	}

	// 字节保险：序列化超过阈值时二分砍行，避免触发 reduction offload
	for len(out) > 1 {
		encoded, err := json.Marshal(out)
		if err == nil && len(encoded) <= crossDBJoinMaxSampleBytes {
			break
		}
		out = out[:len(out)/2]
	}
	return out
}

func compactValue(v any) any {
	switch t := v.(type) {
	case string:
		if len(t) > crossDBJoinCompactLen {
			return t[:crossDBJoinCompactLen] + "…"
		}
		return t
	case []byte:
		s := string(t)
		if len(s) > crossDBJoinCompactLen {
			return s[:crossDBJoinCompactLen] + "…"
		}
		return s
	case float64:
		return round4(t)
	case float32:
		return round4(float64(t))
	default:
		return v
	}
}

// ── 审计 ────────────────────────────────────────────────────────────────────

// recordCrossDBJoinAudit 统一审计出口：成功/失败路径都记录（与 query_data 一致）。
// out 可能为 nil（执行失败时），审计摘要基于 input 构造。
func recordCrossDBJoinAudit(auditCtx *ExecAuditCtx, out *CrossDBJoinOutput, input *CrossDBJoinInput, runErr error, start time.Time) {
	if auditCtx == nil {
		return
	}

	connID := ""
	if out != nil {
		connID = out.LeftConnID
	} else {
		connID = input.LeftConnID
	}
	joinType := "inner"
	if input.JoinType != "" {
		joinType = input.JoinType
	}
	summary := fmt.Sprintf(
		"cross_db_join type=%s left=%s.%s(key=%s) right=%s.%s(key=%s) limit=%d",
		joinType, connID, input.LeftTable, input.LeftKey,
		input.RightConnID, input.RightTable, input.RightKey, input.Limit,
	)
	if out != nil {
		summary += fmt.Sprintf(" matched=%d truncated=%v", out.MatchedRows, out.Truncated)
	}

	status := "success"
	errorMsg := ""
	if runErr != nil {
		status = "failed"
		errorMsg = runErr.Error()
	}
	recordQueryAudit(auditCtx, truncateForAudit(summary), connID, status, 0, int(time.Since(start).Milliseconds()), errorMsg)
}
