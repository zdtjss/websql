import { describe, expect, it } from 'vitest'
import { isJdbcUrl, parseJdbcUrl } from '../jdbcUrl'

describe('isJdbcUrl', () => {
  it('仅 jdbc: 前缀（含空格/大小写）触发识别', () => {
    expect(isJdbcUrl('jdbc:mysql://h/db')).toBe(true)
    expect(isJdbcUrl('  JDBC:Oracle:thin:@//h:1521/orcl')).toBe(true)
    expect(isJdbcUrl('tcp(127.0.0.1:3306)/demo')).toBe(false)
    expect(isJdbcUrl('')).toBe(false)
  })

  it('非 jdbc 输入返回 null，调用方按原格式透传', () => {
    expect(parseJdbcUrl('10.0.0.1:3306/demo')).toBeNull()
    expect(parseJdbcUrl('')).toBeNull()
  })
})

describe('parseJdbcUrl —— MySQL', () => {
  it('拆出主机/端口/库名，并把 JDBC 参数映射为驱动参数', () => {
    const r = parseJdbcUrl(
      'jdbc:mysql://127.0.0.1:3306/demo?useUnicode=true&characterEncoding=utf8' +
      '&serverTimezone=Asia/Shanghai&connectTimeout=5000&socketTimeout=30000&allowMultiQueries=true'
    )!
    expect(r.ok).toBe(true)
    expect(r.dbType).toBe('mysql')
    expect(r.host).toBe('127.0.0.1')
    expect(r.port).toBe(3306)
    expect(r.schema).toBe('demo')
    // 裸 host:port/db 后端驱动不认，必须落成 tcp(addr)/db 形式
    expect(r.dsn).toBe('tcp(127.0.0.1:3306)/demo?charset=utf8mb4&timeout=5s&readTimeout=30s&multiStatements=true')
    const dropped = r.params.filter(p => p.action === 'dropped').map(p => p.source)
    expect(dropped).toEqual(['useUnicode', 'serverTimezone'])
  })

  it('未知参数一律丢弃（透传会被驱动当作 SET 语句执行）', () => {
    const r = parseJdbcUrl('jdbc:mysql://h/db?totallyUnknown=1&parseTime=true')!
    expect(r.params.find(p => p.source === 'totallyUnknown')?.action).toBe('dropped')
    expect(r.params.find(p => p.source === 'parseTime')?.action).toBe('kept')
    expect(r.dsn).toBe('tcp(h:3306)/db?parseTime=true')
  })

  it('支持 userinfo、address=(...) 形式与多主机列表', () => {
    const withUser = parseJdbcUrl('jdbc:mysql://root:pw@db.internal/testdb')!
    expect(withUser.user).toBe('root')
    expect(withUser.hasPassword).toBe(true)
    expect(withUser.dsn).toBe('tcp(db.internal:3306)/testdb')

    const addr = parseJdbcUrl('jdbc:mysql://address=(protocol=tcp)(host=127.0.0.1)(port=3307)/demo')!
    expect(addr.dsn).toBe('tcp(127.0.0.1:3307)/demo')

    const multi = parseJdbcUrl('jdbc:mysql://h1:3306,h2:3306/demo')!
    expect(multi.host).toBe('h1')
    expect(multi.warnings.join()).toContain('多主机')
  })

  it('省略主机时按 127.0.0.1 处理并提示', () => {
    const r = parseJdbcUrl('jdbc:mysql:///demo')!
    expect(r.host).toBe('127.0.0.1')
    expect(r.warnings.join()).toContain('127.0.0.1')
  })

  it('IPv6 主机在连接串中保留方括号', () => {
    const r = parseJdbcUrl('jdbc:mysql://[::1]:3307/demo')!
    expect(r.host).toBe('::1')
    expect(r.dsn).toBe('tcp([::1]:3307)/demo')
  })

  it('mariadb 子协议识别为 mariadb', () => {
    const r = parseJdbcUrl('jdbc:mariadb://h/mydb')!
    expect(r.dbType).toBe('mariadb')
    expect(r.dsn).toBe('tcp(h:3306)/mydb')
  })

  it('readOnly 提示改用系统的只读开关', () => {
    const r = parseJdbcUrl('jdbc:mysql://h/db?readOnly=true')!
    expect(r.params[0].note).toContain('只读连接')
  })

  it('同名参数保留但取值会规范化', () => {
    const r = parseJdbcUrl('jdbc:mysql://h/db?charset=UTF8&loc=Asia%2FShanghai')!
    expect(r.dsn).toBe('tcp(h:3306)/db?charset=utf8mb4')
    expect(r.params.find(p => p.source === 'loc')?.action).toBe('dropped')
  })
})

describe('parseJdbcUrl —— Oracle', () => {
  it('双斜杠形式是服务名', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:@//10.0.0.2:1521/ORCL')!
    expect(r.dbType).toBe('oracle')
    expect(r.schema).toBe('ORCL')
    expect(r.sid).toBe('')
    expect(r.dsn).toBe('10.0.0.2:1521/ORCL')
  })

  it('冒号第三段是 SID，落成 ?sid= 写法', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:@10.0.0.2:1521:orcl')!
    expect(r.sid).toBe('orcl')
    expect(r.dsn).toBe('10.0.0.2:1521?SID=orcl')
  })

  it('内嵌 user/pass 时可识别用户名与口令', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:scott/tiger@//h:1521/orclpdb')!
    expect(r.user).toBe('scott')
    expect(r.hasPassword).toBe(true)
  })

  it('超时参数按秒换算，未知选项丢弃（go-ora 会报 unknown URL option）', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:@//h:1521/orcl?oracle.jdbc.ReadTimeout=30000&foo=bar&charset=ZHS16GBK')!
    // 键名含空格时需 URL 编码，go-ora 解析后会还原成大写选项名
    expect(r.dsn).toBe('h:1521/orcl?read%20timeout=30')
    expect(r.params.find(p => p.source === 'foo')?.action).toBe('dropped')
    expect(r.params.find(p => p.source === 'charset')?.action).toBe('dropped')
  })

  it('TNS DESCRIPTION 取第一个地址与服务名', () => {
    const r = parseJdbcUrl(
      'jdbc:oracle:thin:@(DESCRIPTION=(ADDRESS_LIST=(ADDRESS=(PROTOCOL=TCP)(HOST=scan1)(PORT=1521))' +
      '(ADDRESS=(PROTOCOL=TCP)(HOST=scan2)(PORT=1522)))(CONNECT_DATA=(SERVICE_NAME=orcl)))'
    )!
    expect(r.host).toBe('scan1')
    expect(r.port).toBe(1521)
    expect(r.schema).toBe('orcl')
    expect(r.dsn).toBe('scan1:1521/orcl')
    expect(r.warnings.join()).toContain('2 个地址')
  })

  it('带下划线的 JDBC 选项名亦能映射', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:@//h:1521/orcl?oracle.net.CONNECT_TIMEOUT=8000&prefetch_rows=100')!
    expect(r.dsn).toBe('h:1521/orcl?connect%20timeout=8&PREFETCH_ROWS=100')
  })

  it('缺主机 / TNS 别名时判定为解析失败', () => {
    const r = parseJdbcUrl('jdbc:oracle:thin:@MYTNSALIAS')!
    expect(r.ok).toBe(false)
    expect(r.error).toContain('tnsnames')
  })
})

describe('parseJdbcUrl —— SQLite 与其他厂商', () => {
  it('SQLite 只保留文件路径，xerial 参数换算为 modernc 写法', () => {
    const r = parseJdbcUrl(
      'jdbc:sqlite:D:\\data\\x.db?journal_mode=WAL&busy_timeout=5000&date_string_format=yyyy-MM-dd HH:mm:ss.SSS&date_class=java.util.Date'
    )!
    expect(r.dbType).toBe('sqlite')
    expect(r.schema).toBe('D:\\data\\x.db')
    expect(r.dsn).toContain('D:\\data\\x.db?')
    expect(r.dsn).toContain('_pragma=journal_mode(WAL)')
    expect(r.dsn).toContain('_pragma=busy_timeout(5000)')
    expect(r.dsn).toContain('_time_format=2006-01-02%2015%3A04%3A05.000')
    expect(r.params.find(p => p.source === 'date_class')?.action).toBe('dropped')
  })

  it('未接入的厂商给出明确原因，并回显已解析出的信息', () => {
    const r = parseJdbcUrl('jdbc:postgresql://h:5432/analytics')!
    expect(r.supported).toBe(false)
    expect(r.ok).toBe(false)
    expect(r.error).toContain('postgresql')
    expect(r.host).toBe('h')
    expect(r.port).toBe(5432)
    expect(r.schema).toBe('analytics')
    expect(r.dsn).toBe('')
  })

  it('未知厂商直接报错', () => {
    const r = parseJdbcUrl('jdbc:whatever://h/db')!
    expect(r.ok).toBe(false)
    expect(r.error).toContain('whatever')
  })
})
