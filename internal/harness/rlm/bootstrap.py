"""
RLM Python REPL bootstrap.
Embedded in Go binary via //go:embed. Started as `python3 -u` subprocess.
Enters a JSON-line message loop on stdin/stdout. Executes code blocks in a
persistent namespace. Callbacks to Go for sub_lm, read_file, grep_file, list_files.
"""
import json
import signal
import sys
import re
import time
import traceback

_namespace = {}
_stdout_capture = []
_done = False
_final_answer = None
_last_output = ""
_exec_count = 0

_EXEC_TIMEOUT = 30
_has_alarm = hasattr(signal, 'alarm')

if _has_alarm:
    def _timeout_handler(signum, frame):
        raise TimeoutError(f"Code execution timed out ({_EXEC_TIMEOUT}s limit)")
    signal.signal(signal.SIGALRM, _timeout_handler)


def _log(event, **kv):
    _send({"type": "debug", "event": event, **{k: v for k, v in kv.items() if v is not None}})


def _send(msg):
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()


def _recv():
    remaining = signal.alarm(0) if _has_alarm else 0
    try:
        line = sys.stdin.readline()
        if not line:
            sys.exit(0)
        return json.loads(line.strip())
    finally:
        if remaining > 0:
            signal.alarm(remaining)


def _print(*args):
    _stdout_capture.append(" ".join(str(a) for a in args) + "\n")


def _sub_lm(prompt, max_tokens=4096):
    _log("callback_start", func="sub_lm", prompt_len=len(str(prompt)), max_tokens=max_tokens)
    t0 = time.monotonic()
    _send({"type": "callback", "func": "sub_lm",
           "args": {"prompt": str(prompt), "max_tokens": int(max_tokens)}})
    resp = _recv()
    _log("callback_done", func="sub_lm", elapsed_ms=round((time.monotonic() - t0) * 1000), error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _llm_batch(prompts, max_tokens=4096):
    """Dispatch multiple sub-LLM queries in parallel. Returns list of response strings."""
    prompt_list = [str(p) for p in prompts]
    _log("callback_start", func="sub_lm_batch", count=len(prompt_list), max_tokens=max_tokens)
    t0 = time.monotonic()
    _send({"type": "callback", "func": "sub_lm_batch",
           "args": {"prompts": prompt_list, "max_tokens": int(max_tokens)}})
    resp = _recv()
    _log("callback_done", func="sub_lm_batch",
         elapsed_ms=round((time.monotonic() - t0) * 1000),
         error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return json.loads(resp["result"])


def _read_file(path, start_line=None, end_line=None):
    _log("callback_start", func="read_file", path=str(path), start_line=start_line, end_line=end_line)
    args = {"path": str(path)}
    if start_line is not None:
        args["start_line"] = int(start_line)
    if end_line is not None:
        args["end_line"] = int(end_line)
    _send({"type": "callback", "func": "read_file", "args": args})
    resp = _recv()
    _log("callback_done", func="read_file", error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _grep_file(pattern, path=None):
    _log("callback_start", func="grep_file", pattern=str(pattern), path=path)
    _send({"type": "callback", "func": "grep_file", "args": {"pattern": str(pattern), **({"path": str(path)} if path else {})}})
    resp = _recv()
    _log("callback_done", func="grep_file", error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _list_files(pattern):
    _log("callback_start", func="list_files", pattern=str(pattern))
    _send({"type": "callback", "func": "list_files",
           "args": {"pattern": str(pattern)}})
    resp = _recv()
    _log("callback_done", func="list_files", error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _rlm_query(context, query):
    """Spawn a child RLM to recursively process context with a query."""
    _log("callback_start", func="rlm_query", context_len=len(str(context)), query_len=len(str(query)))
    t0 = time.monotonic()
    _send({"type": "callback", "func": "rlm_query",
           "args": {"prompt": f"Context:\n{str(context)}\n\nQuery: {str(query)}"}})
    resp = _recv()
    _log("callback_done", func="rlm_query", elapsed_ms=round((time.monotonic() - t0) * 1000), error=resp.get("error"), result_len=len(resp.get("result", "")))
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _page_output(offset=0, limit=2000):
    text = _last_output
    total = len(text)
    if offset < 0:
        offset = 0
    if offset >= total:
        return f"[page_output: offset {offset} beyond end of output ({total} chars)]"
    end = min(offset + limit, total)
    header = f"[page_output: chars {offset}-{end - 1} of {total}]\n"
    return header + text[offset:end]


def _final(answer):
    global _done, _final_answer
    _final_answer = str(answer)
    _done = True


def _final_var(var_name):
    global _done, _final_answer
    val = _namespace.get(str(var_name), str(var_name))
    if isinstance(val, str):
        _final_answer = val
    else:
        try:
            _final_answer = json.dumps(val)
        except (TypeError, ValueError):
            _final_answer = str(val)
    _done = True


def _apply_os_sandbox():
    """Best-effort OS-level sandbox. Failures logged to stderr, not fatal."""
    try:
        if sys.platform == "linux":
            _sandbox_linux()
    except Exception as e:
        print(f"[sandbox] setup failed: {e}", file=sys.stderr)


def _sandbox_linux():
    import ctypes
    import ctypes.util
    import os
    import struct

    libc = ctypes.CDLL(
        ctypes.util.find_library("c") or "libc.so.6", use_errno=True
    )

    PR_SET_NO_NEW_PRIVS = 38
    RULE_PATH_BENEATH = 1

    FS_EXECUTE   = 1 << 0
    FS_WRITE     = 1 << 1
    FS_READ_FILE = 1 << 2
    FS_READ_DIR  = 1 << 3
    FS_REMOVE_DIR  = 1 << 4
    FS_REMOVE_FILE = 1 << 5
    FS_MAKE_CHAR   = 1 << 6
    FS_MAKE_DIR    = 1 << 7
    FS_MAKE_REG    = 1 << 8
    FS_MAKE_SOCK   = 1 << 9
    FS_MAKE_FIFO   = 1 << 10
    FS_MAKE_BLOCK  = 1 << 11
    FS_MAKE_SYM    = 1 << 12

    ALL_FS = (FS_EXECUTE | FS_WRITE | FS_READ_FILE | FS_READ_DIR |
              FS_REMOVE_DIR | FS_REMOVE_FILE | FS_MAKE_CHAR | FS_MAKE_DIR |
              FS_MAKE_REG | FS_MAKE_SOCK | FS_MAKE_FIFO | FS_MAKE_BLOCK |
              FS_MAKE_SYM)
    READ_ONLY = FS_EXECUTE | FS_READ_FILE | FS_READ_DIR

    NR_CREATE   = 444
    NR_ADD_RULE = 445
    NR_RESTRICT = 446

    libc.syscall.restype = ctypes.c_long

    if libc.prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0:
        print("[sandbox] prctl(NO_NEW_PRIVS) failed", file=sys.stderr)
        return

    attr = struct.pack("=Q", ALL_FS)
    attr_buf = ctypes.create_string_buffer(attr)
    fd = libc.syscall(
        ctypes.c_long(NR_CREATE),
        ctypes.byref(attr_buf),
        ctypes.c_size_t(len(attr)),
        ctypes.c_uint32(0),
    )
    if fd < 0:
        print("[sandbox] Landlock not supported on this kernel", file=sys.stderr)
        return

    root_fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        rule = struct.pack("=Qi", READ_ONLY, root_fd)
        rule_buf = ctypes.create_string_buffer(rule)
        ret = libc.syscall(
            ctypes.c_long(NR_ADD_RULE),
            ctypes.c_int(fd),
            ctypes.c_int(RULE_PATH_BENEATH),
            ctypes.byref(rule_buf),
            ctypes.c_uint32(0),
        )
        if ret < 0:
            os.close(fd)
            print(f"[sandbox] landlock_add_rule failed: errno={ctypes.get_errno()}",
                  file=sys.stderr)
            return
    finally:
        os.close(root_fd)

    ret = libc.syscall(
        ctypes.c_long(NR_RESTRICT),
        ctypes.c_int(fd),
        ctypes.c_uint32(0),
    )
    os.close(fd)

    if ret == 0:
        print("[sandbox] Linux Landlock active", file=sys.stderr)
    else:
        print(f"[sandbox] landlock_restrict_self failed: errno={ctypes.get_errno()}",
              file=sys.stderr)


_apply_os_sandbox()

_blocked_builtins = frozenset({
    "open", "__import__", "exec", "eval", "compile",
    "breakpoint", "exit", "quit", "input",
})
_safe_builtins = {k: v for k, v in __builtins__.__dict__.items()
                  if k not in _blocked_builtins}

_namespace["__builtins__"] = _safe_builtins
_namespace["print"] = _print
_namespace["llm_query"] = _sub_lm
_namespace["sub_lm"] = _sub_lm  # backwards compat alias
_namespace["llm_batch"] = _llm_batch
_namespace["read_file"] = _read_file
_namespace["grep_file"] = _grep_file
_namespace["list_files"] = _list_files
_namespace["page_output"] = _page_output
_namespace["FINAL"] = _final
_namespace["FINAL_VAR"] = _final_var
_namespace["json"] = json
_namespace["re"] = re

_log("repl_ready", python=sys.version.split()[0])

while True:
    msg = _recv()

    if msg["type"] == "shutdown":
        break

    if msg["type"] == "enable_rlm_query":
        _namespace["rlm_query"] = _rlm_query
        continue

    if msg["type"] == "set_var":
        _log("set_var", name=msg["name"], value_len=len(str(msg["value"])))
        _namespace[msg["name"]] = msg["value"]
        _send({"type": "result", "stdout": "", "done": False})
        continue

    if msg["type"] == "exec":
        _exec_count += 1
        _stdout_capture.clear()
        _done = False
        _final_answer = None

        code = msg["code"]
        _log("exec_start", n=_exec_count, code_len=len(code), code_preview=code[:200])
        t0 = time.monotonic()

        if _has_alarm:
            signal.alarm(_EXEC_TIMEOUT)
        try:
            exec(code, _namespace)
        except TimeoutError:
            _log("exec_timeout", n=_exec_count, timeout=_EXEC_TIMEOUT)
            _stdout_capture.append(
                f"[Timeout] Code execution exceeded {_EXEC_TIMEOUT}s limit. "
                "Break into smaller steps or delegate to llm_query/llm_batch.\n")
        except Exception:
            tb = traceback.format_exc()
            _log("exec_error", n=_exec_count, traceback=tb)
            _stdout_capture.append(f"[Python Error] {tb}")
        finally:
            if _has_alarm:
                signal.alarm(0)

        elapsed_ms = round((time.monotonic() - t0) * 1000)
        stdout = "".join(_stdout_capture)
        _last_output = stdout

        user_vars = [k for k in _namespace if not k.startswith("_") and k != "__builtins__"]
        _log("exec_done", n=_exec_count, elapsed_ms=elapsed_ms, stdout_len=len(stdout), done=_done, has_final=_final_answer is not None, namespace_vars=user_vars)

        _send({
            "type": "result",
            "stdout": stdout,
            "done": _done,
            "final": _final_answer,
        })
