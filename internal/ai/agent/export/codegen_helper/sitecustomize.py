"""WebSQL 高级导出沙箱：运行期导入钩子。

解释器启动时（``site`` 模块加载 ``sitecustomize``）安装一个 ``meta_path`` finder，
拒绝大模型临时代码导入网络 / 进程 / 原生调用 / 序列化 / 数据库裸驱动等模块，
防止临时代码外泄数据或绕过只读约束。

放行规则：受信只读助手 ``websql_db`` 在导入数据库驱动期间，会临时把
``builtins.__websql_import_state__['allow']`` 置为 True，使驱动内部的
``import socket`` 等传递依赖得以加载；模型代码自身无法导入这些模块。
"""

import builtins
import sys

_STATE_KEY = "__websql_import_state__"

# 运行期硬拦截的顶层模块
_BLOCKED = {
    # 网络
    "socket", "ssl", "http", "urllib", "urllib2", "urllib3", "requests", "httpx",
    "aiohttp", "ftplib", "smtplib", "poplib", "imaplib", "telnetlib", "xmlrpc",
    "websocket",
    # 进程 / 原生调用
    "subprocess", "multiprocessing", "ctypes", "_ctypes", "cffi", "pty",
    # 序列化 / 动态加载
    "pickle", "marshal", "shelve",
    # 数据库裸驱动（必须经 websql_db 只读助手）
    "pymysql", "MySQLdb", "mysql", "sqlite3", "oracledb", "cx_Oracle", "psycopg2",
}

if not hasattr(builtins, _STATE_KEY):
    setattr(builtins, _STATE_KEY, {"allow": False})


def _allowed():
    state = getattr(builtins, _STATE_KEY, None)
    return bool(state and state.get("allow"))


class _DenyFinder:
    """拒绝导入被禁模块的 meta_path finder。"""

    def find_spec(self, fullname, path=None, target=None):
        root = fullname.split(".")[0]
        if root in _BLOCKED and not _allowed():
            raise ImportError(
                "模块 %s 在 WebSQL 高级导出沙箱中被禁止导入"
                "（临时代码仅可通过 websql_db 只读助手访问数据库）" % fullname
            )
        return None


def _install():
    for finder in sys.meta_path:
        if isinstance(finder, _DenyFinder):
            break
    else:
        sys.meta_path.insert(0, _DenyFinder())

    # 包装 builtins.__import__：meta_path finder 无法拦截「已缓存在 sys.modules 中的模块」
    # （受信助手导入驱动后会缓存 socket/sqlite3 等），故在 __import__ 层再拦一次。
    if not getattr(builtins, "__websql_import_guarded__", False):
        original_import = builtins.__import__

        def _guarded_import(name, globals=None, locals=None, fromlist=(), level=0):
            root = name.split(".")[0]
            if root in _BLOCKED and not _allowed():
                raise ImportError(
                    "模块 %s 在 WebSQL 高级导出沙箱中被禁止导入"
                    "（临时代码仅可通过 websql_db 只读助手访问数据库）" % name
                )
            return original_import(name, globals, locals, fromlist, level)

        builtins.__import__ = _guarded_import
        builtins.__websql_import_guarded__ = True


_install()