#!/usr/bin/python3
"""Second-language synthetic protocol fixture. No VM access or host side effects."""
import hashlib
import json
import sys

ready = False

def unique(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError('duplicate key')
        result[key] = value
    return result

def handle(method, params):
    global ready
    if method == 'initialize':
        if ready or '1.0' not in params.get('protocolVersions', []):
            raise ValueError('version mismatch or repeated initialize')
        if {'name': 'vm.read', 'scope': 'selection'} not in params.get('permissions', []):
            raise ValueError('permission denied')
        ready = True
        return {'protocolVersion': '1.0', 'plugin': {'id': 'example.virmill.python-summary', 'version': '0.1.0'}, 'extensionTypes': ['action']}
    if not ready:
        raise ValueError('initialize first')
    if method == 'describe':
        return {'actions': [{'id': 'summary', 'readOnly': True}]}
    if method in ('shutdown', 'ping'):
        return {'ok': True}
    if method not in ('action.plan', 'action.execute'):
        raise LookupError('method not found')
    vms = params.get('context', {}).get('selectedVMs', [])
    if params.get('action') != 'summary' or not vms or len(vms) > 5000:
        raise ValueError('invalid selection')
    inputs = {key: params[key] for key in ('action', 'input', 'context')}
    token = hashlib.sha256(json.dumps(inputs, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    if method == 'action.plan':
        return {'summary': 'Summarize selected VMs', 'effects': [], 'requires': [], 'planToken': token}
    if params.get('planToken') != token:
        raise ValueError('stale plan')
    return {'count': len(vms), 'rows': sorted(vms, key=lambda v: v['name'])}

for frame in sys.stdin.buffer:
    if len(frame) > 8388608:
        sys.exit(2)
    request = None
    try:
        request = json.loads(frame.decode('utf-8'), object_pairs_hook=unique, parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite number')))
        if not isinstance(request, dict) or request.get('jsonrpc') != '2.0':
            raise ValueError('invalid envelope')
        result = handle(request['method'], request.get('params', {}))
        if 'id' in request:
            print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}), flush=True)
        if request['method'] == 'shutdown':
            break
    except (ValueError, KeyError, LookupError) as exc:
        if isinstance(request, dict) and 'id' not in request:
            continue
        code = -32601 if isinstance(exc, LookupError) and not isinstance(exc, KeyError) else -32602
        print(json.dumps({'jsonrpc': '2.0', 'id': request.get('id') if isinstance(request, dict) else None, 'error': {'code': code, 'message': str(exc)}}), flush=True)
