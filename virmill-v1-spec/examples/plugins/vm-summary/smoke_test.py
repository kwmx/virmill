#!/usr/bin/env python3
"""Exercise the synthetic read-only plugin. No VM/application access is used."""
from __future__ import annotations
import copy
import json
import pathlib
import queue
import subprocess
import sys
import threading

class Client:
    def __init__(self, executable: str) -> None:
        self.p = subprocess.Popen([str(pathlib.Path(executable).resolve())], stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding="utf-8")
        self.q: queue.Queue[str] = queue.Queue()
        self.serial = 0
        def pump() -> None:
            assert self.p.stdout is not None
            for line in self.p.stdout:
                self.q.put(line)
        threading.Thread(target=pump, daemon=True).start()
    def raw(self, line: str) -> dict:
        assert self.p.stdin is not None
        self.p.stdin.write(line + "\n")
        self.p.stdin.flush()
        try:
            return json.loads(self.q.get(timeout=5))
        except queue.Empty as exc:
            raise AssertionError("No bounded protocol response") from exc
    def call(self, method: str, params: dict | None = None) -> dict:
        self.serial += 1
        ident = f"h-{self.serial}"
        result = self.raw(json.dumps({"jsonrpc": "2.0", "id": ident,
                                     "method": method, "params": params or {}}))
        assert result["id"] == ident, result
        return result
    def close(self) -> None:
        if self.p.stdin and not self.p.stdin.closed:
            self.p.stdin.close()
        try:
            self.p.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.p.kill()
            self.p.wait()
        assert self.p.returncode == 0, "Plugin did not exit cleanly"

def initialization(granted: bool = True) -> dict:
    return {"protocolVersions": ["1.0"], "host": {"version": "1.0.0", "os": "linux", "arch": "amd64"},
            "sessionID": "synthetic-session", "permissions": [{"name": "vm.read", "scope": "selection"}] if granted else [],
            "limits": {"maxMessageBytes": 8388608}}

def main() -> None:
    executable = sys.argv[1] if len(sys.argv) > 1 else "./vm-summary"
    checks = 0
    c = Client(executable)
    try:
        assert c.call("describe")["error"]["data"]["code"] == "NOT_INITIALIZED"; checks += 1
        init = c.call("initialize", initialization())["result"]
        assert init["plugin"]["id"] == "example.virmill.vm-summary"; checks += 1
        assert c.call("describe")["result"]["actions"][0]["id"] == "summary"; checks += 1
        params = {"action": "summary", "input": {"sortBy": "name"}, "context": {"selectedVMs": [
            {"id": "vm-b", "name": "Server", "state": "shutoff"},
            {"id": "vm-a", "name": "Desktop", "state": "running"}]}}
        planned = c.call("action.plan", params)["result"]
        assert planned["effects"] == []; checks += 1
        execution = {**params, "planToken": planned["planToken"], "operationID": "synthetic-operation", "grants": []}
        result = c.call("action.execute", execution)["result"]
        assert result["count"] == 2 and result["rows"][0]["id"] == "vm-a"; checks += 1
        stale = copy.deepcopy(execution)
        stale["context"]["selectedVMs"][0]["state"] = "running"
        assert c.call("action.execute", stale)["error"]["data"]["code"] == "STALE_PLAN"; checks += 1
        empty = copy.deepcopy(params); empty["context"]["selectedVMs"] = []
        assert c.call("action.plan", empty)["error"]["code"] == -32602; checks += 1
        assert c.call("no.such.method")["error"]["code"] == -32601; checks += 1
        assert c.raw('{"jsonrpc":"2.0","id":"h-x","method":"ping","method":"shutdown"}')["error"]["code"] == -32700; checks += 1
        assert c.call("ping")["result"]["ok"]; checks += 1
        assert c.call("shutdown")["result"]["ok"]; checks += 1
    finally:
        c.close()
    denied = Client(executable)
    try:
        assert "result" in denied.call("initialize", initialization(False))
        assert denied.call("action.plan", params)["error"]["data"]["code"] == "PERMISSION_DENIED"; checks += 1
        denied.call("shutdown")
    finally:
        denied.close()
    mismatch = Client(executable)
    try:
        bad = initialization(); bad["protocolVersions"] = ["2.0"]
        assert mismatch.call("initialize", bad)["error"]["data"]["code"] == "PROTOCOL_VERSION_UNSUPPORTED"; checks += 1
    finally:
        mismatch.close()
    print(json.dumps({"status": "passed", "smoke_checks": checks, "scope": "synthetic plugin only"}))

if __name__ == "__main__":
    main()
