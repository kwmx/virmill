#!/usr/bin/python3
"""Deliberately hostile protocol peer for tests; no VM or network operations.

This is not an installable plugin. Fault selection is supplied by the Go tests
through fixtureFault in a request. The sandbox-only observation reads a generated
test path and attempts socket creation without connecting. Only stdlib is used.
"""

import json
import os
import socket
import sys
import time


def write(data):
    sys.stdout.buffer.write(data)
    sys.stdout.buffer.flush()


def reply(request, result):
    write(json.dumps({"jsonrpc": "2.0", "id": request["id"], "result": result}).encode() + b"\n")


def hang():
    sys.stderr.write("FAULT_READY\n")
    sys.stderr.flush()
    while True:
        time.sleep(60)


for line in sys.stdin.buffer:
    request = json.loads(line)
    fault = request.get("params", {}).get("fixtureFault")
    identifier = json.dumps(request.get("id")).encode()
    prefix = b'{"jsonrpc":"2.0","id":' + identifier
    if fault == "partial-exit":
        write(prefix + b',"result":{"success":')
        sys.exit(73)
    if fault == "unterminated-exit":
        write(prefix + b',"result":{"success":true}}')
        sys.exit(73)
    if fault == "duplicate-key":
        write(prefix + b',"result":{},"res\\u0075lt":{"success":true}}\n')
    elif fault == "oversized-line":
        write(prefix + b',"result":"' + b"x" * 8388608 + b'"}\n')
    elif fault == "wrong-id":
        write(b'{"jsonrpc":"2.0","id":"h-unissued","result":{"success":true}}\n')
    elif fault == "invalid-utf8":
        write(prefix + b',"result":"\xff"}\n')
    elif fault == "stdout-log":
        write(b"This diagnostic is not a protocol frame.\n")
    elif fault == "request-response-hybrid":
        write(prefix + b',"method":"host.operation.apply","result":{"success":true}}\n')
    elif fault == "wrong-version":
        write(b'{"jsonrpc":"1.0","id":' + identifier + b',"result":{"success":true}}\n')
    elif fault == "unknown-notification":
        write(b'{"jsonrpc":"2.0","method":"host.unsolicited","params":{}}\n')
    elif fault == "negotiated-version-mismatch":
        reply(request, {"protocolVersion": "2.0", "plugin": {"id": "example.virmill.protocol-faults", "version": "0.1.0"}, "extensionTypes": ["action"]})
    elif fault == "missing-protocol-version":
        reply(request, {"plugin": {"id": "example.virmill.protocol-faults", "version": "0.1.0"}, "extensionTypes": ["action"]})
    elif fault == "non-object-initialize-result":
        reply(request, ["1.0"])
    elif fault in ("apply-before-initialize", "apply-during-plan"):
        write(b'{"jsonrpc":"2.0","id":"p-mutate","method":"host.operation.apply","params":{"planToken":"read-token-is-not-authority","grants":["vm.read"],"operation":"vm.delete","resourceID":"synthetic-only"}}\n')
        host_reply = json.loads(sys.stdin.buffer.readline())
        denied = host_reply.get("id") == "p-mutate" and "result" not in host_reply and host_reply.get("error", {}).get("data", {}).get("code") == "PERMISSION_DENIED"
        if fault == "apply-before-initialize":
            # Even denial must not make an illegal pre-initialization session valid.
            reply(request, {"protocolVersion": "1.0", "plugin": {"id": "example.virmill.protocol-faults", "version": "0.1.0"}, "extensionTypes": ["action"], "hostDenied": denied})
        elif denied:
            write(json.dumps({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32010, "message": "fixture observed denied mutation", "data": {"code": "PERMISSION_DENIED", "fixtureDeniedMutation": True}}}).encode() + b"\n")
        else:
            reply(request, {"mutationGranted": True, "hostReply": host_reply})
    elif fault == "hang":
        hang()
    elif fault == "stop-reading":
        reply(request, {"protocolVersion": "1.0", "plugin": {"id": "example.virmill.protocol-faults", "version": "0.1.0"}, "extensionTypes": ["action"]})
        hang()
    elif fault == "flood-after-reply":
        reply(request, {"ready": True})
        while True:
            write(b'{"jsonrpc":"2.0","id":"h-unissued","result":{"success":true}}\n')
    elif fault == "stderr-flood":
        sys.stderr.buffer.write(b"bounded-test-log\n" * 16384)
        sys.stderr.buffer.flush()
        write(prefix + b',"result":{},"result":{"success":true}}\n')
    elif fault == "sandbox-observation":
        try:
            with open(request["params"]["hostFixturePath"], "rb") as source:
                readable = bool(source.read(1))
        except OSError:
            readable = False
        sockets = {}
        for name, family in (("inet", socket.AF_INET), ("unix", socket.AF_UNIX)):
            try:
                probe = socket.socket(family, socket.SOCK_STREAM)
                probe.close()
                sockets[name] = {"allowed": True}
            except OSError as error:
                sockets[name] = {"allowed": False, "errno": error.errno}
        reply(request, {"hostFileReadable": readable, "sockets": sockets, "uid": os.getuid(), "sensitiveEnvironment": any(name in os.environ for name in ("SSH_AUTH_SOCK", "DBUS_SESSION_BUS_ADDRESS", "HOME"))})
    elif request.get("method") == "initialize":
        reply(request, {"protocolVersion": "1.0", "plugin": {"id": "example.virmill.protocol-faults", "version": "0.1.0"}, "extensionTypes": ["action"]})
    elif request.get("method") == "shutdown":
        reply(request, {"ok": True})
        break
    else:
        # Any later successful reply exposes reuse of a poisoned session.
        reply(request, {"success": True})
