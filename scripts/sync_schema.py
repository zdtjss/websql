"""
SQLite 表结构同步脚本
从 migrations/full/sqlite_full.sql 读取表结构定义，同步到 nway.sqlite3.db
- 源库中存在但目标库中不存在的表：创建
- 两边都存在的表：对比列差异，新增缺失列
- 源库中不存在但目标库中存在的表：仅提示，不删除
- INSERT 语句使用 INSERT OR IGNORE 执行，避免重复插入
"""
import sqlite3
import re
import os
import sys
from datetime import datetime

if sys.platform == 'win32':
    import codecs
    sys.stdout = codecs.getwriter('utf-8')(sys.stdout.buffer, 'strict')
    sys.stderr = codecs.getwriter('utf-8')(sys.stderr.buffer, 'strict')

SQL_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'migrations', 'full', 'sqlite_full.sql')
DB_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'nway.sqlite3.db')


def parse_sql_file(sql_file):
    with open(sql_file, 'r', encoding='utf-8') as f:
        content = f.read()

    tables = {}
    inserts = []

    for stmt in re.split(r';\s*\n', content):
        stmt = stmt.strip()
        if not stmt:
            continue

        create_match = re.match(
            r'CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\']?(\w+)["\']?\s*\((.*)\)',
            stmt, re.IGNORECASE | re.DOTALL
        )
        if create_match:
            table_name = create_match.group(1).lower()
            columns_part = create_match.group(2)

            columns = {}
            depth = 0
            current = ""
            parts = []
            for char in columns_part:
                if char == '(':
                    depth += 1
                    current += char
                elif char == ')':
                    depth -= 1
                    current += char
                elif char == ',' and depth == 0:
                    parts.append(current.strip())
                    current = ""
                else:
                    current += char
            if current.strip():
                parts.append(current.strip())

            for part in parts:
                part = part.strip()
                if re.match(r'^(PRIMARY\s+KEY|UNIQUE|FOREIGN\s+KEY|CHECK|CONSTRAINT)', part, re.IGNORECASE):
                    continue
                col_match = re.match(r'["\']?(\w+)["\']?\s+(.+)', part, re.IGNORECASE | re.DOTALL)
                if col_match:
                    col_name = col_match.group(1).lower()
                    col_def = col_match.group(2).strip()
                    columns[col_name] = col_def

            tables[table_name] = {
                'name': table_name,
                'columns': columns,
                'raw_sql': stmt.strip(),
            }
            continue

        insert_match = re.match(r'INSERT\s+(?:OR\s+\w+\s+)?INTO\s+', stmt, re.IGNORECASE)
        if insert_match:
            inserts.append(stmt.strip())

    return tables, inserts


def get_db_tables(db_path):
    conn_str = f"file:{db_path}?mode=ro"
    conn = sqlite3.connect(conn_str, uri=True)
    cursor = conn.cursor()
    cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
    tables = {row[0].lower() for row in cursor.fetchall()}
    conn.close()
    return tables


def get_db_table_columns(db_path, table_name):
    conn = sqlite3.connect(db_path)
    cursor = conn.cursor()
    cursor.execute(f"PRAGMA table_info({table_name})")
    columns = {}
    for row in cursor.fetchall():
        col_name = row[1].lower()
        col_type = row[2].upper() if row[2] else ''
        notnull = 'NOT NULL' if row[3] else ''
        default_val = row[4]
        pk = row[5]

        col_def = col_type
        if pk:
            col_def += ' PRIMARY KEY'
        if notnull and not pk:
            col_def += ' NOT NULL'
        if default_val is not None:
            col_def += f' DEFAULT {default_val}'

        columns[col_name] = col_def
    conn.close()
    return columns


def sync():
    print("=" * 60)
    print("SQLite 表结构同步工具")
    print(f"源: {SQL_FILE}")
    print(f"目标: {DB_FILE}")
    print("=" * 60)
    print()

    if not os.path.exists(SQL_FILE):
        print(f"[ERROR] SQL 文件不存在: {SQL_FILE}")
        return 1

    if not os.path.exists(DB_FILE):
        print(f"[INFO] 目标数据库不存在，将创建: {DB_FILE}")

    source_tables, source_inserts = parse_sql_file(SQL_FILE)
    print(f"SQL 文件中定义了 {len(source_tables)} 个表: {', '.join(sorted(source_tables.keys()))}")
    print(f"SQL 文件中包含 {len(source_inserts)} 条 INSERT 语句")
    print()

    db_tables = get_db_tables(DB_FILE) if os.path.exists(DB_FILE) else set()
    print(f"目标数据库中有 {len(db_tables)} 个表: {', '.join(sorted(db_tables))}")
    print()

    conn = sqlite3.connect(DB_FILE)
    cursor = conn.cursor()

    new_tables = set(source_tables.keys()) - db_tables
    common_tables = set(source_tables.keys()) & db_tables
    extra_tables = db_tables - set(source_tables.keys())

    changes = 0

    if new_tables:
        print(f"[新增表] {len(new_tables)} 个: {', '.join(sorted(new_tables))}")
        for table_name in sorted(new_tables):
            raw_sql = source_tables[table_name]['raw_sql']
            print(f"  创建表 {table_name} ...")
            cursor.execute(raw_sql)
            changes += 1
        print()

    if common_tables:
        print(f"[对比差异] {len(common_tables)} 个共同表")
        for table_name in sorted(common_tables):
            source_cols = source_tables[table_name]['columns']
            target_cols = get_db_table_columns(DB_FILE, table_name)

            added_cols = set(source_cols.keys()) - set(target_cols.keys())
            if added_cols:
                print(f"  表 {table_name} 需要新增列: {', '.join(sorted(added_cols))}")
                for col_name in sorted(added_cols):
                    col_def = source_cols[col_name]
                    sql = f"ALTER TABLE {table_name} ADD COLUMN {col_name} {col_def}"
                    print(f"    执行: {sql}")
                    cursor.execute(sql)
                    changes += 1
            else:
                print(f"  表 {table_name} 结构一致，无需变更")
        print()

    if extra_tables:
        print(f"[仅目标库存在] {len(extra_tables)} 个表: {', '.join(sorted(extra_tables))}")
        print("  (不会自动删除，如需删除请手动操作)")
        print()

    if source_inserts:
        print(f"[初始化数据] 执行 {len(source_inserts)} 条 INSERT 语句")
        for insert_sql in source_inserts:
            try:
                cursor.execute(insert_sql)
                if cursor.rowcount > 0:
                    print(f"  插入成功: {insert_sql[:80]}...")
                    changes += 1
            except sqlite3.IntegrityError:
                pass
            except sqlite3.Error as e:
                print(f"  [WARN] {e}: {insert_sql[:80]}...")
        print()

    if changes > 0:
        conn.commit()
        print(f"[DONE] 同步完成，共 {changes} 项变更已提交")
    else:
        print("[DONE] 表结构完全一致，无需同步")

    conn.close()
    return 0


if __name__ == '__main__':
    exit(sync())
