"""
RLM Python REPL bootstrap.
Embedded in Go binary via //go:embed. Started as `python3 -u` subprocess.
Enters a JSON-line message loop on stdin/stdout. Executes code blocks in a
persistent namespace. Callbacks to Go for sub_lm, read_file, grep_file, list_files.
"""
import json
import sys
import re
import traceback

_namespace = {}
_stdout_capture = []
_done = False
_final_answer = None
_last_output = ""


def _send(msg):
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()


def _recv():
    line = sys.stdin.readline()
    if not line:
        sys.exit(0)
    return json.loads(line.strip())


def _print(*args):
    _stdout_capture.append(" ".join(str(a) for a in args) + "\n")


def _sub_lm(prompt, max_tokens=4096):
    _send({"type": "callback", "func": "sub_lm",
           "args": {"prompt": str(prompt), "max_tokens": int(max_tokens)}})
    resp = _recv()
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _read_file(path, start_line=None, end_line=None):
    args = {"path": str(path)}
    if start_line is not None:
        args["start_line"] = int(start_line)
    if end_line is not None:
        args["end_line"] = int(end_line)
    _send({"type": "callback", "func": "read_file", "args": args})
    resp = _recv()
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _grep_file(pattern, path=None):
    args = {"pattern": str(pattern)}
    if path is not None:
        args["path"] = str(path)
    _send({"type": "callback", "func": "grep_file", "args": args})
    resp = _recv()
    if resp.get("error"):
        raise RuntimeError(resp["error"])
    return resp["result"]


def _list_files(pattern):
    _send({"type": "callback", "func": "list_files",
           "args": {"pattern": str(pattern)}})
    resp = _recv()
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
    try:
        _final_answer = json.dumps(val)
    except (TypeError, ValueError):
        _final_answer = str(val)
    _done = True


_namespace["print"] = _print
_namespace["sub_lm"] = _sub_lm
_namespace["read_file"] = _read_file
_namespace["grep_file"] = _grep_file
_namespace["list_files"] = _list_files
_namespace["page_output"] = _page_output
_namespace["FINAL"] = _final
_namespace["FINAL_VAR"] = _final_var
_namespace["json"] = json
_namespace["re"] = re

while True:
    msg = _recv()

    if msg["type"] == "shutdown":
        break

    if msg["type"] == "set_var":
        _namespace[msg["name"]] = msg["value"]
        _send({"type": "result", "stdout": "", "done": False})
        continue

    if msg["type"] == "exec":
        _stdout_capture.clear()
        _done = False
        _final_answer = None

        try:
            exec(msg["code"], _namespace)
        except Exception:
            _stdout_capture.append(f"[Python Error] {traceback.format_exc()}")

        stdout = "".join(_stdout_capture)
        _last_output = stdout

        _send({
            "type": "result",
            "stdout": stdout,
            "done": _done,
            "final": _final_answer,
        })
