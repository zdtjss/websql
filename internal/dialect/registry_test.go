package dialect

import "testing"

// TestRegistryCoversManagedDialects 受管数据库都必须注册，且模板与转换器可用。
func TestRegistryCoversManagedDialects(t *testing.T) {
	for _, name := range []string{"mysql", "mariadb", "sqlite", "oracle"} {
		d, ok := Get(name)
		if !ok {
			t.Fatalf("%s 未注册", name)
		}
		if _, ok := d.Template("listTable"); !ok {
			t.Errorf("%s 缺少 listTable 模板", name)
		}
		if info, err := Describe(name); err != nil || info["templateCount"] == 0 {
			t.Errorf("%s 能力摘要异常: %v, err=%v", name, info, err)
		}
	}
}

// TestConvertValueSafeForUnknownDialect 未注册方言不得 panic（原实现会调用 nil 函数）。
func TestConvertValueSafeForUnknownDialect(t *testing.T) {
	colType := "varchar"
	var val any = "hello"
	got := ConvertValue("postgres", &colType, &val, true)
	if got == nil || *got != "hello" {
		t.Fatalf("未知方言应原样返回，实际 %+v", got)
	}

	var num any = int64(42)
	colType = "int"
	got = ConvertValue("unknown-db", &colType, &num, false)
	if got == nil || *got != int64(42) {
		t.Fatalf("未知方言应原样返回整数，实际 %+v", got)
	}
}

// TestParseValueSafeForUnknownDialect 同上，解析侧也不能 panic。
func TestParseValueSafeForUnknownDialect(t *testing.T) {
	colType := "varchar"
	in := "abc"
	got := ParseValue("unknown-db", &colType, &in)
	if got == nil || *got != "abc" {
		t.Fatalf("未知方言应原样返回，实际 %+v", got)
	}
}

// TestMapDialectBehavesLikeLegacyMap 重构后行为必须与旧 map 完全一致。
func TestMapDialectBehavesLikeLegacyMap(t *testing.T) {
	d, ok := Get("mysql")
	if !ok {
		t.Fatal("mysql 未注册")
	}

	legacySQL, legacyOK := SQL_DIALECT["mysql"]["listTable"]
	newSQL, newOK := d.Template("listTable")
	if legacyOK != newOK || legacySQL != newSQL {
		t.Fatalf("模板与旧实现不一致: %q/%v vs %q/%v", legacySQL, legacyOK, newSQL, newOK)
	}

	colType := "INT"
	var val any = []byte("123")
	legacy := ConvertColHandler["mysql"](&colType, &val, false)
	current := d.Convert(&colType, &val, false)
	if legacy == nil || current == nil || *legacy != *current {
		t.Fatalf("转换结果与旧实现不一致: %+v vs %+v", legacy, current)
	}

	if !d.SupportsPaging() {
		t.Error("mysql 应支持分页")
	}
	sql, params := d.WrapPage("select 1", 10, 20)
	if sql != "select 1 LIMIT ? OFFSET ?" || len(params) != 2 {
		t.Errorf("分页包装异常: %s %v", sql, params)
	}
}

// TestNamesSorted 注册表名单应稳定有序，便于自检与文档生成。
func TestNamesSorted(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("注册表为空")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("名单未排序: %v", names)
		}
	}
}
