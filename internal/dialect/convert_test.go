package dialect

import (
	"testing"
	"time"
)

func convert(t *testing.T, dbType, colType string, val any, overSign bool) any {
	t.Helper()
	got := ConvertValue(dbType, &colType, &val, overSign)
	if got == nil {
		t.Fatalf("%s 转换返回 nil", colType)
	}
	return *got
}

// 高精度 DECIMAL/NUMBER 不能被 float64 截断，必须保留原始文本（overSign=true 时加 s: 标记）。
func TestConvertDecimalPreservesPrecision(t *testing.T) {
	raw := []byte("12345678901234567890.123456")

	for _, dbType := range []string{"mysql", "mariadb"} {
		if got := convert(t, dbType, "DECIMAL", raw, true); got != "s:12345678901234567890.123456" {
			t.Errorf("%s DECIMAL 高精度应保留原文，实际 %+v", dbType, got)
		}
	}
	if got := convert(t, "oracle", "NUMBER", raw, true); got != "s:12345678901234567890.123456" {
		t.Errorf("oracle NUMBER 高精度应保留原文，实际 %+v", got)
	}

	// 普通精度仍按数值返回，避免前端展示为字符串
	if got := convert(t, "mysql", "DECIMAL", []byte("123.45"), true); got != float64(123.45) {
		t.Errorf("普通 DECIMAL 应返回 float64，实际 %+v", got)
	}
	if got := convert(t, "oracle", "NUMBER", []byte("42"), true); got != int64(42) {
		t.Errorf("oracle 整数 NUMBER 应返回 int64，实际 %+v", got)
	}
}

// 时间字段读取必须按列类型保留精度：DATE 到日，DATETIME/TIMESTAMP 保留小数秒，带时区列保留偏移。
func TestConvertTimePreservesPrecision(t *testing.T) {
	dt := time.Date(2026, 1, 1, 12, 0, 0, 123456000, time.UTC)
	if got := convert(t, "mysql", "DATETIME", dt, true); got != "2026-01-01 12:00:00.123456" {
		t.Errorf("mysql DATETIME 应保留小数秒，实际 %+v", got)
	}
	if got := convert(t, "mysql", "DATE", dt, true); got != "2026-01-01" {
		t.Errorf("mysql DATE 应只到日，实际 %+v", got)
	}
	if got := convert(t, "oracle", "DATE", dt, true); got != "2026-01-01 12:00:00" {
		t.Errorf("oracle DATE 不应带小数秒，实际 %+v", got)
	}
	if got := convert(t, "oracle", "TIMESTAMP(6)", dt, true); got != "2026-01-01 12:00:00.123456" {
		t.Errorf("oracle TIMESTAMP 应保留小数秒，实际 %+v", got)
	}

	loc := time.FixedZone("CST", 8*3600)
	tzTime := time.Date(2026, 1, 1, 12, 0, 0, 123456000, loc)
	if got := convert(t, "oracle", "TIMESTAMP(6) WITH TIME ZONE", tzTime, true); got != "2026-01-01 12:00:00.123456 +08:00" {
		t.Errorf("oracle TIMESTAMP WITH TIME ZONE 应带偏移，实际 %+v", got)
	}
}
