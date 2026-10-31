"""skill_worker.py — 技能脚本的常驻执行器（由 Go 侧 worker 池拉起）。

背景
----
每次导出都新起一个 Python 进程，需要重新 import matplotlib/numpy/python-docx/
python-pptx（约 0.6s，占单次导出 CPU 的 10%~20%）。本 worker 常驻进程内复用已
导入的模块，只把「脚本执行」按请求重放。

协议（stdin/stdout 每行一个 JSON，UTF-8）
----------------------------------------
启动后先输出一次握手：
    {"ready": true, "pid": 1234, "version": "1"}

请求：
    {"id": 1, "script": "/abs/path/x.py", "argv": ["a.json"], "cwd": "/work",
     "env": {"TEMP": "/tmp/u1"}, "stdinPath": "/tmp/in.json", "timeoutMs": 300000}

响应：
    {"id": 1, "exitCode": 0, "output": "...", "truncated": false, "durationMs": 812}

约束
----
- 脚本只能由 Go 侧（已通过沙箱校验）传入；本文件位于只读 skills/lib，禁止被技能脚本改写。
- 每次执行前替换 sys.argv / sys.stdin / sys.stdout / sys.stderr / cwd / 指定环境变量，
  执行后恢复；脚本以 runpy.run_path(..., run_name="__main__") 运行，等价 `python x.py`。
- 脚本输出被写入有上限的内存缓冲（MAX_OUTPUT），不会污染协议通道。
"""

import io
import json
import os
import runpy
import sys
import tempfile
import time
import traceback

PROTOCOL_VERSION = "1"
MAX_OUTPUT = 256 * 1024


class _BoundedTextIO(io.TextIOBase):
    """有上限的文本缓冲：脚本输出超出部分被丢弃并标记 truncated。"""

    encoding = "utf-8"

    def __init__(self, limit=MAX_OUTPUT):
        self._limit = limit
        self._chunks = []
        self._size = 0
        self.truncated = False

    def writable(self):
        return True

    def write(self, s):
        if not isinstance(s, str):
            s = str(s)
        if self._size >= self._limit:
            self.truncated = True
            return len(s)
        remain = self._limit - self._size
        if len(s) > remain:
            self._chunks.append(s[:remain])
            self._size = self._limit
            self.truncated = True
        else:
            self._chunks.append(s)
            self._size += len(s)
        return len(s)

    def flush(self):
        return None

    def isatty(self):
        return False

    def getvalue(self):
        return "".join(self._chunks)


def _write_response(stream, payload):
    stream.write(json.dumps(payload, ensure_ascii=False) + "\n")
    stream.flush()


def _apply_env(env):
    """设置请求指定的环境变量，返回整个 environ 的快照（执行结束后整体恢复）。"""
    saved = dict(os.environ)
    for key, value in (env or {}).items():
        os.environ[key] = str(value)
    # tempfile 会缓存首次计算的临时目录；不重置会把上一个用户的 TEMP 带给下一个请求。
    tempfile.tempdir = None
    return saved


def _restore_env(saved):
    """恢复完整环境快照，避免脚本设置的变量泄漏到后续请求（跨用户隔离）。"""
    os.environ.clear()
    os.environ.update(saved)
    tempfile.tempdir = None


def _close_plots():
    """回收上一个请求遗留的 matplotlib 图形，避免常驻进程内存持续增长。"""
    plt = sys.modules.get("matplotlib.pyplot")
    if plt is not None:
        try:
            plt.close("all")
        except Exception:
            pass


def _run_one(req, proto_out):
    script = req.get("script") or ""
    argv = [script] + list(req.get("argv") or [])
    cwd = req.get("cwd") or ""
    started = time.time()

    out = _BoundedTextIO()
    err = _BoundedTextIO(MAX_OUTPUT // 2)

    saved_cwd = os.getcwd()
    saved_argv = sys.argv
    saved_stdin, saved_stdout, saved_stderr = sys.stdin, sys.stdout, sys.stderr
    saved_env = _apply_env(req.get("env") or {})
    script_dir = os.path.dirname(os.path.abspath(script))
    path_added = False
    stdin_file = None

    exit_code = 0
    try:
        if cwd:
            os.chdir(cwd)
        if script_dir and script_dir not in sys.path:
            sys.path.insert(0, script_dir)
            path_added = True
        sys.argv = argv
        sys.stdout = out
        sys.stderr = err

        stdin_path = req.get("stdinPath")
        if stdin_path:
            stdin_file = open(stdin_path, "r", encoding="utf-8-sig", errors="replace")
            sys.stdin = stdin_file
        else:
            sys.stdin = io.StringIO("")

        runpy.run_path(script, run_name="__main__")
    except SystemExit as exc:
        code = exc.code
        if code is None:
            exit_code = 0
        elif isinstance(code, int):
            exit_code = code
        else:
            exit_code = 1
    except BaseException:
        exit_code = 1
        err.write(traceback.format_exc())
    finally:
        _close_plots()
        if stdin_file is not None:
            try:
                stdin_file.close()
            except Exception:
                pass
        sys.argv = saved_argv
        sys.stdin, sys.stdout, sys.stderr = saved_stdin, saved_stdout, saved_stderr
        _restore_env(saved_env)
        if path_added:
            try:
                sys.path.remove(script_dir)
            except ValueError:
                pass
        try:
            os.chdir(saved_cwd)
        except OSError:
            pass

    output = out.getvalue()
    err_text = err.getvalue()
    if err_text:
        output = (output + "\n" + err_text).strip() if output else err_text
    truncated = out.truncated or err.truncated
    if truncated:
        output += "\n...(输出过长，已截断)"

    _write_response(
        proto_out,
        {
            "id": req.get("id"),
            "exitCode": exit_code,
            "output": output,
            "truncated": truncated,
            "durationMs": int((time.time() - started) * 1000),
        },
    )


def main():
    # 协议通道使用 UTF-8；保存原始 stdio 供脚本执行期间替换。
    try:
        sys.stdin.reconfigure(encoding="utf-8", errors="replace")
        sys.stdout.reconfigure(encoding="utf-8")
    except (AttributeError, ValueError):
        pass

    proto_in = sys.stdin
    proto_out = sys.stdout
    _write_response(proto_out, {"ready": True, "pid": os.getpid(), "version": PROTOCOL_VERSION})

    for line in proto_in:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except ValueError:
            _write_response(proto_out, {"id": None, "exitCode": -1, "output": "invalid request json"})
            continue
        if req.get("op") == "ping":
            _write_response(proto_out, {"id": req.get("id"), "exitCode": 0, "output": "pong"})
            continue
        _run_one(req, proto_out)


if __name__ == "__main__":
    main()
