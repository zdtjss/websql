"""WebSQL 高级导出：只读数据库助手。

大模型在高级导出中编写的临时代码，只能通过本模块访问数据库。连接为只读：
- MySQL / MariaDB：``SET SESSION TRANSACTION READ ONLY``；
- SQLite：以 ``mode=ro`` 打开并置 ``PRAGMA query_only=ON``；
- Oracle：每个查询置于只读事务（``SET TRANSACTION READ ONLY``）。
此外每条语句在发送前都会做只读校验，仅允许 SELECT / SHOW / DESCRIBE / EXPLAIN / WITH(只读)。

连接信息从环境变量读取（``WEBSQL_DB_*``），不会出现在模型上下文中。

用法：
    import websql_db

    conn = websql_db.connect()
    rows = conn.query("SELECT id, name FROM t_user LIMIT 10").dicts()
    conn.close()

    # 或一次性查询
    rows = websql_db.query("SELECT COUNT(*) AS c FROM t_order")
"""

import builtins
import os
import re

_STATE_KEY = "__websql_import_state__"

_DB_TYPE = os.environ.get("WEBSQL_DB_TYPE", "").strip().lower()
_DB_URL = os.environ.get("WEBSQL_DB_URL", "").strip()
_DB_HOST = os.environ.get("WEBSQL_DB_HOST", "").strip()
_DB_PORT = os.environ.get("WEBSQL_DB_PORT", "").strip()
_DB_NAME = os.environ.get("WEBSQL_DB_NAME", "").strip()
_DB_USER = os.environ.get("WEBSQL_DB_USER", "")
_DB_PASSWORD = os.environ.get("WEBSQL_DB_PASSWORD", "")
_DB_SCHEMA = os.environ.get("WEBSQL_DB_SCHEMA", "").strip()

_READ_ONLY_PREFIX = ("select", "with", "show", "describe", "desc", "explain")
_WRITE_WORDS = (
    "insert", "update", "delete", "merge", "replace", "upsert", "alter", "drop",
    "create", "truncate", "grant", "revoke", "call", "exec", "execute", "commit",
    "rollback", "lock", "rename", "vacuum", "attach", "detach", "reindex", "pragma",
)


class ReadOnlyViolation(RuntimeError):
    """违反只读约束（尝试写数据库或执行非查询语句）。"""


def _allow_import():
    """在导入数据库驱动期间临时放行沙箱导入钩子（驱动内部会 import socket 等）。"""
    state = getattr(builtins, _STATE_KEY, None)
    if state is not None:
        state["allow"] = True
    return state


def _restore_import(state):
    if state is not None:
        state["allow"] = False


def _strip_comments(sql):
    sql = re.sub(r"/\*.*?\*/", " ", sql, flags=re.S)
    sql = re.sub(r"--[^\n]*", " ", sql)
    sql = re.sub(r"#[^\n]*", " ", sql)
    return sql


def assert_read_only(sql):
    """校验语句为只读查询，否则抛出 ReadOnlyViolation。"""
    if not isinstance(sql, str) or not sql.strip():
        raise ReadOnlyViolation("SQL 不能为空")
    body = _strip_comments(sql).strip()
    first = re.split(r"\s+", body, maxsplit=1)[0].lower().strip("();,")
    if first not in _READ_ONLY_PREFIX:
        raise ReadOnlyViolation(
            "高级导出仅允许查询语句（SELECT/SHOW/DESCRIBE/EXPLAIN/WITH），收到：%s" % first
        )
    if first == "with":
        words = set(re.findall(r"[A-Za-z_]+", body.lower()))
        for word in _WRITE_WORDS:
            if word in words:
                raise ReadOnlyViolation("WITH 查询中不允许包含写操作：%s" % word)
    return body


def _parse_mysql_url(url):
    """解析连接信息：优先使用分项环境变量，其次兼容 Go 侧连接串。

    支持 ``tcp(host:port)/dbname``、``host:port/dbname`` 与
    ``mysql://user:pass@host:port/dbname``（凭据由 user/password 分项提供）。
    """
    if _DB_HOST:
        try:
            port = int(_DB_PORT) if _DB_PORT else 3306
        except ValueError:
            port = 3306
        return _DB_HOST, port, (_DB_NAME or _DB_SCHEMA)
    match = re.match(r"^(?:tcp\()?([^)/:]+):(\d+)\)?/(.*)$", url)
    if match:
        return match.group(1), int(match.group(2)), (match.group(3) or _DB_SCHEMA)
    return "", 3306, (_DB_NAME or _DB_SCHEMA)


class Result:
    """查询结果：``columns`` 为列名，``rows`` 为按列顺序的原始行。"""

    def __init__(self, columns, rows):
        self.columns = columns
        self.rows = rows

    def dicts(self):
        return [dict(zip(self.columns, row)) for row in self.rows]

    def __iter__(self):
        return iter(self.dicts())

    def __len__(self):
        return len(self.rows)


class ReadOnlyConnection:
    """只读数据库连接。仅暴露 ``query`` / ``close``。"""

    def __init__(self, raw, kind):
        self._raw = raw
        self._kind = kind

    def query(self, sql, params=None):
        assert_read_only(sql)
        if self._kind == "oracle":
            cur = self._raw.cursor()
            try:
                try:
                    cur.execute("SET TRANSACTION READ ONLY")
                except Exception:
                    pass
                cur.execute(sql, params or {})
                cols = [d[0] for d in cur.description] if cur.description else []
                rows = cur.fetchall() if cols else []
            finally:
                cur.close()
            self._raw.rollback()
            return Result(cols, list(rows))

        if self._kind == "mysql":
            cur = self._raw.cursor()
            try:
                cur.execute(sql, params or ())
                cols = [d[0] for d in cur.description] if cur.description else []
                rows = cur.fetchall() if cols else []
            finally:
                cur.close()
            return Result(cols, list(rows))

        # sqlite
        cur = self._raw.execute(sql, params or ())
        cols = [d[0] for d in cur.description] if cur.description else []
        rows = cur.fetchall() if cols else []
        return Result(cols, list(rows))

    def close(self):
        try:
            self._raw.close()
        except Exception:
            pass

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()
        return False


def connect():
    """建立只读连接。数据库类型取自 ``WEBSQL_DB_TYPE``。"""
    if _DB_TYPE in ("sqlite", "sqlite3"):
        state = _allow_import()
        try:
            import sqlite3
        finally:
            _restore_import(state)
        path = (_DB_URL or _DB_NAME).split("?", 1)[0]
        uri = "file:%s?mode=ro" % path.replace("\\", "/")
        raw = sqlite3.connect(uri, uri=True)
        raw.execute("PRAGMA query_only=ON")
        return ReadOnlyConnection(raw, "sqlite")

    if _DB_TYPE in ("mysql", "mariadb"):
        state = _allow_import()
        try:
            import pymysql
        finally:
            _restore_import(state)
        host, port, database = _parse_mysql_url(_DB_URL)
        raw = pymysql.connect(
            host=host, port=port, user=_DB_USER, password=_DB_PASSWORD,
            database=database or None, charset="utf8mb4", autocommit=True,
        )
        try:
            cur = raw.cursor()
            cur.execute("SET SESSION TRANSACTION READ ONLY")
            cur.close()
        except Exception:
            pass
        return ReadOnlyConnection(raw, "mysql")

    if _DB_TYPE == "oracle":
        state = _allow_import()
        try:
            import oracledb
        finally:
            _restore_import(state)
        dsn = _DB_URL
        if not dsn and _DB_HOST:
            if _DB_PORT:
                dsn = "%s:%s/%s" % (_DB_HOST, _DB_PORT, _DB_NAME or _DB_SCHEMA)
            else:
                dsn = "%s/%s" % (_DB_HOST, _DB_NAME or _DB_SCHEMA)
        raw = oracledb.connect(user=_DB_USER, password=_DB_PASSWORD, dsn=dsn)
        return ReadOnlyConnection(raw, "oracle")

    raise ReadOnlyViolation("不支持或缺少数据库类型信息（WEBSQL_DB_TYPE=%r）" % _DB_TYPE)


def query(sql, params=None):
    """一次性只读查询，返回 ``dict`` 列表（自动连接与关闭）。"""
    conn = connect()
    try:
        return conn.query(sql, params).dicts()
    finally:
        conn.close()