package permission

import (
	"testing"
)

// findColumn 按「表名.列名」查找解析出的写入列。
func findColumn(cols []ColumnRef, table, column string) (ColumnRef, bool) {
	for _, c := range cols {
		if c.TableName == table && c.ColumnName == column {
			return c, true
		}
	}
	return ColumnRef{}, false
}

// TestAnalyzeSQL_MultiTableUpdate 冻结多表 UPDATE 的表/列归属（F-36）。
func TestAnalyzeSQL_MultiTableUpdate(t *testing.T) {
	cases := []struct {
		name        string
		sql         string
		wantTables  []string
		wantColumns [][2]string
	}{
		{
			name:       "JOIN 多表更新按别名归属",
			sql:        "UPDATE a JOIN b ON a.id=b.id SET a.x=1, b.y=2",
			wantTables: []string{"a", "b"},
			wantColumns: [][2]string{
				{"a", "x"}, {"b", "y"},
			},
		},
		{
			name:       "逗号多表更新",
			sql:        "UPDATE a, b SET a.x=1, b.y=2 WHERE a.id=b.id",
			wantTables: []string{"a", "b"},
			wantColumns: [][2]string{
				{"a", "x"}, {"b", "y"},
			},
		},
		{
			name:       "LEFT JOIN 修饰词不当作别名",
			sql:        "UPDATE t1 LEFT JOIN t2 ON t1.id=t2.id SET t1.c1=1, t2.c2=2",
			wantTables: []string{"t1", "t2"},
			wantColumns: [][2]string{
				{"t1", "c1"}, {"t2", "c2"},
			},
		},
		{
			name:        "单表更新无需限定",
			sql:         "UPDATE orders SET amount=100 WHERE id=1",
			wantTables:  []string{"orders"},
			wantColumns: [][2]string{{"orders", "amount"}},
		},
		{
			name:       "别名 AS",
			sql:        "UPDATE orders AS o SET o.amount=100",
			wantTables: []string{"orders"},
			wantColumns: [][2]string{
				{"orders", "amount"},
			},
		},
		{
			name:       "函数值与 CASE 表达式不破坏归属",
			sql:        "UPDATE a JOIN b ON a.id=b.id SET a.x=COALESCE(b.y, 0), b.z = CASE WHEN a.id>0 THEN 1 ELSE 2 END WHERE a.flag=1",
			wantTables: []string{"a", "b"},
			wantColumns: [][2]string{
				{"a", "x"}, {"b", "z"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analysis := AnalyzeSQL(tc.sql, "s1")
			if len(analysis.WriteTables) != len(tc.wantTables) {
				t.Fatalf("写入表数量不符：got %+v want %v", analysis.WriteTables, tc.wantTables)
			}
			for i, name := range tc.wantTables {
				if analysis.WriteTables[i].Name != name {
					t.Fatalf("第 %d 张写入表应为 %s，实际 %+v", i, name, analysis.WriteTables)
				}
			}
			for _, want := range tc.wantColumns {
				if _, ok := findColumn(analysis.WriteColumns, want[0], want[1]); !ok {
					t.Fatalf("应解析出写入列 %s.%s，实际 %+v", want[0], want[1], analysis.WriteColumns)
				}
			}
		})
	}
}

// TestAnalyzeSQL_UpdateWithoutQualifierOnMultiTable 多表更新未限定列必须归属为空（fail-closed）。
func TestAnalyzeSQL_UpdateWithoutQualifierOnMultiTable(t *testing.T) {
	analysis := AnalyzeSQL("UPDATE a JOIN b ON a.id=b.id SET x=1", "s1")
	if len(analysis.WriteColumns) != 1 {
		t.Fatalf("应解析出一条写入列：%+v", analysis.WriteColumns)
	}
	if analysis.WriteColumns[0].TableName != "" {
		t.Fatalf("无法归属目标表时 TableName 必须为空，交由上层拒绝：%+v", analysis.WriteColumns[0])
	}
}

// TestAnalyzeSQL_UpdateWithSchemaQualifiedColumns schema.table.column 形式仍能归属。
func TestAnalyzeSQL_UpdateWithSchemaQualifiedColumns(t *testing.T) {
	analysis := AnalyzeSQL("UPDATE s1.a JOIN s1.b ON a.id=b.id SET s1.a.x=1, b.y=2", "s1")
	if _, ok := findColumn(analysis.WriteColumns, "a", "x"); !ok {
		t.Fatalf("限定列应归属表 a：%+v", analysis.WriteColumns)
	}
	if _, ok := findColumn(analysis.WriteColumns, "b", "y"); !ok {
		t.Fatalf("别名列应归属表 b：%+v", analysis.WriteColumns)
	}
}
