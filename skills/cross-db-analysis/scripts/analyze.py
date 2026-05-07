"""
跨数据库大数据量分析脚本。
将聚合计算下沉到数据库端，仅返回紧凑的统计结果 JSON，避免上下文溢出。
"""

import json
import sys
import argparse
from typing import Any


def get_connection(db_type: str, dsn: str):
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
        conn = psycopg2.connect(dsn)
        return conn
    elif db_type == "sqlite":
        import sqlite3
        conn = sqlite3.connect(dsn)
        conn.row_factory = sqlite3.Row
        return conn
    elif db_type == "oracle":
        import oracledb
        conn = oracledb.connect(dsn)
        return conn
    else:
        raise ValueError(f"Unsupported db_type: {db_type}")


def build_metric_sql(metric: str, table: str, schema: str) -> str:
    if ":" in metric:
        func, col = metric.split(":", 1)
        return f"{func.upper()}({col})"
    return f"{metric.upper()}(*)"


def run_aggregate(conn, db_type: str, schema: str, task: dict) -> dict:
    results = {}
    tables = task.get("tables", [])
    metrics = task.get("metrics", ["count"])
    group_by = task.get("groupBy", None)

    for table in tables:
        metric_parts = [build_metric_sql(m, table, schema) for m in metrics]
        metric_aliases = [f"m{i}" for i in range(len(metrics))]
        select_clause = ", ".join(
            f"{mp} AS {ma}" for mp, ma in zip(metric_parts, metric_aliases)
        )

        if group_by:
            select_clause = f"{group_by}, {select_clause}"
            group_clause = f" GROUP BY {group_by}"
            order_clause = f" ORDER BY {group_by}"
        else:
            group_clause = ""
            order_clause = ""

        full_table = f"{schema}.{table}" if schema else table
        sql = f"SELECT {select_clause} FROM {full_table}{group_clause}{order_clause}"
        sql = f"{sql} LIMIT 1000"

        cursor = conn.cursor()
        cursor.execute(sql)
        rows = cursor.fetchall()

        if db_type == "sqlite":
            rows = [dict(r) for r in rows]
        elif hasattr(rows[0], "keys") if rows else False:
            rows = [dict(r) for r in rows]

        cursor.close()

        results[table] = {
            "metrics": metrics,
            "groupBy": group_by,
            "rowCount": len(rows),
            "data": rows,
        }

    return results


def run_compare(sources: list, task: dict) -> dict:
    results = {}
    tables = task.get("tables", [])

    for table in tables:
        table_results = {}
        for src in sources:
            conn = get_connection(src["dbType"], src["dsn"])
            try:
                cursor = conn.cursor()
                full_table = f"{src['schema']}.{table}" if src.get("schema") else table
                cursor.execute(f"SELECT COUNT(*) AS cnt FROM {full_table}")
                row = cursor.fetchone()
                count = row[0] if hasattr(row, "__getitem__") else row["cnt"]
                cursor.close()
                table_results[f"{src['connId']}::{src.get('schema', '')}"] = {
                    "rowCount": count
                }
            finally:
                conn.close()
        results[table] = table_results

    return results


def run_custom(sources: list, task: dict) -> dict:
    queries = task.get("queries", [])
    results = {}

    for i, q in enumerate(queries):
        src = sources[q["sourceIndex"]]
        conn = get_connection(src["dbType"], src["dsn"])
        try:
            cursor = conn.cursor()
            cursor.execute(q["sql"])
            rows = cursor.fetchall()
            if src["dbType"] == "sqlite":
                rows = [dict(r) for r in rows]
            elif rows and hasattr(rows[0], "keys"):
                rows = [dict(r) for r in rows]
            else:
                cols = [d[0] for d in cursor.description] if cursor.description else []
                rows = [dict(zip(cols, r)) for r in rows]
            cursor.close()
            results[f"query_{i}"] = {"sql": q["sql"], "rowCount": len(rows), "data": rows}
        finally:
            conn.close()

    return results


def main():
    parser = argparse.ArgumentParser(description="跨数据库大数据量分析")
    parser.add_argument("--config", required=True, help="JSON: 数据源配置")
    parser.add_argument("--task", required=True, help="JSON: 分析任务定义")
    parser.add_argument("--output", default="-", help="输出文件路径，默认 stdout")
    args = parser.parse_args()

    config = json.loads(args.config)
    task = json.loads(args.task)
    sources = config.get("sources", [])
    task_type = task.get("type", "aggregate")

    result: dict[str, Any] = {"taskType": task_type}

    try:
        if task_type == "aggregate":
            conn_results = {}
            for src in sources:
                conn = get_connection(src["dbType"], src["dsn"])
                try:
                    schema = src.get("schema", "")
                    conn_results[src["connId"]] = run_aggregate(
                        conn, src["dbType"], schema, task
                    )
                finally:
                    conn.close()
            result["results"] = conn_results

        elif task_type == "compare":
            result["results"] = run_compare(sources, task)

        elif task_type == "custom":
            result["results"] = run_custom(sources, task)

        else:
            result["error"] = f"Unknown task type: {task_type}"

    except Exception as e:
        result["error"] = str(e)
        result["success"] = False
    else:
        result["success"] = True

    output = json.dumps(result, ensure_ascii=False, indent=2, default=str)
    if args.output == "-":
        print(output)
    else:
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(output)


if __name__ == "__main__":
    main()
