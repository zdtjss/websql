"""
跨数据库大数据量分析脚本。
将聚合计算下沉到数据库端，仅返回紧凑的统计结果 JSON，避免上下文溢出。
"""

import json
import sys
import argparse
import re
import time
import threading
from contextlib import contextmanager
from typing import Any, Optional
from collections import defaultdict


MAX_RESULT_ROWS = 5000
MAX_RESULT_BYTES = 512 * 1024
DEFAULT_QUERY_TIMEOUT = 120

_IDENTIFIER_PATTERN = re.compile(r'^[a-zA-Z_][a-zA-Z0-9_]*$')


def validate_identifier(name: str, kind: str = "identifier") -> str:
    if not _IDENTIFIER_PATTERN.match(name):
        raise ValueError(f"Invalid {kind}: {name}")
    return name


def qualified_table(schema: Optional[str], table: str) -> str:
    validate_identifier(table, "table")
    if schema:
        validate_identifier(schema, "schema")
        return f'"{schema}"."{table}"'
    return f'"{table}"'


def quote_column(col: str) -> str:
    if col == "*":
        return col
    validate_identifier(col, "column")
    return f'"{col}"'


# ── Connection Pool ──────────────────────────────────────────────────────────

_connection_pools: dict[str, list] = {}
_pool_locks: dict[str, threading.Lock] = defaultdict(threading.Lock)
_POOL_MAX = 5


def _pool_key(src: dict) -> str:
    return f"{src['dbType']}::{src.get('dsn', '')}"


def _create_connection(src: dict):
    db_type = src["dbType"]
    dsn = src.get("dsn", "")

    if db_type in ("mysql", "mariadb"):
        import pymysql
        parts = dsn.split("@")
        user_pass = parts[0].split(":")
        host_db = parts[1].split("/")
        host_port = host_db[0].split(":")
        return pymysql.connect(
            host=host_port[0],
            port=int(host_port[1]) if len(host_port) > 1 else 3306,
            user=user_pass[0],
            password=user_pass[1] if len(user_pass) > 1 else "",
            database=host_db[1],
            charset="utf8mb4",
            cursorclass=pymysql.cursors.DictCursor,
        )
    elif db_type in ("postgresql", "postgres"):
        import psycopg2
        import psycopg2.extras
        return psycopg2.connect(dsn)
    elif db_type == "sqlite":
        import sqlite3
        conn = sqlite3.connect(dsn)
        conn.row_factory = sqlite3.Row
        return conn
    elif db_type == "oracle":
        import oracledb
        return oracledb.connect(dsn)
    else:
        raise ValueError(f"Unsupported db_type: {db_type}")


@contextmanager
def connection(src: dict):
    key = _pool_key(src)
    lock = _pool_locks[key]

    conn = None
    with lock:
        pool = _connection_pools.get(key, [])
        if pool:
            conn = pool.pop()
            _connection_pools[key] = pool
            try:
                if hasattr(conn, "ping"):
                    conn.ping(reconnect=True)
            except Exception:
                try:
                    conn.close()
                except Exception:
                    pass
                conn = None

    if conn is None:
        conn = _create_connection(src)

    ok = False
    try:
        yield conn
        ok = True
    finally:
        if ok:
            with lock:
                pool = _connection_pools.get(key, [])
                if len(pool) < _POOL_MAX:
                    pool.append(conn)
                    _connection_pools[key] = pool
                else:
                    try:
                        conn.close()
                    except Exception:
                        pass
        else:
            try:
                conn.close()
            except Exception:
                pass


def cleanup_pools():
    for pool in _connection_pools.values():
        for conn in pool:
            try:
                conn.close()
            except Exception:
                pass
    _connection_pools.clear()


# ── Query Execution ──────────────────────────────────────────────────────────

def _set_timeout(conn, db_type: str, timeout_seconds: int):
    cursor = conn.cursor()
    try:
        if db_type in ("postgresql", "postgres"):
            cursor.execute(f"SET statement_timeout = '{timeout_seconds}s'")
        elif db_type in ("mysql", "mariadb"):
            cursor.execute(f"SET SESSION max_execution_time = {timeout_seconds * 1000}")
    except Exception:
        pass
    finally:
        try:
            cursor.close()
        except Exception:
            pass


def execute_query(conn, sql: str, db_type: str = "", timeout: int = DEFAULT_QUERY_TIMEOUT) -> list[dict]:
    if timeout > 0:
        _set_timeout(conn, db_type, timeout)

    cursor = conn.cursor()
    try:
        cursor.execute(sql)
        rows = cursor.fetchall()
        return normalize_rows(rows, cursor, db_type)
    finally:
        try:
            cursor.close()
        except Exception:
            pass


def execute_scalar(conn, sql: str, db_type: str = "", timeout: int = DEFAULT_QUERY_TIMEOUT):
    rows = execute_query(conn, sql, db_type, timeout)
    if not rows:
        return None
    return list(rows[0].values())[0]


def normalize_rows(rows, cursor, db_type: str) -> list[dict]:
    if not rows:
        return []
    if db_type == "sqlite":
        return [dict(r) for r in rows]
    if hasattr(rows[0], "keys"):
        return [dict(r) for r in rows]
    if cursor and cursor.description:
        cols = [d[0] for d in cursor.description]
        return [dict(zip(cols, r)) for r in rows]
    return rows


# ── Result Size Control ──────────────────────────────────────────────────────

def truncate_result(
    rows: list[dict],
    max_rows: int = MAX_RESULT_ROWS,
    max_bytes: int = MAX_RESULT_BYTES,
) -> tuple[list[dict], bool]:
    truncated = False
    if len(rows) > max_rows:
        rows = rows[:max_rows]
        truncated = True

    encoded = json.dumps(rows, ensure_ascii=False, default=str).encode("utf-8")
    if len(encoded) > max_bytes:
        while len(rows) > 1:
            rows = rows[: len(rows) // 2]
            encoded = json.dumps(rows, ensure_ascii=False, default=str).encode("utf-8")
            if len(encoded) <= max_bytes:
                break
        truncated = True

    return rows, truncated


# ── Metric SQL Builder ───────────────────────────────────────────────────────

def build_metric_sql(metric: str) -> tuple[str, str]:
    if ":" in metric:
        func, col = metric.split(":", 1)
        return f"{func.upper()}({quote_column(col)})", f"{func.lower()}_{col}"
    return f"{metric.upper()}(*)", f"{metric.lower()}_star"


# ── Task: aggregate ──────────────────────────────────────────────────────────

def run_aggregate(sources: list, task: dict, timeout: int) -> dict:
    results = {}
    tables = task.get("tables", [])
    metrics = task.get("metrics", ["count"])
    group_by = task.get("groupBy")
    limit = task.get("limit", MAX_RESULT_ROWS)

    metric_parts = []
    metric_aliases = []
    for m in metrics:
        sql_part, alias = build_metric_sql(m)
        metric_parts.append(sql_part)
        metric_aliases.append(alias)

    for src in sources:
        conn_results = {}
        with connection(src) as conn:
            for table in tables:
                select_parts = [
                    f"{mp} AS {ma}" for mp, ma in zip(metric_parts, metric_aliases)
                ]
                select_clause = ", ".join(select_parts)

                if group_by:
                    validate_identifier(group_by, "groupBy column")
                    select_clause = f'"{group_by}", {select_clause}'
                    group_clause = f' GROUP BY "{group_by}"'
                    order_clause = f' ORDER BY "{group_by}"'
                else:
                    group_clause = ""
                    order_clause = ""

                tbl = qualified_table(src.get("schema"), table)
                limit_clause = f" LIMIT {limit}" if limit > 0 else ""
                sql = (
                    f"SELECT {select_clause} FROM {tbl}"
                    f"{group_clause}{order_clause}{limit_clause}"
                )

                rows = execute_query(conn, sql, src["dbType"], timeout)
                rows, truncated = truncate_result(rows)

                conn_results[table] = {
                    "metrics": metrics,
                    "groupBy": group_by,
                    "rowCount": len(rows),
                    "truncated": truncated,
                    "data": rows,
                }

        results[src["connId"]] = conn_results

    return results


# ── Task: compare ────────────────────────────────────────────────────────────

def run_compare(sources: list, task: dict, timeout: int) -> dict:
    results = {}
    tables = task.get("tables", [])
    metrics = task.get("metrics", ["count"])

    metric_parts = []
    metric_aliases = []
    for m in metrics:
        sql_part, alias = build_metric_sql(m)
        metric_parts.append(sql_part)
        metric_aliases.append(alias)

    for table in tables:
        source_data = {}
        for src in sources:
            with connection(src) as conn:
                select_parts = [
                    f"{mp} AS {ma}" for mp, ma in zip(metric_parts, metric_aliases)
                ]
                select_clause = ", ".join(select_parts)
                tbl = qualified_table(src.get("schema"), table)
                sql = f"SELECT {select_clause} FROM {tbl}"
                rows = execute_query(conn, sql, src["dbType"], timeout)
                if rows:
                    source_data[src["connId"]] = rows[0]
                else:
                    source_data[src["connId"]] = {ma: None for ma in metric_aliases}

        diff = None
        source_ids = list(source_data.keys())
        if len(source_ids) >= 2:
            diff = compute_diff(
                source_data[source_ids[0]], source_data[source_ids[1]], metrics, metric_aliases
            )

        results[table] = {
            "sources": source_data,
            "diff": diff,
        }

    return results


def compute_diff(base: dict, target: dict, metrics: list, aliases: list) -> dict:
    diff = {}
    for m, alias in zip(metrics, aliases):
        base_val = base.get(alias)
        target_val = target.get(alias)
        if base_val is None or target_val is None:
            diff[m] = {
                "base": base_val,
                "target": target_val,
                "change": None,
                "pctChange": None,
            }
            continue
        try:
            base_num = float(base_val)
            target_num = float(target_val)
        except (ValueError, TypeError):
            diff[m] = {
                "base": base_val,
                "target": target_val,
                "change": None,
                "pctChange": None,
            }
            continue
        change = target_num - base_num
        if base_num != 0:
            pct = round(change / abs(base_num) * 100, 2)
        else:
            pct = float("inf") if target_num != 0 else 0.0
        diff[m] = {
            "base": base_num,
            "target": target_num,
            "change": round(change, 4),
            "pctChange": pct,
        }
    return diff


# ── Task: join ───────────────────────────────────────────────────────────────

def run_join(sources: list, task: dict, timeout: int) -> dict:
    join_spec = task.get("join", {})
    left_key = join_spec.get("leftKey", "id")
    right_key = join_spec.get("rightKey", "id")
    join_type = join_spec.get("joinType", "inner")
    select_columns = join_spec.get("select", [])
    limit = join_spec.get("limit", MAX_RESULT_ROWS)

    left_src = _find_source(sources, join_spec.get("leftSource"))
    right_src = _find_source(sources, join_spec.get("rightSource"))
    left_table = join_spec.get("leftTable", "")
    right_table = join_spec.get("rightTable", "")

    if not left_src or not right_src:
        raise ValueError("join task requires valid leftSource and rightSource connId")

    if left_src["connId"] == right_src["connId"]:
        return _run_same_conn_join(
            left_src, left_table, right_table,
            left_key, right_key, join_type, select_columns, limit, timeout,
        )
    else:
        return _run_cross_conn_join(
            left_src, right_src, left_table, right_table,
            left_key, right_key, join_type, select_columns, limit, timeout,
        )


def _find_source(sources: list, conn_id: str) -> Optional[dict]:
    for src in sources:
        if src["connId"] == conn_id:
            return src
    return None


def _run_same_conn_join(
    src: dict, left_table: str, right_table: str,
    left_key: str, right_key: str, join_type: str,
    select_columns: list, limit: int, timeout: int,
) -> dict:
    with connection(src) as conn:
        ltbl = qualified_table(src.get("schema"), left_table)
        rtbl = qualified_table(src.get("schema"), right_table)

        if select_columns:
            col_list = ", ".join(quote_column(c) for c in select_columns)
        else:
            col_list = '"l".*, "r".*'

        join_map = {
            "inner": "JOIN",
            "left": "LEFT JOIN",
            "right": "RIGHT JOIN",
            "full": "FULL OUTER JOIN",
        }
        join_keyword = join_map.get(join_type, "JOIN")

        sql = (
            f'SELECT {col_list} FROM {ltbl} AS "l"'
            f' {join_keyword} {rtbl} AS "r"'
            f' ON "l"."{left_key}" = "r"."{right_key}"'
        )
        if limit > 0:
            sql += f" LIMIT {limit}"

        rows = execute_query(conn, sql, src["dbType"], timeout)
        rows, truncated = truncate_result(rows)

    return {
        "joinType": f"same-conn-{join_type}-join",
        "leftTable": left_table,
        "rightTable": right_table,
        "matchedRows": len(rows),
        "truncated": truncated,
        "data": rows,
    }


def _run_cross_conn_join(
    left_src: dict, right_src: dict,
    left_table: str, right_table: str,
    left_key: str, right_key: str, join_type: str,
    select_columns: list, limit: int, timeout: int,
) -> dict:
    fetch_limit = limit * 2 if limit > 0 else MAX_RESULT_ROWS * 2

    with connection(left_src) as lconn:
        ltbl = qualified_table(left_src.get("schema"), left_table)
        left_rows = execute_query(
            lconn, f"SELECT * FROM {ltbl} LIMIT {fetch_limit}", left_src["dbType"], timeout,
        )

    with connection(right_src) as rconn:
        rtbl = qualified_table(right_src.get("schema"), right_table)
        right_rows = execute_query(
            rconn, f"SELECT * FROM {rtbl} LIMIT {fetch_limit}", right_src["dbType"], timeout,
        )

    right_index: dict[Any, list[dict]] = defaultdict(list)
    for r in right_rows:
        key_val = r.get(right_key)
        if key_val is not None:
            right_index[key_val].append(r)

    matched = []
    left_only = []
    for l_row in left_rows:
        key_val = l_row.get(left_key)
        r_matches = right_index.get(key_val, [])
        if r_matches:
            for r_row in r_matches:
                merged = {}
                for k, v in l_row.items():
                    merged[f"left_{k}"] = v
                for k, v in r_row.items():
                    merged[f"right_{k}"] = v
                if select_columns:
                    merged = {k: merged.get(k) for k in select_columns if k in merged}
                matched.append(merged)
        elif join_type in ("left", "full"):
            merged = {}
            for k, v in l_row.items():
                merged[f"left_{k}"] = v
            if select_columns:
                merged = {k: merged.get(k) for k in select_columns if k in merged}
            left_only.append(merged)

    result_rows = matched + left_only
    result_rows, truncated = truncate_result(result_rows)

    return {
        "joinType": f"cross-conn-hash-{join_type}-join",
        "leftTable": left_table,
        "rightTable": right_table,
        "leftRowsFetched": len(left_rows),
        "rightRowsFetched": len(right_rows),
        "matchedRows": len(matched),
        "truncated": truncated,
        "data": result_rows,
    }


# ── Task: custom ─────────────────────────────────────────────────────────────

def run_custom(sources: list, task: dict, timeout: int) -> dict:
    queries = task.get("queries", [])
    results = {}

    for i, q in enumerate(queries):
        src_idx = q.get("sourceIndex", 0)
        src = sources[src_idx]
        with connection(src) as conn:
            rows = execute_query(conn, q["sql"], src["dbType"], timeout)
            rows, truncated = truncate_result(rows)
            results[f"query_{i}"] = {
                "sql": q["sql"],
                "rowCount": len(rows),
                "truncated": truncated,
                "data": rows,
            }

    return results


# ── Input Loading ────────────────────────────────────────────────────────────

def load_input() -> tuple[dict, dict, str]:
    output_path = "-"

    if not sys.stdin.isatty():
        data = json.load(sys.stdin)
        config = data.get("config", {})
        task = data.get("task", {})
        return config, task, output_path

    parser = argparse.ArgumentParser(description="跨数据库大数据量分析")
    parser.add_argument("--config", required=True, help="JSON: 数据源配置")
    parser.add_argument("--task", required=True, help="JSON: 分析任务定义")
    parser.add_argument("--output", default="-", help="输出文件路径，默认 stdout")
    args = parser.parse_args()

    config = json.loads(args.config)
    task = json.loads(args.task)
    output_path = args.output
    return config, task, output_path


# ── Main ─────────────────────────────────────────────────────────────────────

def main():
    config, task, output_path = load_input()
    sources = config.get("sources", [])
    task_type = task.get("type", "aggregate")
    timeout = task.get("timeout", DEFAULT_QUERY_TIMEOUT)

    result: dict[str, Any] = {"taskType": task_type}
    start_time = time.time()

    try:
        if task_type == "aggregate":
            result["results"] = run_aggregate(sources, task, timeout)

        elif task_type == "compare":
            result["results"] = run_compare(sources, task, timeout)

        elif task_type == "join":
            result["results"] = run_join(sources, task, timeout)

        elif task_type == "custom":
            result["results"] = run_custom(sources, task, timeout)

        else:
            result["error"] = f"Unknown task type: {task_type}"
            result["success"] = False
            _emit_output(result, output_path)
            return

    except Exception as e:
        result["error"] = str(e)
        result["success"] = False
    else:
        result["success"] = True

    result["executionTimeMs"] = round((time.time() - start_time) * 1000)
    _emit_output(result, output_path)
    cleanup_pools()


def _emit_output(result: dict, output_path: str):
    output = json.dumps(result, ensure_ascii=False, indent=2, default=str)
    if output_path == "-":
        print(output)
    else:
        with open(output_path, "w", encoding="utf-8") as f:
            f.write(output)


if __name__ == "__main__":
    main()
