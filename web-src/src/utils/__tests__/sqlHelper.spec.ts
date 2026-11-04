import { describe, expect, it } from 'vitest'
import {
  buildWhereCondition,
  fmtVal,
  getDatePickerKind,
  isBinaryOrLobColumnType,
  isDateOnlyColumnType,
  stripBigNumberMarker,
} from '../sqlHelper'

describe('buildWhereCondition', () => {
  it('MySQL JSON 列显式 CAST 成 JSON，避免对象与字符串比较永不相等', () => {
    expect(buildWhereCondition('data', '{"a": 1}', 'mysql', 'json')).toBe(
      '`data` = CAST(\'{"a": 1}\' AS JSON)'
    )
  })

  it('非 JSON 列保持普通等值比较', () => {
    expect(buildWhereCondition('name', 'A', 'mysql', 'varchar(50)')).toBe("`name` = 'A'")
  })

  it('NULL 条件仍使用 IS NULL', () => {
    expect(buildWhereCondition('data', null, 'mysql', 'JSON')).toBe('`data` IS NULL')
  })

  it('MariaDB 的 JSON 是 LONGTEXT 别名，按文本等值比较', () => {
    expect(buildWhereCondition('data', '{"a": 1}', 'mariadb', 'JSON')).toBe('`data` = \'{"a": 1}\'')
  })

  it('JSON 文本中的单引号会被转义后再 CAST', () => {
    expect(buildWhereCondition('data', '{"name": "O\'Brien"}', 'mysql', 'JSON')).toBe(
      '`data` = CAST(\'{"name": "O\'\'Brien"}\' AS JSON)'
    )
  })

  it('MySQL DATE 条件只保留日期部分', () => {
    expect(buildWhereCondition('d', '2026-01-01T12:34:56', 'mysql', 'DATE')).toBe("`d` = '2026-01-01'")
  })

  it('Oracle DATE 条件使用显式 TO_DATE，且规范化 T 分隔符', () => {
    expect(buildWhereCondition('d', '2026-01-01T12:34:56', 'oracle', 'DATE')).toBe(
      '"D" = TO_DATE(\'2026-01-01 12:34:56\', \'YYYY-MM-DD HH24:MI:SS\')'
    )
  })
})

describe('fmtVal typed literals', () => {
  it('MySQL DATE 截断时间部分', () => {
    expect(fmtVal('2026-01-01T12:34:56', 'mysql', 'DATE')).toBe("'2026-01-01'")
  })

  it('Oracle TIMESTAMP 保留小数秒并使用对应 FF 掩码', () => {
    expect(fmtVal('2026-01-01 12:34:56.123456', 'oracle', 'TIMESTAMP(6)')).toBe(
      "TO_TIMESTAMP('2026-01-01 12:34:56.123456', 'YYYY-MM-DD HH24:MI:SS.FF6')"
    )
  })

  it('Oracle TIMESTAMP 无小数秒时不带 FF 掩码', () => {
    expect(fmtVal('2026-01-01 12:34:56', 'oracle', 'TIMESTAMP(6)')).toBe(
      "TO_TIMESTAMP('2026-01-01 12:34:56', 'YYYY-MM-DD HH24:MI:SS')"
    )
  })

  it('Oracle TIMESTAMP WITH TIME ZONE 使用 TO_TIMESTAMP_TZ 并保留偏移', () => {
    expect(fmtVal('2026-01-01 12:34:56.123456 +08:00', 'oracle', 'TIMESTAMP(6) WITH TIME ZONE')).toBe(
      "TO_TIMESTAMP_TZ('2026-01-01 12:34:56.123456 +08:00', 'YYYY-MM-DD HH24:MI:SS.FF6 TZH:TZM')"
    )
  })

  it('高精度数值按数字字面量输出，避免引号导致精度/NLS 差异', () => {
    expect(fmtVal('12345678901234567890.123456', 'mysql', 'DECIMAL')).toBe('12345678901234567890.123456')
  })

  it('非数值列仍按字符串转义', () => {
    expect(fmtVal('123', 'mysql', 'VARCHAR(10)')).toBe("'123'")
  })
})

describe('stripBigNumberMarker', () => {
  it('数值列的 s: 精度标记会被剥离', () => {
    expect(stripBigNumberMarker('s:9007199254740993', 'BIGINT')).toBe('9007199254740993')
    expect(stripBigNumberMarker('s:12345678901234567.89', 'NUMBER')).toBe('12345678901234567.89')
  })

  it('字符串列的 s: 内容保持不变', () => {
    expect(stripBigNumberMarker('s:9007199254740993', 'VARCHAR(30)')).toBe('s:9007199254740993')
  })

  it('普通值保持不变', () => {
    expect(stripBigNumberMarker('123', 'BIGINT')).toBe('123')
    expect(stripBigNumberMarker(123, 'BIGINT')).toBe(123)
  })
})

describe('column type helpers', () => {
  it('DATE 选择器按方言区分：MySQL 到日，Oracle 含时间', () => {
    expect(getDatePickerKind('DATE', 'mysql')).toBe('date')
    expect(getDatePickerKind('DATE', 'mariadb')).toBe('date')
    expect(getDatePickerKind('DATE', 'oracle')).toBe('datetime')
    expect(getDatePickerKind('DATETIME', 'mysql')).toBe('datetime')
  })

  it('带时区的 TIMESTAMP 回退为文本编辑，避免丢偏移', () => {
    expect(getDatePickerKind('TIMESTAMP(6) WITH TIME ZONE', 'oracle')).toBe('')
    expect(getDatePickerKind('TIMESTAMPTZ', 'oracle')).toBe('')
  })

  it('二进制/LOB 类型识别，且不误伤 BINARY_FLOAT/DOUBLE', () => {
    expect(isBinaryOrLobColumnType('BLOB')).toBe(true)
    expect(isBinaryOrLobColumnType('VARBINARY(100)')).toBe(true)
    expect(isBinaryOrLobColumnType('OCIClobLocator')).toBe(true)
    expect(isBinaryOrLobColumnType('POINT')).toBe(true)
    expect(isBinaryOrLobColumnType('BINARY_FLOAT')).toBe(false)
    expect(isBinaryOrLobColumnType('BINARY_DOUBLE')).toBe(false)
    expect(isBinaryOrLobColumnType('VARCHAR(10)')).toBe(false)
  })

  it('isDateOnlyColumnType 只对 MySQL/MariaDB 的 DATE 生效', () => {
    expect(isDateOnlyColumnType('DATE', 'mysql')).toBe(true)
    expect(isDateOnlyColumnType('DATE', 'oracle')).toBe(false)
    expect(isDateOnlyColumnType('DATETIME', 'mysql')).toBe(false)
  })
})
