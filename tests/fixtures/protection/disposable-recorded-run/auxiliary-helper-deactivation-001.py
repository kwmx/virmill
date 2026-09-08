"""Parent-only, single-use restoration of the two previously inactive helper units.

No guest or storage mutations. Reuses only the hash-pinned native observations.
Failure retains receipts and does not restart units or retry a stop.
"""
import argparse
import hashlib
import json
import os
import pathlib
import sys
import types

ROOT = pathlib.Path('/home/virmill-test/virmill-tests/run-65930c6-20260907')
NAME = 'auxiliary-helper-deactivation-001'
WRAPPER_SHA = 'f7994d0e8b67a3a5327d3ea0ba4fee9c8e8b59df70c32ca910885f1c6be01833'
WINDOW_REPORT_SHA = '6797ebef8cece52582d4b170c1b6d67c1e4c8a64e6d244959a9ecf136a37b599'


def require(value, message):
    if not value:
        raise RuntimeError(message)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--recipe-sha256', required=True)
    p.add_argument('--execute-reviewed', action='store_true', required=True)
    args = p.parse_args()
    require(os.getuid() == 1000 and pathlib.Path.home() == pathlib.Path('/home/virmill-test')
            and ROOT.is_dir() and ROOT.resolve() == ROOT, 'designated ordinary actor/root required')
    require(hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest() == args.recipe_sha256,
            'reviewed recipe hash differs')
    sys.dont_write_bytecode = True
    path = ROOT/'sources/auxiliary-tui-policy-window-001/auxiliary-tui-policy-window-001.py'
    require(path.resolve() == path and path.is_file() and path.stat().st_size <= 1 << 20,
            'wrapper path/type/size differs')
    raw = path.read_bytes()
    require(hashlib.sha256(raw).hexdigest() == WRAPPER_SHA, 'wrapper source hash differs')
    w = types.ModuleType('pinned_window'); w.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), w.__dict__)
    n = w.load_pinned(ROOT/'sources'/w.NATIVE_NAME/(w.NATIVE_NAME+'.py'), w.NATIVE_SHA, 'pinned_native')
    args.revision, args.deployment_sha256, args.binary_sha256 = w.REVISION, w.DEPLOYMENT_SHA, w.BINARIES['virmill']
    os.umask(0o077)
    output = ROOT/NAME
    output.mkdir(mode=0o700, exist_ok=False)
    w.sync_directory(output); w.sync_directory(ROOT)
    r = w.make_run(n, args); r.output = output
    report = {'recipe': NAME, 'recipeSHA256': args.recipe_sha256, 'status': 'inconclusive',
              'failures': [], 'stopAttempted': False, 'stopAcknowledged': False,
              'policyChanged': False, 'guestOrStorageChanged': False, 'statePayloadRead': False}
    units = ['virmill-host-helper.socket', 'virmill-host-helper.service']
    try:
        raw = n.read_regular(w.OUTPUT/'report.json')
        require(w.sha(raw) == WINDOW_REPORT_SHA, 'positive window report hash differs')
        prior = n.strict_json(raw)
        require(prior['status'] == prior['tuiResult'] == 'passed' and prior['policyRestored']
                and not prior['manualReviewRequired'] and prior['failures'] == [], 'window not restored')
        activated = n.strict_json(n.read_regular(ROOT/'auxiliary-helper-activation-001/report.json'))
        require(activated['before'] == 'inactive' and activated['status'] == 'passed'
                and activated['units'] == units and activated['helperPID'] == '31125'
                and activated['runningExecutableSHA256'] == w.BINARIES['virmill-host-helper'],
                'activation ownership receipt differs')
        environment = n.strict_json(n.read_regular(ROOT/'environment.json'))
        for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
            value = pathlib.Path(environment[key])
            require(value.is_relative_to(ROOT) and value.resolve() == value and value.is_dir(),
                    'private environment differs')
            r.env[key] = str(value)
        r.runtime()
        require(r.report['runtime'] == w.RUNTIME, 'running identities differ')
        expected_policy = n.strict_json(n.read_regular(w.OUTPUT/'policy-restore-receipt.json'))
        original_policy = n.strict_json(n.read_regular(w.NATIVE/'original-public-policy.json'))
        before_policy = r.root('policy-read')
        w.check_original(n, before_policy, original_policy, expected_policy)
        def observe():
            ids = n.uuid_list(r.virsh('list', '--all', '--uuid'))
            require(set(ids) == w.GUESTS, 'nine-guest inventory differs')
            return {'guests': {vm: w.sha(r.stopped_xml(vm)) for vm in ids},
                    'journal': json.loads(json.dumps(r.journal())),
                    'helper': r.root('prior-helper-meta'), 'fixture': r.root('fixture-meta')}
        before = observe()
        for key, value in before.items():
            require(value == n.strict_json(n.read_regular(w.OUTPUT/(key+'-after.json'))),
                    'positive window preservation differs: '+key)
        r.save('before.json', {'observations': before, 'policy': before_policy, 'runtime': r.report['runtime']})
        r.save('intent.json', {**report, 'units': units, 'expectedHelperPID': '31125',
               'expectedHelperStartMonotonic': '51386551107', 'originalState': 'inactive',
               'windowReportSHA256': WINDOW_REPORT_SHA, 'wrapperSHA256': WRAPPER_SHA,
               'uncertainReply': 'retain evidence; do not retry or restart; parent review required'})
        # Last observations before the only mutation; external systemd writers
        # must remain idle. This is not an atomic compare-and-stop interface.
        require(all(r.property(u, 'ActiveState') == 'active' for u in units)
                and r.property(units[1], 'MainPID') == '31125'
                and r.property(units[1], 'ExecMainStartTimestampMonotonic') == '51386551107'
                and n.same_policy(r.root('policy-read'), before_policy), 'pre-stop identity/policy drift')
        report['stopAttempted'] = True
        r.checked(['/usr/bin/sudo', '-n', '/usr/bin/systemctl', 'stop', *units])
        report['stopAcknowledged'] = True
        require(all(r.property(u, 'ActiveState') == 'inactive' for u in units)
                and r.property(units[1], 'MainPID') == '0', 'helper units not inactive')
        after_policy = r.root('policy-read')
        require(n.same_policy(after_policy, before_policy), 'policy changed during deactivation')
        after = observe()
        require(after == before, 'guest/journal/helper/fixture metadata preservation failed')
        require(r.property(w.RUNTIME['unit'], 'ActiveState', True) == 'active'
                and r.property(w.RUNTIME['unit'], 'MainPID', True) == '30897'
                and w.sha(n.read_regular('/proc/30897/exe', 128 << 20, proc=True)) == w.BINARIES['virmilld'],
                'coordinator changed')
        require(all(r.property(u, 'ActiveState') == 'inactive' for u in units)
                and r.property(units[1], 'MainPID') == '0', 'helper units reactivated during final observations')
        r.save('after.json', {'observations': after, 'policy': after_policy})
        report.update(status='passed', restoredUnits='inactive', helperPID='0', coordinatorPID='30897',
                      guestsPreserved=True, journalPreserved=True, helperMetadataPreserved=True,
                      fixturePreserved=True, policyPreserved=True)
    except BaseException as error:
        report['failures'].append(type(error).__name__+': '+str(error))
    r.save('report.json', report)
    print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
