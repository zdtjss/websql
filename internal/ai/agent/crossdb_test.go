package agent

// ──────────────────────────────────────────────
// cross_db_join 工具核心逻辑单元测试
//
// 覆盖：输入校验、Hash Join 语义（inner/left/right/full、NULL key、
// 跨类型 key 归一化）、聚合指标计算、紧凑样本构建。
// ──────────────────────────────────────────────

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// ─── 输入校验 ─────────────────────────────────

func TestValidateCrossDBJoinInput(t *testing.T) {
	tests := []struct {
		name  string
		input CrossDBJoinInput
		ok    bool
	}{
		{
			name:  "合法输入",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "user_id", RightTable: "users", RightKey: "id", JoinType: "inner"},
			ok:    true,
		},
		{
			name:  "缺表名",
			input: CrossDBJoinInput{LeftKey: "user_id", RightTable: "users", RightKey: "id"},
			ok:    false,
		},
		{
			name:  "缺 join key",
			input: CrossDBJoinInput{LeftTable: "orders", RightTable: "users", RightKey: "id"},
			ok:    false,
		},
		{
			name:  "非法表名（含分号注入）",
			input: CrossDBJoinInput{LeftTable: "orders; DROP TABLE users--", LeftKey: "id", RightTable: "users", RightKey: "id"},
			ok:    false,
		},
		{
			name:  "非法 join key",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id) UNION SELECT 1--", RightTable: "users", RightKey: "id"},
			ok:    false,
		},
		{
			name:  "非法 joinType",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id", RightTable: "users", RightKey: "id", JoinType: "cross"},
			ok:    false,
		},
		{
			name:  "limit 超上限",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id", RightTable: "users", RightKey: "id", Limit: 999999},
			ok:    false,
		},
		{
			name:  "schema.table 形式",
			input: CrossDBJoinInput{LeftTable: "schemaA.orders", LeftKey: "id", RightTable: "users", RightKey: "id"},
			ok:    true,
		},
		{
			name:  "非法 schema",
			input: CrossDBJoinInput{LeftTable: "schema-A.orders", LeftKey: "id", RightTable: "users", RightKey: "id"},
			ok:    false,
		},
		{
			name:  "非法 metric func",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id", RightTable: "users", RightKey: "id", Metrics: []JoinMetric{{Func: "median", Column: "left.amount"}}},
			ok:    false,
		},
		{
			name:  "metric column 缺前缀",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id", RightTable: "users", RightKey: "id", Metrics: []JoinMetric{{Func: "sum", Column: "amount"}}},
			ok:    false,
		},
		{
			name:  "count 无 column 合法",
			input: CrossDBJoinInput{LeftTable: "orders", LeftKey: "id", RightTable: "users", RightKey: "id", Metrics: []JoinMetric{{Func: "count"}}},
			ok:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCrossDBJoinInput(&tt.input)
			if tt.ok && err != nil {
				t.Errorf("期望合法，实际报错: %v", err)
			}
			if !tt.ok && err == nil {
				t.Errorf("期望拒绝，实际通过")
			}
		})
	}
}

// ─── joinKey 类型归一化 ────────────────────────

func TestJoinKey(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want string
		ok   bool
	}{
		{"NULL 不参与匹配", nil, "", false},
		{"字符串", "abc", "abc", true},
		{"int64", int64(42), "42", true},
		{"float64 整数", 42.0, "42", true},
		{"float64 小数", 3.14, "3.14", true},
		{"bool", true, "true", true},
		{"[]byte", []byte("abc"), "abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := joinKey(tt.val)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("joinKey(%v) = (%q, %v), want (%q, %v)", tt.val, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// ─── Hash Join 语义 ────────────────────────────

func TestHashJoinRows(t *testing.T) {
	left := []map[string]any{
		{"id": int64(1), "name": "a"},
		{"id": int64(2), "name": "b"},
		{"id": int64(3), "name": "c"},
		{"id": nil, "name": "null_key"},
	}
	right := []map[string]any{
		{"id": "2", "city": "BJ"}, // 字符串 "2"，验证跨类型归一化
		{"id": int64(3), "city": "SH"},
		{"id": int64(9), "city": "GZ"},
	}

	t.Run("inner join 跨类型 key 匹配", func(t *testing.T) {
		got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "inner")
		if got.MatchedCount() != 2 {
			t.Fatalf("inner 期望匹配 2 行，实际 %d", got.MatchedCount())
		}
		// 校验列前缀与内容
		found := false
		for _, m := range got.matched {
			if m["left_name"] == "b" && m["right_city"] == "BJ" {
				found = true
			}
		}
		if !found {
			t.Errorf("未找到 left_name=b/right_city=BJ 的匹配行: %v", got.matched)
		}
	})

	t.Run("left join", func(t *testing.T) {
		got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "left")
		if got.MatchedCount() != 2 || len(got.leftOnly) != 2 {
			t.Errorf("left join 期望 matched=2 leftOnly=2，实际 matched=%d leftOnly=%d", got.MatchedCount(), len(got.leftOnly))
		}
	})

	t.Run("right join", func(t *testing.T) {
		got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "right")
		if got.MatchedCount() != 2 || len(got.rightOnly) != 1 {
			t.Errorf("right join 期望 matched=2 rightOnly=1，实际 matched=%d rightOnly=%d", got.MatchedCount(), len(got.rightOnly))
		}
	})

	t.Run("full join", func(t *testing.T) {
		got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "full")
		if got.MatchedCount() != 2 || len(got.leftOnly) != 2 || len(got.rightOnly) != 1 {
			t.Errorf("full join 期望 matched=2 leftOnly=2 rightOnly=1，实际 %d/%d/%d", got.MatchedCount(), len(got.leftOnly), len(got.rightOnly))
		}
	})

	t.Run("NULL key 不参与匹配", func(t *testing.T) {
		got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "inner")
		for _, m := range got.matched {
			if m["left_name"] == "null_key" {
				t.Errorf("NULL key 行不应参与匹配: %v", m)
			}
		}
	})
}

// 跨异构库（MySQL DOUBLE s:%f vs Oracle NUMBER s:%d）大整数 key 匹配
func TestHashJoinRowsHeteroBigNum(t *testing.T) {
	left := []map[string]any{
		{"id": "s:12345678901234568.000000", "name": "mysql_double"}, // MySQL DOUBLE 大数
		{"id": "s:12345678901234567", "name": "mysql_bigint"},        // MySQL BIGINT 大数
		{"id": "s:42.000000", "name": "mysql_small"},                 // 规范化后 = 42
	}
	right := []map[string]any{
		{"id": "s:12345678901234568", "name": "oracle_number"}, // Oracle NUMBER 大数
		{"id": "s:12345678901234567", "name": "oracle_int"},
		{"id": "s:42", "name": "oracle_42"},
	}

	got := hashJoinRows(left, right, []string{"id"}, []string{"id"}, "inner")
	if got.matchedCount != 3 {
		t.Fatalf("大数规范化后期望匹配 3 行，实际 %d: %v", got.matchedCount, got.matched)
	}
	// 校验具体配对（mysql_double ↔ oracle_number）
	found := false
	for _, m := range got.matched {
		if m["left_name"] == "mysql_double" && m["right_name"] == "oracle_number" {
			found = true
		}
	}
	if !found {
		t.Errorf("MySQL DOUBLE 大数未与 Oracle NUMBER 匹配: %v", got.matched)
	}
}

func TestNormalizeBigNumMarker(t *testing.T) {
	tests := []struct{ in, want string }{
		{"123.000000", "123"},
		{"123.500000", "123.5"},
		{"123", "123"},
		{"12345678901234568.000000", "12345678901234568"},
		{"123.450000", "123.45"},
	}
	for _, tt := range tests {
		if got := normalizeBigNumMarker(tt.in); got != tt.want {
			t.Errorf("normalizeBigNumMarker(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHashJoinRowsCartesianGuard(t *testing.T) {
	// 低基数 key（如状态码）导致笛卡尔积：1001 行 × 1000 行同 key = 1,001,000 匹配
	// 超过 crossDBJoinMaxMatched（100 万）时：明细行截断保留，但计数完整
	left := make([]map[string]any, 0, 1001)
	for i := 0; i < 1001; i++ {
		left = append(left, map[string]any{"key": int64(1), "v": i})
	}
	right := make([]map[string]any, 0, 1000)
	for i := 0; i < 1000; i++ {
		right = append(right, map[string]any{"key": int64(1), "w": i})
	}

	got := hashJoinRows(left, right, []string{"key"}, []string{"key"}, "inner")
	if got.matchedCount != 1001*1000 {
		t.Errorf("完整计数期望 %d，实际 %d", 1001*1000, got.matchedCount)
	}
	if len(got.matched) != crossDBJoinMaxMatched {
		t.Errorf("明细截断后期望保留 %d 行，实际 %d", crossDBJoinMaxMatched, len(got.matched))
	}
	if !got.truncated {
		t.Errorf("期望 truncated=true（笛卡尔积超限）")
	}

	// 正常小数据不误报截断
	small := hashJoinRows(
		[]map[string]any{{"key": int64(1)}},
		[]map[string]any{{"key": int64(1)}},
		[]string{"key"}, []string{"key"}, "inner",
	)
	if small.truncated || small.matchedCount != 1 {
		t.Errorf("小数据不应截断: truncated=%v count=%d", small.truncated, small.matchedCount)
	}
}

// 辅助：完整匹配计数（含被截断的明细行）
func (j joinedRows) MatchedCount() int { return j.matchedCount }

// ─── 聚合指标 ─────────────────────────────────

func TestComputeJoinMetrics(t *testing.T) {
	matched := []map[string]any{
		{"left_amount": 100.0, "right_flag": true},
		{"left_amount": 250.0, "right_flag": false},
		{"left_amount": nil, "right_flag": true},
		{"left_amount": "50", "right_flag": true}, // 字符串数值也可聚合
	}

	t.Run("count", func(t *testing.T) {
		res := computeJoinMetrics(matched, []JoinMetric{{Func: "count"}})
		if len(res) != 1 || res[0].Value != 4 || res[0].Ratio != 1.0 {
			t.Errorf("count 期望 4/1.0，实际 %+v", res)
		}
	})

	t.Run("sum/avg/min/max 跳过无效值", func(t *testing.T) {
		res := computeJoinMetrics(matched, []JoinMetric{
			{Func: "sum", Column: "left.amount"},
			{Func: "avg", Column: "left.amount"},
			{Func: "min", Column: "left.amount"},
			{Func: "max", Column: "left.amount"},
		})
		if len(res) != 4 {
			t.Fatalf("期望 4 个指标，实际 %d", len(res))
		}
		byFunc := map[string]JoinMetricResult{}
		for _, r := range res {
			byFunc[r.Func] = r
		}
		if byFunc["sum"].Value != 400.0 {
			t.Errorf("sum 期望 400，实际 %v", byFunc["sum"].Value)
		}
		if byFunc["avg"].Value != 133.3333 {
			t.Errorf("avg 期望 133.3333，实际 %v", byFunc["avg"].Value)
		}
		if byFunc["min"].Value != 50.0 {
			t.Errorf("min 期望 50，实际 %v", byFunc["min"].Value)
		}
		if byFunc["max"].Value != 250.0 {
			t.Errorf("max 期望 250，实际 %v", byFunc["max"].Value)
		}
		if byFunc["sum"].Valid != 3 || byFunc["sum"].Ratio != 0.75 {
			t.Errorf("sum 期望 valid=3 ratio=0.75，实际 %d/%v", byFunc["sum"].Valid, byFunc["sum"].Ratio)
		}
	})

	t.Run("默认 alias 生成", func(t *testing.T) {
		res := computeJoinMetrics(matched, []JoinMetric{{Func: "sum", Column: "left.amount"}})
		if res[0].Alias != "sum_left_amount" {
			t.Errorf("alias 期望 sum_left_amount，实际 %s", res[0].Alias)
		}
	})
}

// ─── 紧凑样本 ─────────────────────────────────

func TestBuildCompactSample(t *testing.T) {
	longText := strings.Repeat("x", 500)
	rows := []map[string]any{
		{"left_name": longText, "left_amount": 3.14159265},
		{"left_name": "short", "left_amount": 1.0},
	}

	t.Run("行数与值紧凑", func(t *testing.T) {
		got := buildCompactSample(rows, 10)
		if len(got) != 2 {
			t.Fatalf("期望 2 行，实际 %d", len(got))
		}
		if len(got[0]["left_name"].(string)) >= len(longText) {
			t.Errorf("长文本未截断: len=%d", len(got[0]["left_name"].(string)))
		}
		if got[0]["left_amount"].(float64) != 3.1416 {
			t.Errorf("数值未保留 4 位精度: %v", got[0]["left_amount"])
		}
	})

	t.Run("sampleLimit 截断", func(t *testing.T) {
		got := buildCompactSample(rows, 1)
		if len(got) != 1 {
			t.Errorf("期望 1 行，实际 %d", len(got))
		}
	})

	t.Run("序列化不超字节阈值", func(t *testing.T) {
		big := make([]map[string]any, 0, 200)
		for i := 0; i < 200; i++ {
			big = append(big, map[string]any{"left_col_" + strings.Repeat("k", 20): longText, "right_v": 1.0})
		}
		got := buildCompactSample(big, 200)
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		if len(encoded) > crossDBJoinMaxSampleBytes {
			t.Errorf("样本 %d 字节超过阈值 %d", len(encoded), crossDBJoinMaxSampleBytes)
		}
	})
}

// ─── mergeJoinedRow ───────────────────────────

func TestMergeJoinedRow(t *testing.T) {
	merged := mergeJoinedRow(
		map[string]any{"id": int64(1), "name": "a"},
		map[string]any{"id": int64(1), "city": "BJ"},
	)
	if merged["left_id"] != int64(1) || merged["right_id"] != int64(1) {
		t.Errorf("列前缀冲突未解决: %v", merged)
	}
	if merged["left_name"] != "a" || merged["right_city"] != "BJ" {
		t.Errorf("列内容错误: %v", merged)
	}
}

// ─── 端到端：真实 SQLite 驱动全链路 ─────────────
// 用两个内存 SQLite 库模拟跨库场景，真实执行 SQL（建表/插入/取数/join/聚合/样本），
// 验证 Go 实现的可行性与方言/类型转换链路的正确性。
func TestCrossDBJoinEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 左侧库：orders（模拟 MySQL 侧）
	ldb := sqlx.MustConnect("sqlite", ":memory:")
	defer ldb.Close()
	if _, err := ldb.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL, status TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, row := range [][]any{
		{1, 100, 120.5, "PAID"}, {2, 100, 30.0, "PAID"}, {3, 200, 80.0, "PENDING"},
		{4, 300, 50.0, "PAID"}, {5, 999, 999.0, "PAID"}, // 注意：user_id=999 与右侧 users.id=999 匹配
	} {
		if _, err := ldb.Exec(`INSERT INTO orders (id, user_id, amount, status) VALUES (?, ?, ?, ?)`, row...); err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	// 右侧库：users（模拟 Oracle 侧）
	rdb := sqlx.MustConnect("sqlite", ":memory:")
	defer rdb.Close()
	if _, err := rdb.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	for _, row := range [][]any{
		{100, "alice"}, {200, "bob"}, {999, "ghost"},
	} {
		if _, err := rdb.Exec(`INSERT INTO users (id, name) VALUES (?, ?)`, row...); err != nil {
			t.Fatalf("插入失败: %v", err)
		}
	}

	// 1. 左侧取数：带 where 过滤（仅 PAID 且 amount >= 50）
	leftRows, leftTrunc, err := fetchJoinRows(ctx, ldb, "sqlite", "orders", []string{"id", "user_id", "amount"}, "status = 'PAID' AND amount >= 50", 100)
	if err != nil {
		t.Fatalf("左侧取数失败: %v", err)
	}
	if leftTrunc || len(leftRows) != 3 { // PAID: 120.5, 50, 999 → 3 行
		t.Fatalf("左侧 where 过滤期望 3 行/未截断，实际 %d 行 truncated=%v", len(leftRows), leftTrunc)
	}

	// 2. 右侧取数
	rightRows, rightTrunc, err := fetchJoinRows(ctx, rdb, "sqlite", "users", []string{"id", "name"}, "", 100)
	if err != nil {
		t.Fatalf("右侧取数失败: %v", err)
	}
	if rightTrunc || len(rightRows) != 3 {
		t.Fatalf("右侧期望 3 行，实际 %d", len(rightRows))
	}

	// 3. Hash Join（left.user_id = right.id；left join 验证匹配与左侧独有行）
	joined := hashJoinRows(leftRows, rightRows, []string{"user_id"}, []string{"id"}, "left")
	if joined.matchedCount != 2 { // user 100(120.5) + user 999(999) → 2 行；user 300 无匹配
		t.Fatalf("join 期望匹配 2 行，实际 %d", joined.matchedCount)
	}
	if joined.truncated || len(joined.leftOnly) != 1 {
		t.Fatalf("leftOnly 期望 1 行（user 300），实际 %d truncated=%v", len(joined.leftOnly), joined.truncated)
	}

	// 4. 聚合指标（left.amount 求和）
	metrics := computeJoinMetrics(joined.matched, []JoinMetric{{Func: "sum", Column: "left.amount"}})
	if len(metrics) != 1 || metrics[0].Value != 1119.5 { // 120.5 + 999
		t.Fatalf("sum 期望 1119.5，实际 %+v", metrics)
	}

	// 5. 紧凑样本（列前缀 + 序列化字节限制）
	sample := buildCompactSample(joined.matched, 30)
	if len(sample) != 2 {
		t.Fatalf("样本期望 2 行，实际 %d", len(sample))
	}
	if _, ok := sample[0]["left_amount"]; !ok {
		t.Fatalf("样本缺少 left_ 前缀列: %v", sample[0])
	}
	if _, ok := sample[0]["right_name"]; !ok {
		t.Fatalf("样本缺少 right_ 前缀列: %v", sample[0])
	}
	encoded, _ := json.Marshal(sample)
	if len(encoded) > crossDBJoinMaxSampleBytes {
		t.Fatalf("样本超字节上限: %d", len(encoded))
	}
}

// ─── 复合 key 联合匹配 ────────────────────────

func TestHashJoinRowsCompositeKey(t *testing.T) {
	left := []map[string]any{
		{"user_id": int64(1), "order_date": "2024-01-01", "name": "a"},
		{"user_id": int64(1), "order_date": "2024-02-01", "name": "b"},
		{"user_id": int64(2), "order_date": "2024-01-01", "name": "c"},
		{"user_id": nil, "order_date": "2024-01-01", "name": "null_user"},
	}
	right := []map[string]any{
		{"uid": int64(1), "d": "2024-01-01", "city": "BJ"}, // 与 a 匹配（key 列名不同无妨）
		{"uid": int64(1), "d": "2024-02-01", "city": "SH"}, // 与 b 匹配
		{"uid": "1", "d": "2024-01-01", "city": "GZ"},      // 类型混合：uid 字符串 "1" 与 int64(1) 归一化后相同 → 与 a 也匹配
	}

	got := hashJoinRows(left, right, []string{"user_id", "order_date"}, []string{"uid", "d"}, "inner")
	// a 匹配 2 行（BJ + GZ），b 匹配 1 行（SH），c 不匹配，null_user 不匹配
	if got.matchedCount != 3 {
		t.Fatalf("复合 key 期望匹配 3 行，实际 %d", got.matchedCount)
	}
	// 校验匹配内容正确（a + BJ）
	found := false
	for _, m := range got.matched {
		if m["left_name"] == "a" && m["right_city"] == "BJ" {
			found = true
		}
	}
	if !found {
		t.Errorf("未找到 user_id=1/order_date=2024-01-01 匹配 BJ 的行: %v", got.matched)
	}
	// NULL 任一 key 列不参与匹配
	for _, m := range got.matched {
		if m["left_name"] == "null_user" {
			t.Errorf("NULL 复合 key 行不应参与匹配: %v", m)
		}
	}
}

// ─── where 条件安全校验 ───────────────────────

func TestValidateWhereClause(t *testing.T) {
	valid := []string{
		"status = 'ACTIVE'",
		"created_at >= '2024-01-01' AND status IN ('A','B')",
		"amount > 100 AND amount < 1000",
		"name LIKE 'A%' OR name IS NULL",
		"id BETWEEN 1 AND 100",
		"flag = TRUE AND (status = 'X' OR status = 'Y')",
		"name = 'a;b'", // 字符串内的分号合法
		"",
	}
	invalid := []string{
		"status = 'ACTIVE'; DROP TABLE users",      // 多语句
		"DATE(created_at) >= '2024-01-01'",         // 函数调用
		"status = 'ACTIVE' -- comment",             // 注释
		"status = 'ACTIVE' /* x */",                // 注释
		"id IN (SELECT id FROM users)",             // 子查询
		"status = 'ACTIVE' UNION SELECT 1",         // UNION 注入
		"status = 'ACTIVE' FETCH NEXT 5 ROWS ONLY", // FETCH 绕过行数上限
		"status = 'ACTIVE' @ x",                    // 非法字符
	}

	for _, w := range valid {
		if err := validateWhereClause(w); err != nil {
			t.Errorf("合法 where 被拒 %q: %v", w, err)
		}
	}
	for _, w := range invalid {
		if err := validateWhereClause(w); err == nil {
			t.Errorf("非法 where 未拦截 %q", w)
		}
	}
}

// ─── where 列提取（字面量跳过） ───────────────

func TestExtractWhereColumns(t *testing.T) {
	cols := extractWhereColumns("status = 'ACTIVE' AND created_at >= '2024-01-01' AND amount > 100")
	want := map[string]bool{"status": true, "created_at": true, "amount": true}
	if len(cols) != 3 {
		t.Fatalf("期望 3 列，实际 %v", cols)
	}
	for _, c := range cols {
		if !want[c] {
			t.Errorf("意外列 %s（字面量内的单词不应被提取）", c)
		}
	}
}

// ─── schema-connId 一致性校验 ─────────────────

func TestCheckSchemaConnConsistency(t *testing.T) {
	lookup := map[string]string{"SCHEMA_A": "conn1", "SCHEMA_B": "conn2"}

	// 一致：表前缀 schema 映射的连接 == 解析连接
	if err := checkSchemaConnConsistency("Schema_A.orders", "conn1", lookup); err != nil {
		t.Errorf("一致场景不应报错: %v", err)
	}
	// 无前缀：不校验
	if err := checkSchemaConnConsistency("orders", "conn1", lookup); err != nil {
		t.Errorf("无前缀场景不应报错: %v", err)
	}
	// 不一致：表前缀 schema 映射 conn2，但连接是 conn1
	if err := checkSchemaConnConsistency("Schema_B.orders", "conn1", lookup); err == nil {
		t.Errorf("不一致场景必须拦截（Schema_B.orders + conn1）")
	}
	// 直连模式：schema 不在 lookup 中，不拦截
	if err := checkSchemaConnConsistency("External.orders", "conn9", lookup); err != nil {
		t.Errorf("未知 schema 直连场景不应拦截: %v", err)
	}
}

// ─── 单列/多列 key 解析 ───────────────────────

func TestResolveJoinKeys(t *testing.T) {
	// 多列优先
	keys, err := resolveJoinKeys([]string{"a", "b"}, "x")
	if err != nil || len(keys) != 2 || keys[0] != "a" {
		t.Errorf("多列 keys 应优先: %v %v", keys, err)
	}
	// 回退单列
	keys, err = resolveJoinKeys(nil, "id")
	if err != nil || len(keys) != 1 || keys[0] != "id" {
		t.Errorf("单列回退失败: %v %v", keys, err)
	}
	// 都缺
	if _, err := resolveJoinKeys(nil, ""); err == nil {
		t.Errorf("双缺必须报错")
	}
	// 非法多列
	if _, err := resolveJoinKeys([]string{"a;DROP"}, ""); err == nil {
		t.Errorf("非法多列必须拒绝")
	}
}

func TestCheckCrossDBJoinColumns(t *testing.T) {
	// 构造 column 级权限 scope：users 表仅 id 列可访问
	scope := &PermissionScope{
		UserID:         "u1",
		ConnID:         "c1",
		IsRemote:       true,
		AllowedColumns: map[string]map[string]bool{"users": {"id": true, "name": true}},
	}
	mw := &PermissionMiddleware{Scope: scope}

	t.Run("column 级表显式列放行", func(t *testing.T) {
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id", LeftSelect: []string{"id", "name"},
			RightTable: "orders", RightKey: "user_id", RightSelect: []string{"order_id"},
		}
		if err := mw.checkCrossDBJoinColumns(input); err != nil {
			t.Errorf("显式有权限列应放行，实际报错: %v", err)
		}
	})

	t.Run("column 级表省略 select 被拒", func(t *testing.T) {
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id",
			RightTable: "orders", RightKey: "user_id",
		}
		if err := mw.checkCrossDBJoinColumns(input); err == nil {
			t.Errorf("column 级表省略 select 必须被拒绝（防全列绕过）")
		}
	})

	t.Run("column 级表星号 select 被拒", func(t *testing.T) {
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id", LeftSelect: []string{"*"},
			RightTable: "orders", RightKey: "user_id",
		}
		if err := mw.checkCrossDBJoinColumns(input); err == nil {
			t.Errorf("column 级表 * 必须被拒绝（防全列绕过）")
		}
	})

	t.Run("无权限列被拒", func(t *testing.T) {
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id", LeftSelect: []string{"id", "password"},
			RightTable: "orders", RightKey: "user_id",
		}
		if err := mw.checkCrossDBJoinColumns(input); err == nil {
			t.Errorf("无权限列必须被拒绝")
		}
	})

	t.Run("metrics 列校验", func(t *testing.T) {
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id", LeftSelect: []string{"id"},
			RightTable: "orders", RightKey: "user_id",
			Metrics: []JoinMetric{{Func: "sum", Column: "left.password"}},
		}
		if err := mw.checkCrossDBJoinColumns(input); err == nil {
			t.Errorf("metrics 引用无权限列必须被拒绝")
		}
	})

	t.Run("full 权限表不受显式列限制", func(t *testing.T) {
		scope2 := &PermissionScope{
			UserID:        "u1",
			ConnID:        "c1",
			IsRemote:      true,
			AllowedTables: map[string]bool{"orders": true},
		}
		mw2 := &PermissionMiddleware{Scope: scope2}
		input := &CrossDBJoinInput{
			LeftTable: "users", LeftKey: "id",
			RightTable: "orders", RightKey: "user_id",
		}
		// users 不在 scope2 中无权限？—— users 表在 scope2 中 level=none，
		// 表级检查由 checkCrossDBJoinAccess 负责，此处仅验证 orders(full) 不要求显式列
		if err := mw2.checkCrossDBJoinColumns(input); err != nil {
			t.Errorf("full 权限表不要求显式 select，实际报错: %v", err)
		}
	})
}
