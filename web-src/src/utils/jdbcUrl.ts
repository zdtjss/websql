/**
 * JDBC 连接串识别与规范化
 *
 * 背景：系统的连接配置最终落库的是后端驱动可直接使用的连接串（ConnCfg.Url），
 * 各类型格式并不相同：
 *   mysql/mariadb  tcp(主机:端口)/库名?参数     —— go-sql-driver/mysql
 *   oracle         主机:端口/服务名 或 主机:端口?sid=实例名 —— sijms/go-ora
 *   sqlite         文件路径?_pragma=...          —— modernc.org/sqlite
 * 而 Java 生态里流传的 `jdbc:mysql://...` 一类的串后端并不认识，
 * 且 JDBC 参数名（characterEncoding、socketTimeout …）与 Go 驱动参数名并不一致。
 *
 * 本文件在输入以 `jdbc:` 开头时动态识别：拆出主机 / 端口 / 库(schema) / 参数，
 * 并把 JDBC 参数「翻译」成驱动认识的参数名。解析结果只服务当前编辑会话，
 * 不额外落库（提交时仅把规范化后的连接串写入既有的 url 字段）。
 *
 * 注意两个驱动的硬约束，决定了「未知参数必须丢弃而不是原样透传」：
 * - go-ora 遇到未登记的 URL 选项直接报 `unknown URL option`；
 * - go-sql-driver 把未识别参数当作 `SET @@session.<key>` 执行，多数会报未知系统变量。
 */

/** 系统当前支持的数据库类型（与 ConnManagement 的 dbTypeList 对齐） */
export type SupportedDbType = 'mysql' | 'mariadb' | 'oracle' | 'sqlite'

/** 单个 JDBC 参数的处理结果 */
export interface JdbcParamEntry {
  /** 原始参数名 */
  source: string
  /** 原始参数值 */
  value: string
  /** mapped=改名/换算后下发，kept=同名保留，dropped=不下发 */
  action: 'mapped' | 'kept' | 'dropped'
  /** 落地的驱动参数名（dropped 时为空） */
  target: string
  /** 落地的驱动参数值（已按驱动要求换算） */
  targetValue: string
  /** 改名、保留或丢弃的原因，供界面提示 */
  note: string
}

/** JDBC 解析结果 */
export interface JdbcParseResult {
  /** 解析是否成功（失败时 error 给出原因） */
  ok: boolean
  error: string
  /** jdbc scheme 中的厂商名，如 mysql / oracle / postgresql */
  vendor: string
  /** 识别出的系统数据库类型；不支持时为 '' */
  dbType: SupportedDbType | ''
  /** 该厂商是否为本系统支持的类型 */
  supported: boolean
  host: string
  port: number | null
  /** MySQL 库名 / Oracle 服务名 / SQLite 文件路径 */
  schema: string
  /** Oracle 冒号形式下的实例名（SID） */
  sid: string
  /** 串内内嵌的用户名（如有） */
  user: string
  /** 串内是否带了口令（口令不回填空着，仅提示） */
  hasPassword: boolean
  /** 可直接提交后端的连接串（后端自行拼装 user/pwd 前缀） */
  dsn: string
  params: JdbcParamEntry[]
  warnings: string[]
}

/** go-sql-driver/mysql v1.10 认识的同名参数，原样保留 */
const MYSQL_KNOWN_PARAMS = new Set([
  'allowAllFiles', 'allowCleartextPasswords', 'allowFallbackToPlaintext', 'allowNativePasswords',
  'allowOldPasswords', 'checkConnLiveness', 'clientFoundRows', 'charset', 'collation',
  'columnsWithAlias', 'compress', 'interpolateParams', 'loc', 'multiStatements', 'parseTime',
  'timeTruncate', 'readTimeout', 'rejectReadOnly', 'serverPubKey', 'strict', 'timeout', 'tls',
  'writeTimeout', 'maxAllowedPacket', 'connectionAttributes'
])

/** JDBC 专有、Go 驱动没有对应实现的参数（性能/日志类），统一丢弃并说明 */
const MYSQL_JDBC_ONLY_PARAMS = new Set([
  'autoReconnect', 'useUsageAdvisor', 'profileSQL', 'traceProtocol', 'slowQueryThresholdMillis',
  'slowQueryThresholdNanos', 'maxQuerySizeToLog', 'logger', 'queryInterceptors',
  'statementInterceptors', 'connectionLifecycleInterceptors', 'emulateUnsupportedPstmts',
  'generateSimplePreparedStatement', 'noAccessToProcedureBodies', 'alwaysSendSetIsolation',
  'cachePrepStmts', 'prepStmtCacheSize', 'prepStmtCacheSqlLimit', 'useServerPrepStmts',
  'rewriteBatchedStatements', 'detectCustomCollations', 'useLocalTransactionState',
  'maintainTimeStats', 'queryTimeoutSqlException', 'servicesLookupFail', 'statementCacheMaxSize'
])

/** go-ora 认识的 URL 选项：小写别名 → 驱动期望的写法（含空格的需按 URL 编码下发） */
const ORACLE_KNOWN_PARAMS: Record<string, string> = {
  sid: 'SID', servicename: 'SERVICE NAME', connstr: 'CONNSTR', instancename: 'INSTANCE NAME',
  cid: 'CID', server: 'SERVER', wallet: 'WALLET', walletpassword: 'WALLET PASSWORD',
  authtype: 'AUTH TYPE', osuser: 'OS USER', ospass: 'OS PASS', password: 'OS PASSWORD',
  oshash: 'OS HASH', domain: 'DOMAIN', authserv: 'AUTH SERV', encryption: 'ENCRYPTION',
  dataintegrity: 'DATA INTEGRITY', ssl: 'SSL', sslverify: 'SSL VERIFY', dbaprivilege: 'DBA PRIVILEGE',
  timeout: 'TIMEOUT', readtimeout: 'READ TIMEOUT', sockettimeout: 'SOCKET TIMEOUT',
  connecttimeout: 'CONNECT TIMEOUT', connectiontimeout: 'CONNECTION TIMEOUT',
  tracefile: 'TRACE FILE', tracedir: 'TRACE DIR', prefetchrows: 'PREFETCH_ROWS',
  unixsocket: 'UNIX SOCKET', proxyclientname: 'PROXY CLIENT NAME', lob: 'LOB FETCH',
  language: 'LANGUAGE', territory: 'TERRITORY', charset: 'CHARSET',
  clientcharset: 'CLIENT CHARSET', program: 'PROGRAM', serverlocation: 'SERVER LOCATION',
  location: 'SERVER LOCATION'
}

/** JDBC 常见写法 → go-ora 选项（含单位换算）；键为去掉空格/下划线/连字符后的小写形式 */
const ORACLE_RENAMES: Record<string, string> = {
  'oracle.jdbc.readtimeout': 'read timeout',
  'oracle.net.connecttimeout': 'connect timeout',
  'oracle.jdbc.defaultconnectiontimeout': 'connect timeout',
  'driverlogintimeout': 'connect timeout',
  'oracle.jdbc.timezoneasregion': 'timezoneasregion',
  'oracle.net.sslversion': 'ssl_version',
  'oracle.net.sslwalletlocation': 'wallet',
  'oracle.net.sslwalletpassword': 'wallet password'
}

/** 会被当作 PRAGMA 下发的 SQLite 参数（xerial JDBC 写法 → modernc 的 _pragma 形式） */
const SQLITE_PRAGMAS = new Set([
  'journal_mode', 'busy_timeout', 'synchronous', 'foreign_keys', 'cache_size', 'temp_store',
  'encoding', 'case_sensitive_like', 'locking_mode', 'page_size', 'read_uncommitted',
  'recursive_triggers', 'default_synchronous', 'secure_delete'
])

/** modernc.org/sqlite 认识的 _xxx 与 URI 参数 */
const SQLITE_KNOWN_PARAMS = new Set([
  '_pragma', '_time_format', '_time_integer_format', '_timezone', '_txlock', '_inttotime',
  '_texttotime', '_query_only', '_dqs', '_error_rc', 'mode', 'cache', 'vfs', 'nolock',
  'immutable', '-V'
])

/** xerial sqlite-jdbc 专有、modernc 无对应实现的参数 */
const SQLITE_JDBC_ONLY_PARAMS = new Set([
  'date_class', 'date_precision', 'config', 'open_mode',
  'enable_load_extension', 'vm_busy_timeout', 'transaction_mode', 'jdbc_compatible'
])

const DEFAULT_PORTS: Record<string, number> = {
  mysql: 3306, mariadb: 3306, oracle: 1521
}

/** TNS DESCRIPTION 中结构/调度类关键字：不参与主机与库名提取，也不报「未支持」 */
const TNS_IGNORED_KEYS = [
  'HOST', 'PORT', 'SERVICE_NAME', 'SID', 'PROTOCOL', 'ADDRESS', 'ADDRESS_LIST', 'CONNECT_DATA',
  'DESCRIPTION', 'DESCRIPTION_LIST', 'LOAD_BALANCE', 'FAILOVER', 'SERVER', 'INSTANCE_NAME',
  'GLOBAL_DBNAME', 'PURGE', 'SOURCE_ADDRESS', 'RETRY_COUNT', 'CONNECT_TIMEOUT'
]

/** 是否 jdbc 开头的连接串 */
export function isJdbcUrl(raw: string): boolean {
  return /^\s*jdbc\s*:/i.test(raw || '')
}

/**
 * 解析 jdbc 串。非 jdbc 输入返回 null（调用方按原有格式直接透传，行为不变）。
 */
export function parseJdbcUrl(raw: string): JdbcParseResult | null {
  const text = (raw || '').trim()
  if (!isJdbcUrl(text)) return null

  const body = text.replace(/^\s*jdbc\s*:\s*/i, '')
  const schemeEnd = body.indexOf(':')
  const vendor = (schemeEnd === -1 ? body : body.slice(0, schemeEnd)).toLowerCase()
  const rest = schemeEnd === -1 ? '' : body.slice(schemeEnd + 1)

  const result = emptyResult(vendor)
  switch (vendor) {
    case 'mysql':
    case 'mariadb':
      return parseMysqlLike(result, vendor, rest)
    case 'oracle':
      return parseOracle(result, rest)
    case 'sqlite':
    case 'sqlite3':
    case 'xerial':
      return parseSqlite(result, rest)
    case 'postgresql':
    case 'postgres':
    case 'sqlserver':
    case 'jtds':
    case 'microsoft':
    case 'dm':
    case 'kingbase':
    case 'kingbase8':
    case 'db2':
    case 'h2':
    case 'clickhouse':
    case 'odbc':
      return parseUnsupported(result, vendor, rest)
    default:
      result.ok = false
      result.error = `暂不识别的 JDBC 厂商「${vendor || '未知'}」。当前支持 MySQL/MariaDB、Oracle、SQLite`
      return result
  }
}

function emptyResult(vendor: string): JdbcParseResult {
  return {
    ok: true, error: '', vendor, dbType: '', supported: false, host: '', port: null,
    schema: '', sid: '', user: '', hasPassword: false, dsn: '', params: [], warnings: []
  }
}

// ===== MySQL / MariaDB =====

function parseMysqlLike(r: JdbcParseResult, vendor: string, rest: string): JdbcParseResult {
  r.dbType = vendor === 'mariadb' ? 'mariadb' : 'mysql'
  r.supported = true
  const host = stripScheme(r, rest)
  const { base, query } = splitQuery(host, /[?;]/)
  const cut = findTopLevel(base, '/')
  const authority = cut === -1 ? base : base.slice(0, cut)
  const dbname = cut === -1 ? '' : base.slice(cut + 1)

  r.schema = decodeURIComponentSafe(dbname.split('?')[0])
  readUserinfo(r, authority)
  // 主机部分需剔除 userinfo，否则 user:pass@host 会被当成 host:port 解析
  let hostPart = authority.includes('@') ? authority.slice(authority.lastIndexOf('@') + 1) : authority
  if (hostPart.includes('(')) {
    // 形式二：jdbc:mysql://address=(protocol=tcp)(host=127.0.0.1)(port=3306)/db
    const pairs = parseParenPairs(hostPart)
    hostPart = pairs['host'] || pairs['address'] || ''
    const p = pairs['port']
    if (p) r.port = toPort(r, p)
  }
  if (hostPart.includes(',')) {
    // 多主机（故障转移/负载均衡）：驱动不支持该写法，取第一个并提示
    r.warnings.push(`检测到多主机列表（${hostPart}），MySQL 驱动不支持故障转移写法，已取第一个地址`)
    hostPart = hostPart.split(',')[0].trim()
  }
  if (hostPart.startsWith('[')) {
    // IPv6：[::1]:3306
    const end = hostPart.indexOf(']')
    if (end !== -1) {
      r.host = hostPart.slice(1, end)
      const tail = hostPart.slice(end + 1)
      if (tail.startsWith(':')) r.port = toPort(r, tail.slice(1))
    }
  } else if (hostPart.includes(':')) {
    const idx = hostPart.lastIndexOf(':')
    r.host = hostPart.slice(0, idx).trim()
    r.port = toPort(r, hostPart.slice(idx + 1))
  } else {
    r.host = hostPart.trim()
  }

  if (!r.host) {
    r.host = '127.0.0.1'
    r.warnings.push('连接串未指定主机，已按 127.0.0.1 处理')
  }
  if (!r.port) r.port = DEFAULT_PORTS[r.dbType]

  r.params = mapMysqlParams(query)
  const qs = buildQueryString(r.params)
  const db = r.schema ? encodeURIComponent(r.schema) : ''
  // 后端拼的是 user:pwd@ + url，因此必须是驱动要求的 net(addr) 形式，
  // 裸 host:port/db 会被 go-sql-driver 当成网络名解析失败。
  r.dsn = `tcp(${bracketHost(r.host)}:${r.port})/${db}${qs ? `?${qs}` : ''}`
  return r
}

/** 去掉 `//` 或 `replication://`、`loadbalance://` 等子协议前缀 */
function stripScheme(r: JdbcParseResult, rest: string): string {
  const s = rest.trim()
  const m = s.match(/^([a-zA-Z][\w-]*):\/\//)
  if (m) {
    // 故障转移/负载均衡子协议驱动不支持，只保留地址部分继续按单主机解析
    r.warnings.push(`子协议「${m[1].toLowerCase()}」（故障转移/负载均衡）不受 Go 驱动支持，已按单主机连接处理`)
    return s.slice(m[0].length)
  }
  return s.startsWith('//') ? s.slice(2) : s
}

/** 读取 authority 中的 userinfo（`user:pass@host`）；口令只提示不回传 */
function readUserinfo(r: JdbcParseResult, authority: string): void {
  const at = authority.lastIndexOf('@')
  if (at === -1) return
  const info = authority.slice(0, at)
  const idx = info.indexOf(':')
  if (idx === -1) {
    r.user = decodeURIComponentSafe(info)
    return
  }
  r.user = decodeURIComponentSafe(info.slice(0, idx))
  r.hasPassword = info.slice(idx + 1).length > 0
}

/** 解析 `(host=abc)(port=3306)` 形式的键值对 */
function parseParenPairs(s: string): Record<string, string> {
  const out: Record<string, string> = {}
  const re = /\(([^)=]+)=([^)]*)\)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(s)) !== null) {
    out[m[1].trim().toLowerCase()] = m[2].trim().replace(/^["']|["']$/g, '')
  }
  return out
}

/** JDBC 参数 → go-sql-driver 参数 */
function mapMysqlParams(query: string): JdbcParamEntry[] {
  const entries: JdbcParamEntry[] = []
  for (const [k, v] of splitPairs(query)) {
    const key = k.trim()
    if (!key) continue
    const lower = key.toLowerCase()

    if (lower === 'characterencoding') {
      entries.push(entry(key, v, 'mapped', 'charset', normalizeMysqlCharset(v),
        `JDBC 字符集名 ${v || '空'} 已换算为驱动的 charset 取值`))
      continue
    }
    if (lower === 'useunicode') {
      entries.push(entry(key, v, 'dropped', '', '', 'Go 驱动始终按连接字符集传输，无需该参数'))
      continue
    }
    if (lower === 'servertimezone') {
      const val = (v || '').toLowerCase()
      if (val === 'utc' || val === 'local') {
        entries.push(entry(key, v, 'mapped', 'loc', val, '时区名为 Go 可直接加载的形式，已映射为 loc'))
      } else {
        entries.push(entry(key, v, 'dropped', '', '',
          '后端未内嵌时区库（Windows 下无法加载 Asia/Shanghai 一类人名时区），置入 loc 会直接连接失败；时间字段按服务器原值读取'))
      }
      continue
    }
    if (lower === 'allowmultiqueries') {
      entries.push(entry(key, v, 'mapped', 'multiStatements', toBool(v), 'allowMultiQueries 对应驱动的 multiStatements'))
      continue
    }
    if (lower === 'multistatements') {
      entries.push(entry(key, v, 'mapped', 'multiStatements', toBool(v), '同 multiStatements'))
      continue
    }
    if (lower === 'connecttimeout') {
      const d = msToDuration(v)
      entries.push(d
        ? entry(key, v, 'mapped', 'timeout', d, 'JDBC 单位为毫秒，驱动需 Go 时长写法')
        : entry(key, v, 'dropped', '', '', '超时取值非毫秒整数，无法换算为 Go 时长'))
      continue
    }
    if (lower === 'sockettimeout') {
      const d = msToDuration(v)
      entries.push(d
        ? entry(key, v, 'mapped', 'readTimeout', d, 'JDBC socketTimeout 对应驱动的 readTimeout（毫秒→Go 时长）')
        : entry(key, v, 'dropped', '', '', '超时取值非毫秒整数，无法换算为 Go 时长'))
      continue
    }
    if (lower === 'usessl') {
      if (toBool(v) === 'true') {
        entries.push(entry(key, v, 'mapped', 'tls', 'true', 'useSSL=true 需用 tls 参数表达；自签证书请改为 tls=skip-verify'))
      } else {
        entries.push(entry(key, v, 'dropped', '', '', '默认即为不加密连接'))
      }
      continue
    }
    if (lower === 'sslmode') {
      const map: Record<string, [string, string] | null> = {
        disabled: null,
        none: null,
        preferred: ['tls', 'preferred'],
        required: ['tls', 'true'],
        verify_ca: ['tls', 'true'],
        verify_identity: ['tls', 'true']
      }
      const hit = map[(v || '').toLowerCase().replace(/-/g, '_')]
      if (hit) {
        entries.push(entry(key, v, 'mapped', hit[0], hit[1],
          hit[1] === 'true' ? '已转为 tls=true（校验 CA 需服务端证书在系统信任库内）' : '已转为 tls=preferred（允许回退明文）'))
      } else {
        entries.push(entry(key, v, 'dropped', '', '', '该 sslMode 取值等价于不启用 TLS'))
      }
      continue
    }
    if (lower === 'verifyservercertificate' || lower === 'requiressl' || lower === 'ssltruststore' ||
      lower === 'sslkeystore' || lower === 'sslcipher' || lower === 'fipsenabled') {
      entries.push(entry(key, v, 'dropped', '', '', 'TLS 细节由 Go 驱动 tls 参数与系统信任库决定，JDBC 专有写法不下发'))
      continue
    }
    if (lower === 'allowpublickeyretrieval') {
      entries.push(entry(key, v, 'dropped', '', '',
        'Go 驱动以 allowNativePasswords 处理 caching_sha2_password；若报 public key 错误请显式加 allowNativePasswords=true'))
      continue
    }
    if (lower === 'zerodatetimebehavior' || lower === 'tinyint1isbit' ||
      lower === 'transformedbitisboolean' || lower === 'nullcatalogmeanscurrent' || lower === 'usecursorfetch' ||
      lower === 'defaultfetchsize' || lower === 'sessionvariables' ||
      lower === 'readonly' || lower === 'failoverreadonly') {
      const note = lower === 'readonly'
        ? '请用对话框中的「只读连接」开关（写语句由应用层拦截）'
        : 'JDBC 客户端专有行为参数，Go 驱动无同名实现'
      entries.push(entry(key, v, 'dropped', '', '', note))
      continue
    }
    if (MYSQL_JDBC_ONLY_PARAMS.has(lower)) {
      entries.push(entry(key, v, 'dropped', '', '', 'JDBC 客户端性能/日志参数，Go 驱动无对应实现'))
      continue
    }
    if (MYSQL_KNOWN_PARAMS.has(key)) {
      if (lower === 'charset') {
        entries.push(entry(key, v, 'kept', 'charset', normalizeMysqlCharset(v), '驱动同名参数，字符集取值已按 MySQL charset 名规范化'))
        continue
      }
      if (lower === 'loc' && (v || '').toLowerCase() !== 'utc' && (v || '').toLowerCase() !== 'local') {
        // 同 serverTimezone：后端未内嵌时区库，命名人像时区会直接连不上
        entries.push(entry(key, v, 'dropped', '', '', '后端未内嵌时区库，loc 仅接受 UTC / Local'))
        continue
      }
      entries.push(entry(key, v, 'kept', key, v, '驱动同名参数，原样下发'))
      continue
    }
    // 兜底：未知参数若透传，驱动会当作 SET @@session.<key> 执行而报错
    entries.push(entry(key, v, 'dropped', '', '', `「${key}」不是 Go MySQL 驱动认识的参数，透传会被当作 SET 语句执行而报错，已丢弃`))
  }
  return entries
}

function normalizeMysqlCharset(v: string): string {
  const s = (v || '').toLowerCase().replace(/[-_]/g, '')
  if (s === 'utf8' || s === 'utf8mb4' || s === 'utf8mb3') return 'utf8mb4'
  if (s === 'gbk' || s === 'big5' || s === 'latin1') return s
  return (v || '').toLowerCase()
}

// ===== Oracle =====

function parseOracle(r: JdbcParseResult, rest: string): JdbcParseResult {
  r.dbType = 'oracle'
  r.supported = true
  let s = rest.trim()
  const thin = s.match(/^(thin|oci|emv|kznb|zoneless):/i)
  if (thin) s = s.slice(thin[0].length)

  if (s.startsWith('@')) s = s.slice(1)
  // user/pass@connect 形式
  const atIdx = s.indexOf('@')
  if (atIdx > 0 && !s.startsWith('(')) {
    const info = s.slice(0, atIdx)
    const slash = info.indexOf('/')
    r.user = decodeURIComponentSafe(slash === -1 ? info : info.slice(0, slash))
    r.hasPassword = slash !== -1 && info.slice(slash + 1).length > 0
    s = s.slice(atIdx + 1)
  }

  if (s.startsWith('(')) {
    const tns = splitQuery(s, '?')
    parseTns(r, tns.base)
    r.params = mapOracleParams(tns.query)
  } else {
    const body = s.startsWith('//') ? s.slice(2) : s
    const { base, query } = splitQuery(body, '?')
    // base 可能是 host:port/svc、host:port:SID、host/svc、svc
    const slashIdx = base.indexOf('/')
    let hostPort = slashIdx === -1 ? base : base.slice(0, slashIdx)
    const after = slashIdx === -1 ? '' : base.slice(slashIdx + 1)

    if (hostPort.startsWith('[')) {
      // IPv6：[::1]:1521/orcl
      const end = hostPort.indexOf(']')
      r.host = hostPort.slice(1, end)
      const tail = hostPort.slice(end + 1)
      if (tail.startsWith(':')) r.port = toPort(r, tail.slice(1))
    } else if (hostPort.includes(':')) {
      const parts = hostPort.split(':')
      r.host = parts[0].trim()
      if (parts.length >= 3) {
        // host:port:SID —— 冒号第三段是实例名（SID），不是服务名
        r.port = toPort(r, parts[1])
        r.sid = parts[2].trim()
      } else {
        r.port = toPort(r, parts[1])
      }
    } else {
      r.host = hostPort.trim()
    }
    if (after) {
      // after 既可能是服务名，也可能是 ? 之前的空串
      r.schema = decodeURIComponentSafe(after)
    }
    if (!r.host) {
      r.ok = false
      r.error = '未能从 Oracle JDBC 串中解析出主机，TNS 别名（依赖 tnsnames.ora）请改写为 主机:端口/服务名'
      return r
    }
    if (!hostPort.includes(':') && !after) {
      // 既无端口又无服务名 → 只有一个裸词，在 JDBC 里含义是 tnsnames.ora 别名，驱动不读该文件
      r.ok = false
      r.error = `「${hostPort}」看起来是 TNS 别名（需 tnsnames.ora），请改写为 主机:端口/服务名 或 主机:端口:实例名`
      return r
    }
    r.params = mapOracleParams(query)
  }

  if (!r.port) r.port = DEFAULT_PORTS.oracle
  if (!r.host) {
    r.ok = false
    if (!r.error) r.error = '未能从 Oracle JDBC 串中解析出主机'
    return r
  }

  const qs = buildQueryString(r.params)
  const host = bracketHost(r.host)
  if (r.sid) {
    // SID 连接：驱动侧写法是 ?sid=xxx（服务名留空），与已映射参数合并
    const merged = ['SID=' + encodeURIComponent(r.sid), qs].filter(Boolean).join('&')
    r.dsn = `${host}:${r.port}?${merged}`
  } else {
    const svc = r.schema ? encodeURIComponent(r.schema) : ''
    r.dsn = `${host}:${r.port}/${svc}${qs ? `?${qs}` : ''}`
    if (!svc) {
      r.warnings.push('未解析到服务名，保存前请补全，否则测试连接会因 SID 与服务名同时为空而失败')
    }
  }
  return r
}

/** (DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=h)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=s))) */
function parseTns(r: JdbcParseResult, s: string): void {
  const hosts = [...s.matchAll(/\(\s*HOST\s*=\s*"?([^")]+)"?\s*\)/gi)].map(m => m[1].trim())
  const ports = [...s.matchAll(/\(\s*PORT\s*=\s*"?([^")]+)"?\s*\)/gi)].map(m => m[1].trim())
  const services = [...s.matchAll(/\(\s*SERVICE_NAME\s*=\s*"?([^")]+)"?\s*\)/gi)].map(m => m[1].trim())
  const sids = [...s.matchAll(/\(\s*SID\s*=\s*"?([^")]+)"?\s*\)/gi)].map(m => m[1].trim())

  r.host = hosts[0] || ''
  r.port = ports.length ? toPort(r, ports[0]) : DEFAULT_PORTS.oracle
  if (services.length) r.schema = services[0]
  else if (sids.length) r.sid = sids[0]

  if (hosts.length > 1) {
    r.warnings.push(`DESCRIPTION 含 ${hosts.length} 个地址（failover/scan），已取第一个：${r.host}`)
  }
  const extras = [...s.matchAll(/\(\s*([A-Z_][A-Z0-9_]*)\s*=/gi)]
    .map(m => m[1].toUpperCase())
    .filter(k => !TNS_IGNORED_KEYS.includes(k))
  if (extras.length) {
    r.warnings.push(`TNS 中的 ${[...new Set(extras)].join('、')} 未支持，已忽略`)
  }
  if (!r.host) {
    r.ok = false
    r.error = 'TNS DESCRIPTION 中未找到 HOST'
  }
}

/** JDBC/Oracle 客户端参数 → go-ora URL 选项 */
function mapOracleParams(query: string): JdbcParamEntry[] {
  const entries: JdbcParamEntry[] = []
  for (const [k, v] of splitPairs(query)) {
    const key = k.trim()
    if (!key) continue
    const lower = key.toLowerCase().replace(/[\s_-]/g, '')

    const renamed = ORACLE_RENAMES[lower]
    if (renamed) {
      if (renamed.endsWith('timeout') && renamed !== 'timezoneasregion') {
        const secs = msToSeconds(v)
        if (secs === '') {
          entries.push(entry(key, v, 'dropped', '', '', '超时取值非数字，无法换算为 go-ora 所需的秒'))
        } else {
          entries.push(entry(key, v, 'mapped', renamed, secs, `JDBC 毫秒 → go-ora ${renamed}（秒）`))
        }
        continue
      }
      if (renamed === 'timezoneasregion' || renamed === 'ssl_version') {
        entries.push(entry(key, v, 'dropped', '', '', renamed === 'timezoneasregion'
          ? 'go-ora 无该开关，时区由驱动按会话 NLS 处理'
          : 'TLS 版本由 go-ora 的 ssl/wallet 参数决定'))
        continue
      }
      entries.push(entry(key, v, 'mapped', renamed, v, `已映射为 go-ora 选项 ${renamed}`))
      continue
    }
    const known = ORACLE_KNOWN_PARAMS[lower]
    if (known) {
      // 后端固定追加 charset=AL32UTF8，用户再指定会互相覆盖，故提示
      if (known === 'CHARSET' || known === 'CLIENT CHARSET') {
        entries.push(entry(key, v, 'dropped', '', '', '系统统一按 AL32UTF8 客户端字符集连接，该参数已被覆盖'))
        continue
      }
      if (known === 'SID' || known === 'SERVICE NAME') {
        entries.push(entry(key, v, 'dropped', '', '', '服务名/实例名取自连接串主体，重复指定已忽略'))
        continue
      }
      entries.push(entry(key, v, 'kept', known, v, 'go-ora 支持的连接选项，原样下发'))
      continue
    }
    // go-ora 对未知选项直接报 unknown URL option，必须丢弃
    entries.push(entry(key, v, 'dropped', '', '', `「${key}」不是 go-ora 认识的连接选项，透传会报 unknown URL option，已丢弃`))
  }
  return entries
}

// ===== SQLite =====

function parseSqlite(r: JdbcParseResult, rest: string): JdbcParseResult {
  r.dbType = 'sqlite'
  r.supported = true
  let s = rest.trim()
  if (s.startsWith('//')) s = s.slice(2)
  const { base, query } = splitQuery(s, '?')
  r.schema = base
  r.host = ''
  if (!base) {
    r.ok = false
    r.error = '未能从 SQLite JDBC 串中解析出文件路径'
    return r
  }
  r.params = mapSqliteParams(query)
  const qs = buildQueryString(r.params)
  r.dsn = `${base}${qs ? `?${qs}` : ''}`
  r.warnings.push('SQLite 连接串只需文件路径；WAL/busy_timeout 等由系统统一注入，重复参数会自动跳过')
  return r
}

function mapSqliteParams(query: string): JdbcParamEntry[] {
  const entries: JdbcParamEntry[] = []
  for (const [k, v] of splitPairs(query)) {
    const key = k.trim()
    if (!key) continue
    const lower = key.toLowerCase()
    if (lower === 'date_string_format' || lower === 'datestringformat') {
      const layout = javaDateToGoLayout(v)
      if (layout) {
        entries.push(entry(key, v, 'mapped', '_time_format', layout, `Java 日期格式已换算为 Go layout（${layout}）`))
      } else {
        entries.push(entry(key, v, 'dropped', '', '', '无法换算该 Java 日期模板'))
      }
      continue
    }
    if (SQLITE_PRAGMAS.has(lower)) {
      entries.push(entry(key, v, 'mapped', '_pragma', `${lower}(${v})`, `按 modernc 写法转成 _pragma=${lower}(${v})`))
      continue
    }
    if (SQLITE_KNOWN_PARAMS.has(key)) {
      entries.push(entry(key, v, 'kept', key, v, '驱动同名参数，原样下发'))
      continue
    }
    entries.push(entry(key, v, 'dropped', '', '',
      SQLITE_JDBC_ONLY_PARAMS.has(lower)
        ? 'sqlite-jdbc（xerial）专有参数，modernc 驱动无对应实现'
        : `「${key}」不是 modernc SQLite 驱动认识的参数，已丢弃`))
  }
  return entries
}

/** yyyy-MM-dd HH:mm:ss.SSS → 2006-01-02 15:04:05.000（SSS 要先于 ss 替换，小数点可能已存在） */
function javaDateToGoLayout(v: string): string {
  let s = (v || '').trim()
  if (!s) return ''
  const map: Array<[RegExp, string]> = [
    [/yyyy/g, '2006'], [/yy/g, '06'], [/MM/g, '01'], [/dd/g, '02'],
    [/HH/g, '15'], [/hh/g, '03'], [/\.?SSS/g, '.000'], [/mm/g, '04'], [/ss/g, '05']
  ]
  for (const [re, to] of map) s = s.replace(re, to)
  return /2006/.test(s) ? s : ''
}

// ===== 未支持厂商 =====

function parseUnsupported(r: JdbcParseResult, vendor: string, rest: string): JdbcParseResult {
  r.supported = false
  r.dbType = ''
  const { base, query } = splitQuery(rest.replace(/^\/\/?/, ''), '?')
  const cut = findTopLevel(base, '/')
  const authority = (cut === -1 ? base : base.slice(0, cut)).replace(/^.*@/, '')
  const db = cut === -1 ? '' : base.slice(cut + 1)
  r.schema = decodeURIComponentSafe(db)
  const parts = authority.split(':')
  r.host = parts[0]
  if (parts[1]) r.port = toPort(r, parts[1])
  r.params = splitPairs(query).map(([k, v]) => entry(k, v, 'dropped', '', '', '该数据库类型尚未接入，参数不下发'))
  r.ok = false
  r.error = `JDBC 厂商「${vendor}」暂不支持。当前可选：MySQL/MariaDB、Oracle、SQLite`
  r.warnings.push(`已解析出 主机 ${r.host || '未知'}${r.port ? ':' + r.port : ''}${r.schema ? ' / ' + r.schema : ''}，供你改写为受支持的连接串`)
  return r
}

// ===== 通用小工具 =====

function entry(
  source: string, value: string, action: JdbcParamEntry['action'],
  target: string, targetValue: string, note: string
): JdbcParamEntry {
  return { source, value, action, target, targetValue, note }
}

/** 按 sep 拆出查询串（同时支持 ? 与 ; 作为起始分隔符） */
function splitQuery(s: string, sep: RegExp | string): { base: string; query: string } {
  const re = typeof sep === 'string' ? new RegExp(`[${sep}]`) : sep
  const m = s.match(re)
  if (!m || m.index === undefined) return { base: s, query: '' }
  return { base: s.slice(0, m.index), query: s.slice(m.index + 1) }
}

/** `a=1&b=2` / `a=1;b=2` → 键值对数组（不做二次解码，值可能已带 %2F） */
function splitPairs(query: string): Array<[string, string]> {
  if (!query) return []
  return query
    .split(/[&;]/)
    .filter(Boolean)
    .map((kv) => {
      const idx = kv.indexOf('=')
      if (idx === -1) return [kv, 'true'] as [string, string]
      return [kv.slice(0, idx), kv.slice(idx + 1)] as [string, string]
    })
}

/** 找不在括号内的第一个分隔符，兼容 address=(...) 这类写法 */
function findTopLevel(s: string, ch: string): number {
  let depth = 0
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c === '(') depth++
    else if (c === ')') depth = Math.max(0, depth - 1)
    else if (c === ch && depth === 0) return i
  }
  return -1
}

/** 已下发的参数拼成 query string（键名含空格时按 URL 编码，go-ora 可识别） */
function buildQueryString(entries: JdbcParamEntry[]): string {
  return entries
    .filter(e => e.action !== 'dropped' && e.target)
    .map(e => `${encodeURIComponent(e.target)}=${encodeURIComponent(e.targetValue)}`)
    .join('&')
}

/** IPv6 地址必须带方括号，否则驱动用 net.SplitHostPort 定界时会解错 */
function bracketHost(host: string): string {
  return host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
}

function toPort(r: JdbcParseResult, raw: string): number | null {
  const n = parseInt((raw || '').trim(), 10)
  if (!Number.isFinite(n) || n <= 0 || n > 65535) {
    if ((raw || '').trim()) r.warnings.push(`端口「${raw}」非法，已改用默认端口`)
    return null
  }
  return n
}

function toBool(v: string): string {
  const s = (v || '').trim().toLowerCase()
  return s === '1' || s === 'y' || s === 'yes' || s === 'true' || s === 'on' ? 'true' : 'false'
}

/** 毫秒 → Go 时长写法（5000 → 5s，90000 → 1m30s） */
function msToDuration(v: string): string {
  const ms = parseInt((v || '').trim(), 10)
  if (!Number.isFinite(ms) || ms <= 0) return ''
  if (ms % 1000 !== 0) return `${ms}ms`
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  const rem = sec % 60
  return rem ? `${min}m${rem}s` : `${min}m`
}

/** 毫秒 → 秒（go-ora 超时参数以秒为单位，向上取整避免变成 0） */
function msToSeconds(v: string): string {
  const ms = parseInt((v || '').trim(), 10)
  if (!Number.isFinite(ms) || ms < 0) return ''
  if (ms === 0) return '0'
  return String(Math.max(1, Math.ceil(ms / 1000)))
}

function decodeURIComponentSafe(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}
