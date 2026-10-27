import { describe, expect, it } from 'vitest'
import { buildGridText, toSqlLiteral } from '../gridCopy'

const columns = ['id', 'name', 'note']
const rows = [
  { id: 1, name: 'A', note: null },
  { id: 2, name: '含,逗号', note: '带"引号"' },
]

describe('buildGridText', () => {
  it('TSV：制表符分隔，NULL 输出空串', () => {
    const text = buildGridText({ format: 'tsv', columns, rows })
    expect(text).toBe('1\tA\t\n2\t含,逗号\t带"引号"')
  })

  it('CSV：带表头，含逗号/引号的字段加引号', () => {
    const text = buildGridText({ format: 'csv', columns, rows })
    const lines = text.split('\n')
    expect(lines[0]).toBe('id,name,note')
    expect(lines[1]).toBe('1,A,')
    expect(lines[2]).toBe('2,"含,逗号","带""引号"""')
  })

  it('CSV：可关闭表头', () => {
    const text = buildGridText({ format: 'csv', columns, rows, withHeader: false })
    expect(text.split('\n')).toHaveLength(2)
  })

  it('JSON：保持列顺序，NULL 保留为 null', () => {
    const text = buildGridText({ format: 'json', columns, rows })
    const parsed = JSON.parse(text)
    expect(parsed).toHaveLength(2)
    expect(Object.keys(parsed[0])).toEqual(columns)
    expect(parsed[0].note).toBeNull()
  })

  it('Markdown：转义管道符与换行', () => {
    const text = buildGridText({
      format: 'markdown',
      columns: ['a', 'b'],
      rows: [{ a: 'x|y', b: 'l1\nl2' }],
    })
    const lines = text.split('\n')
    expect(lines[0]).toBe('| a | b |')
    expect(lines[1]).toBe('| --- | --- |')
    expect(lines[2]).toBe('| x\\|y | l1<br>l2 |')
  })

  it('INSERT：按方言引用标识符与字面量', () => {
    const text = buildGridText({
      format: 'insert',
      columns: ['id', 'name'],
      rows: [{ id: 1, name: "O'Brien" }],
      table: 't_user',
      dbType: 'mysql',
    })
    expect(text).toBe("INSERT INTO `t_user` (`id`, `name`) VALUES (1, 'O''Brien');")
  })

  it('INSERT：Oracle 使用双引号标识符', () => {
    const text = buildGridText({
      format: 'insert',
      columns: ['id'],
      rows: [{ id: 1 }],
      table: 't_user',
      dbType: 'oracle',
    })
    expect(text).toBe('INSERT INTO "t_user" ("id") VALUES (1);')
  })

  it('空数据返回空串', () => {
    expect(buildGridText({ format: 'tsv', columns: [], rows })).toBe('')
    expect(buildGridText({ format: 'tsv', columns, rows: [] })).toBe('')
  })
})

describe('toSqlLiteral', () => {
  it('NULL / 数字 / 布尔', () => {
    expect(toSqlLiteral(null, 'mysql')).toBe('NULL')
    expect(toSqlLiteral(undefined, 'mysql')).toBe('NULL')
    expect(toSqlLiteral(42, 'mysql')).toBe('42')
    expect(toSqlLiteral(true, 'mysql')).toBe('1')
    expect(toSqlLiteral(false, 'mysql')).toBe('0')
  })

  it('MySQL 额外转义反斜杠，Oracle 不转义', () => {
    expect(toSqlLiteral('a\\b', 'mysql')).toBe("'a\\\\b'")
    expect(toSqlLiteral('a\\b', 'oracle')).toBe("'a\\b'")
  })

  it('单引号转义为两个单引号', () => {
    expect(toSqlLiteral("it's", 'mysql')).toBe("'it''s'")
  })
})
