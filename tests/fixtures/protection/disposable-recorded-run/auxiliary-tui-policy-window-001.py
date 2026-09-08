"""Single-use positive TUI policy window; only --self-test is local authoring.

Normal execution belongs exclusively to the parent on the authorized disposable
host. The pinned native003 module owns the public-policy comparison/replacement.
"""
import argparse
import base64
import copy
import hashlib
import json
import os
import pathlib
import re
import stat
import sys
import tempfile
import time
import types
import unittest
from unittest import mock

ROOT = pathlib.Path('/home/virmill-test/virmill-tests/run-65930c6-20260907')
RECIPE = 'auxiliary-tui-policy-window-001'
OUTPUT = ROOT / 'aux-tui-policy-window001'
NATIVE_NAME = 'auxiliary-inspection-native-003'
TUI_NAME = 'auxiliary-inspection-tui-001'
NATIVE = ROOT / NATIVE_NAME
NATIVE_SHA = '32b047be8a48c1a2b1d191b1515d3d632329a0eacc6322bb661e7ccc91d36bcc'
TUI_SHA = 'b3a80e536dc644037c486d6f6e7dd8f1a35001e2d4a4c5d47474ac4d1f75a925'
NATIVE_REPORT_SHA = '4447e177dd0df0aabfbda022495a0f081c1a23ec5d747c5458105d8207377d8c'
ORIGINAL_POLICY_SHA = '1ec86a37a44cac12e7fa7c16c542ec83e552e348ea2f2cec841597dc999d6ed4'
REVISION = '490b88cba5bf6e0837e2cb5780e56211c22a7d27'
DEPLOYMENT_SHA = '5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c'
BINARIES = {
    'virmill': '9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886',
    'virmilld': '44522f6cbe2dd7eb54134038adfee4bd650ced9e4ae5aafe4e042471158cd7aa',
    'virmill-host-helper': '4d67ec32a0aa891d16af237885fe45ba95ee5377829f86ab4761aba89b42917a',
}
RUNTIME = {'unit': 'virmill-test-490b88c.service', 'pid': '30897', 'helperPID': '31125', 'binarySHA256': BINARIES}
VM = '074d9083-1953-4941-b006-9e301bb6d907'
GUESTS = {VM, '2ec994ce-2950-498c-8b19-d2f7dbb53a78', '50a0583b-3587-495f-90f4-9a42b8ad2fa3',
          '666c692d-da0e-4119-9554-727c4af3c751', 'a19bf9ee-cd7f-4921-baac-39ce1694eb35',
          'ae630461-91d3-4f07-ad88-e6842c3dc3ea', 'b9496482-2eeb-40e1-892b-4e291c108c52',
          'd4c95f21-28bc-428d-9e5f-ceda025d279e', 'f78674f3-bf3a-43e5-81f9-4283e2472024'}
COUNTS = {'plans': 36, 'jobs': 32, 'metadata': 24, 'events': 177, 'dedup': 32, 'locks': 0}
POLICY_METADATA = ('mode', 'uid', 'gid', 'size', 'links', 'mtimeNS')


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def read_source(path, digest):
    path = pathlib.Path(path)
    require(path.is_absolute() and path.resolve() == path, 'source path is not absolute without symlinks')
    fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= 1 << 20,
                'source is not one bounded regular file')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            raw = stream.read((1 << 20) + 1)
        signature = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        require(len(raw) == before.st_size and signature(before) == signature(os.fstat(fd)) ==
                signature(path.lstat()) and sha(raw) == digest, 'source changed or source hash differs')
        return raw
    finally:
        os.close(fd)


def load_pinned(path, digest, name):
    # Compile the exact verified bytes, never import an adjacent module/pycache.
    raw = read_source(path, digest)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def policy_bytes(snapshot):
    return base64.b64decode(snapshot['bytes'], validate=True)


def check_original(native, current, original, restored):
    require(sha(policy_bytes(original)) == ORIGINAL_POLICY_SHA and
            native.same_policy(current, restored), 'original policy bytes or restored generation differs')
    require(current['bytes'] == original['bytes'] and current['xattrs'] == original['xattrs'] and
            all(current['stat'][key] == original['stat'][key] for key in POLICY_METADATA),
            'original policy access metadata/mtime differs')


def check_grant(native, original, grant, identity, ownership):
    require(ownership == {'user': 'qemu', 'group': 'qemu', 'uid': 107, 'gid': 107}, 'native state owner differs')
    expected = native.policy_variant(native.strict_json(policy_bytes(original)), identity, VM, 107, 107, True)
    require(native.strict_json(policy_bytes(grant)) == expected, 'saved grant broadens or changes approved metadata scope')
    require(grant['xattrs'] == original['xattrs'] and all(grant['stat'][key] == original['stat'][key]
            for key in ('mode', 'uid', 'gid', 'links', 'mtimeNS')), 'saved grant changes original access metadata')
    require(grant['stat']['size'] == len(policy_bytes(grant)), 'saved grant byte count differs')
    return policy_bytes(grant)


def tui_arguments(policy_sha):
    return ['/usr/bin/python3', '-B', '-u', str(ROOT/'sources'/TUI_NAME/(TUI_NAME+'.py')),
            '--mode', 'positive', '--native-recipe', NATIVE_NAME, '--native-recipe-sha256', NATIVE_SHA,
            '--native-report-sha256', NATIVE_REPORT_SHA, '--policy-sha256', policy_sha,
            '--recipe-sha256', TUI_SHA, '--revision', REVISION, '--deployment-sha256', DEPLOYMENT_SHA,
            '--binary-sha256', BINARIES['virmill'], '--prepared-positive-policy']


def validate_tui_report(report, policy_sha):
    require(report['recipe'] == TUI_NAME and report['mode'] == 'positive' and report['status'] == 'passed' and
            report['failures'] == [] and report['recipeSHA256'] == TUI_SHA and
            report['nativeReportSHA256'] == NATIVE_REPORT_SHA and report['revision'] == REVISION and
            report['deploymentManifestSHA256'] == DEPLOYMENT_SHA and report['preparedPolicySHA256'] == policy_sha,
            'actual TUI report is not a passed pinned positive observation')
    require(report['nativeFixture'] == {'recipe': NATIVE_NAME, 'status': 'passed', 'recipeSHA256': NATIVE_SHA,
            'reportSHA256': NATIVE_REPORT_SHA, 'vmID': VM}, 'TUI native selection differs')
    require(all(report['runtime'][key] == value for key, value in RUNTIME.items()), 'TUI runtime differs')
    require(report.get('actualTUIObserved') is True and type(report.get('pageCount')) is int and
            0 < report['pageCount'] <= 128 and len(report['checks']) == 2 and
            report['terminal'] == {'rows': 24, 'columns': 80}, 'actual current TUI pages/checks incomplete')
    for key in ('policy', 'journal', 'guests', 'pools', 'media', 'files'):
        require(report.get(key+'Preserved') is True, 'TUI preservation missing: '+key)
    for key in ('captureVerified', 'independentRestoreVerified', 'guestBootVerified', 'policyChangedByRecipe',
                'auxiliaryPayloadBytesRead', 'mediaPayloadBytesRead'):
        require(report[key] is False, 'TUI report contains unsupported mutation/proof claim: '+key)
    require(report['noApplyOrLifecycleSubmitted'] is True and report['positivePolicyRestorationOwnedByParent'] is True,
            'TUI authorization boundary differs')


def make_run(native, args):
    class PolicyRun(native.Run):
        def save(self, name, value):
            require(pathlib.Path(name).name == name, 'output must be a direct child')
            raw = value if isinstance(value, bytes) else (json.dumps(value, sort_keys=True, indent=2)+'\n').encode()
            require(len(raw) <= 32 << 20, 'output artifact bound')
            super().save(name, raw)
            sync_directory(self.output)

        def root(self, action, cleanup=False, **fields):
            require(action in ('policy-read', 'policy-replace', 'process-hash', 'prior-helper-meta', 'fixture-meta'),
                    'wrapper refuses native guest/state mutation or payload read')
            if action == 'policy-replace':
                require(fields['phase'] in ('grant', 'restore'), 'wrapper is positive-only')
            return super().root(action, cleanup, **fields)

        def virsh(self, *arguments, write=False):
            require(not write, 'wrapper never changes native definitions')
            return super().virsh(*arguments, write=False)

    runner = PolicyRun(args)
    runner.output = OUTPUT
    for env in (runner.host_env, runner.env):
        env['PYTHONDONTWRITEBYTECODE'] = '1'
        for key in ('VIRSH_DEBUG', 'VIRSH_LOG_FILE'):
            env.pop(key, None)
    return runner


class Window:
    def __init__(self, native, tui, args):
        self.n, self.t = native, tui
        self.run = make_run(native, args)
        self.baselines = {}
        self.report = {'recipe': RECIPE, 'recipeSHA256': args.recipe_sha256, 'revision': REVISION,
            'deploymentManifestSHA256': DEPLOYMENT_SHA, 'nativeRecipeSHA256': NATIVE_SHA,
            'nativeReportSHA256': NATIVE_REPORT_SHA, 'tuiRecipeSHA256': TUI_SHA,
            'status': 'inconclusive', 'failures': [], 'checks': [], 'tuiResult': 'not-run',
            'grantAttempted': False, 'policyRestored': False, 'manualReviewRequired': False,
            'guestStarted': False, 'guestOrStateChangedByWrapper': False, 'auxiliaryPayloadBytesRead': False,
            'captureVerified': False, 'independentRestoreVerified': False, 'guestBootVerified': False}
        self.run.report = self.report

    def artifact(self, name):
        raw = self.n.read_regular(NATIVE/name)
        self.run.save('native-'+name, raw)
        return self.n.strict_json(raw)

    def guests(self):
        values = self.n.uuid_list(self.run.virsh('list', '--all', '--uuid'))
        require(set(values) == GUESTS, 'exact nine stopped guests required')
        result = {vm: sha(self.run.stopped_xml(vm)) for vm in values}
        for vm in values:
            require(self.run.virsh('domstate', vm).strip() == b'shut off', 'guest state changed around inventory')
        return result

    def journal(self):
        result = self.run.journal()
        require({table: len(result[table]) for table in COUNTS} == COUNTS, 'expected 301 rows/zero locks differ')
        # Native Run returns schema tuples in memory, JSON artifacts contain lists.
        return json.loads(json.dumps(result))

    def runtime(self):
        self.run.runtime()
        require(self.report['runtime'] == RUNTIME, 'pinned actual coordinator/helper process differs')

    def preflight(self):
        n, r = self.n, self.run
        require(not os.path.lexists(ROOT/(TUI_NAME+'-positive')), 'positive TUI output already exists; no retry')
        raw = n.read_regular(NATIVE/'report.json')
        require(sha(raw) == NATIVE_REPORT_SHA, 'native report hash differs')
        report = n.strict_json(raw)
        self.t.eligible_native(report)
        require(report['vmID'] == VM and report['runtime'] == RUNTIME, 'native VM/runtime pins differ')
        r.save('selected-native-report.json', raw)
        environment = n.strict_json(n.read_regular(ROOT/'environment.json'))
        for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
            path = pathlib.Path(environment[key])
            require(path.is_absolute() and path.is_relative_to(ROOT) and path.resolve() == path and path.is_dir(),
                    'private environment differs')
            r.env[key] = str(path)
        require(r.env['XDG_STATE_HOME'] == str(ROOT/'state'), 'wrong coordinator journal')
        self.runtime()
        identity = r.cli('host', 'helper', 'identity')['data']
        historical_identity = self.artifact('public-helper-identity.json')
        require(identity['policyApproved'] is True and identity['actorUID'] == os.getuid() and all(
            identity[key] == historical_identity[key] for key in ('actorUID', 'keyID', 'publicKey', 'policyPath', 'socketPath')),
            'public helper actor/key/endpoint identity differs')
        original = self.artifact('original-public-policy.json')
        restored = self.artifact('policy-restore-receipt.json')
        grant = self.artifact('policy-grant-receipt.json')
        current = r.root('policy-read')
        check_original(n, current, original, restored)
        self.grant = check_grant(n, original, grant, identity, report['fixtureStateAccount'])
        r.original_policy, r.current_policy = original, current
        r.save('policy-before.json', current)
        self.report['grantPolicySHA256'] = sha(self.grant)
        self.report['originalPolicySHA256'] = ORIGINAL_POLICY_SHA
        expected_guests = self.artifact('baseline-guests.json')
        expected_guests[VM] = report['finalFixtureXMLSHA256']
        require(self.guests() == expected_guests, 'stopped XML differs from native003')
        require(self.journal() == self.artifact('baseline-journal.json'), 'native journal baseline differs')
        helper = r.root('prior-helper-meta')
        require(helper == self.artifact('baseline-helper-files.json'), 'historical helper journal metadata differs')
        # Metadata only, including the generated fixture; no TPM/NVRAM payload opens.
        self.baselines = {'guests': expected_guests, 'journal': self.journal(), 'helper': helper,
                          'fixture': r.root('fixture-meta')}
        r.save('preservation-before.json', self.baselines)
        self.report['checks'].append('pinned sources/native report/runtime, original policy generation/access metadata, nine stopped XMLs and 301 rows')

    def observe_tui(self):
        r = self.run
        argv = tui_arguments(sha(self.grant))
        r.save('intent.json', {'recipe': RECIPE, 'nativeReportSHA256': NATIVE_REPORT_SHA,
            'rootProgramSHA256': sha(self.n.ROOT_PROGRAM.encode()), 'expectedPolicy': r.current_policy,
            'originalPolicy': r.original_policy, 'grantPolicySHA256': sha(self.grant),
            'grantBytes': base64.b64encode(self.grant).decode(), 'tuiArguments': argv,
            'lostAcknowledgementAction': 'retain output; no forced restoration; parent manual comparison required'})
        r.stage = 'grant exact saved metadata-only policy'
        self.report['grantAttempted'] = True
        r.replace_policy('grant', self.grant)
        require(self.n.same_policy(r.root('policy-read'), r.current_policy), 'policy changed after grant acknowledgement')
        r.stage = 'positive TUI observation'
        # Run.command records output only AFTER the child completes. The TUI
        # inventories this wrapper's directory; do not write it while active.
        code, out, err = r.command(argv, timeout=600)
        raw = self.n.read_regular(ROOT/(TUI_NAME+'-positive')/'report.json')
        r.save('actual-tui-report.json', raw)
        self.report['tuiReportSHA256'] = sha(raw)
        actual = self.n.strict_json(raw)
        self.report['tuiReportedStatus'] = actual.get('status', 'invalid')
        self.report['tuiResult'] = 'inconclusive'
        validate_tui_report(actual, sha(self.grant))
        self.report['tuiResult'] = 'passed'
        require(code == 0 and not err and self.n.strict_json(out) == actual,
                'TUI exit/printed result differs from actual report')
        self.report['checks'].append('pinned positive TUI report passed with all preservation checks; no capture or boot proof')

    def restore(self):
        r = self.run
        if r.original_policy is None:
            return
        try:
            require(not r.policy_uncertain, 'policy acknowledgement unknown; manual comparison required')
            require(self.n.same_policy(r.root('policy-read', cleanup=True), r.current_policy),
                    'current policy generation differs; manual comparison required')
            if self.report['grantAttempted']:
                r.stage = 'restore exact original policy'
                r.replace_policy('restore', policy_bytes(r.original_policy))
            r.verify_policy_restored()
        except BaseException as error:
            self.report['policyRestored'] = False
            self.report['manualReviewRequired'] = True
            self.report['failures'].append('conditional policy restoration: '+str(error))

    def finish(self):
        self.run.final_started = time.monotonic()
        self.restore()
        observers = {'guests': self.guests, 'journal': self.journal,
                     'helper': lambda: self.run.root('prior-helper-meta', cleanup=True),
                     'fixture': lambda: self.run.root('fixture-meta', cleanup=True)}
        for name, before in self.baselines.items():
            try:
                after = observers[name]()
                require(after == before, name+' preservation differs')
                self.run.save(name+'-after.json', after)
                self.report[name+'Preserved'] = True
            except BaseException as error:
                self.report['failures'].append('final '+name+': '+str(error))
        try:
            self.runtime()
        except BaseException as error:
            self.report['failures'].append('final runtime: '+str(error))
        if not self.report['failures'] and self.report['tuiResult'] == 'passed' and self.report['policyRestored']:
            self.report['status'] = 'passed'
        self.report['commandCount'] = self.run.sequence
        self.run.save('report.json', self.report)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--recipe-sha256')
    parser.add_argument('--execute-reviewed', action='store_true')
    parser.add_argument('--exclusive-policy-window', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(not args.recipe_sha256 and not args.execute_reviewed and not args.exclusive_policy_window,
                'self-test takes no native options')
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SelfTests))
        return 0 if result.wasSuccessful() else 1
    require(args.execute_reviewed and args.exclusive_policy_window and
            re.fullmatch('[a-f0-9]{64}', args.recipe_sha256 or ''), 'parent review/exclusive window/source pin required')
    read_source(pathlib.Path(__file__).absolute(), args.recipe_sha256)
    require(os.getuid() != 0 and pathlib.Path.home() == pathlib.Path('/home/virmill-test') and
            ROOT.is_dir() and ROOT.resolve() == ROOT, 'designated ordinary disposable actor/root required')
    sys.dont_write_bytecode = True
    native = load_pinned(ROOT/'sources'/NATIVE_NAME/(NATIVE_NAME+'.py'), NATIVE_SHA, 'reviewed_native003')
    tui = load_pinned(ROOT/'sources'/TUI_NAME/(TUI_NAME+'.py'), TUI_SHA, 'reviewed_tui001')
    tui.configure_native(NATIVE_NAME, NATIVE_SHA)
    args.revision, args.deployment_sha256, args.binary_sha256 = REVISION, DEPLOYMENT_SHA, BINARIES['virmill']
    os.umask(0o077)
    OUTPUT.mkdir(mode=0o700, exist_ok=False)
    sync_directory(OUTPUT); sync_directory(ROOT)
    window = Window(native, tui, args)
    try:
        window.preflight()
        window.observe_tui()
    except BaseException as error:
        window.report['failures'].append(type(error).__name__+': '+str(error))
    finally:
        window.finish()
    print(json.dumps(window.report, sort_keys=True))
    return 0 if window.report['status'] == 'passed' else 1


class SelfTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Import only pinned source definitions. No native methods or children
        # run in these tests; root and policy replacement use in-memory seams.
        base = pathlib.Path(__file__).absolute().parent
        cls.native = load_pinned(base/(NATIVE_NAME+'.py'), NATIVE_SHA, 'test_native003')

    def original(self):
        raw = b'{"apiVersion":"virmill/v1","keys":{},"roots":{},"actors":{}}'
        return {'bytes': base64.b64encode(raw).decode(), 'xattrs': {'user.generated': '61'},
                'stat': {'mode': stat.S_IFREG | 0o644, 'uid': 0, 'gid': 0, 'links': 1, 'size': len(raw),
                         'mtimeNS': 12, 'ctimeNS': 13, 'atimeNS': 14, 'dev': 1, 'ino': 2}}

    def test_pinned_import_refuses_changed_bytes_before_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory)/'fixture.py'
            raw = b'raise AssertionError("must not execute")\n'
            path.write_bytes(raw)
            with self.assertRaisesRegex(RuntimeError, 'hash differs'):
                load_pinned(path, '0'*64, 'refused')
            path.write_text('answer = 42\n')
            self.assertEqual(load_pinned(path, sha(path.read_bytes()), 'accepted').answer, 42)
            self.assertFalse((path.parent/'__pycache__').exists())

    def test_source_leaf_and_ancestor_symlinks_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            real = root/'real'; real.mkdir(); path = real/'source.py'; path.write_bytes(b'pass\n')
            (root/'leaf.py').symlink_to(path); (root/'alias').symlink_to(real, target_is_directory=True)
            for alias in (root/'leaf.py', root/'alias/source.py'):
                with self.subTest(alias=alias), self.assertRaisesRegex(RuntimeError, 'symlinks'):
                    read_source(alias, sha(b'pass\n'))

    def seam(self, uncertain=False, drift=False, restore_lost=False):
        native, events = self.native, []
        snapshot = self.original()
        runner = native.Run(types.SimpleNamespace(revision=REVISION, deployment_sha256=DEPLOYMENT_SHA,
                            recipe_sha256='0'*64, binary_sha256=BINARIES['virmill']))
        runner.original_policy = copy.deepcopy(snapshot)
        runner.current_policy = copy.deepcopy(snapshot)
        runner.current_policy['bytes'] = base64.b64encode(b'{"granted":true}').decode()
        runner.policy_uncertain = uncertain
        def root(action, cleanup=False, **fields):
            events.append(action)
            if action == 'policy-replace':
                self.assertEqual(fields['phase'], 'restore')
                if restore_lost:
                    raise RuntimeError('lost reply')
                result = copy.deepcopy(snapshot); result['stat']['ino'] = 3
                return result
            value = copy.deepcopy(runner.current_policy)
            if drift: value['stat']['ctimeNS'] += 1
            return value
        runner.root = root
        runner.save = lambda name, value: events.append(name)
        window = object.__new__(Window)
        window.n, window.run = native, runner
        window.report = {'grantAttempted': True, 'policyRestored': False, 'manualReviewRequired': False,
                         'tuiResult': 'passed', 'failures': []}
        runner.report = window.report
        return window, events

    def test_lost_grant_ack_never_reads_or_restores_policy(self):
        window, events = self.seam(uncertain=True)
        window.restore()
        self.assertEqual(events, [])
        self.assertTrue(window.report['manualReviewRequired'])
        self.assertFalse(window.report['policyRestored'])

    def test_concurrent_policy_generation_never_replaced(self):
        window, events = self.seam(drift=True)
        window.restore()
        self.assertEqual(events, ['policy-read'])
        self.assertTrue(window.report['manualReviewRequired'])
        self.assertEqual(window.report['tuiResult'], 'passed')

    def test_known_grant_restores_and_rechecks_original_metadata(self):
        window, events = self.seam()
        window.restore()
        self.assertEqual(events, ['policy-read', 'policy-replace', 'policy-restore-receipt.json', 'policy-read'])
        self.assertTrue(window.report['policyRestored'])
        self.assertFalse(window.report['manualReviewRequired'])

    def test_lost_restore_ack_stays_uncertain_no_retry(self):
        window, events = self.seam(restore_lost=True)
        window.restore(); window.restore()
        self.assertEqual(events, ['policy-read', 'policy-replace'])
        self.assertTrue(window.run.policy_uncertain)
        self.assertTrue(window.report['manualReviewRequired'])
        self.assertFalse(window.report['policyRestored'])

    def test_positive_invocation_exact_pins_and_no_denial_fallback(self):
        arguments = tui_arguments('a'*64)
        self.assertEqual(arguments[:3], ['/usr/bin/python3', '-B', '-u'])
        self.assertEqual(arguments[arguments.index('--mode')+1], 'positive')
        self.assertIn('--prepared-positive-policy', arguments)
        for value in (NATIVE_SHA, NATIVE_REPORT_SHA, TUI_SHA, REVISION, DEPLOYMENT_SHA, BINARIES['virmill'], 'a'*64):
            self.assertIn(value, arguments)

    def test_wrapper_restricts_imported_native_mutation_methods(self):
        args = types.SimpleNamespace(revision=REVISION, deployment_sha256=DEPLOYMENT_SHA,
                                    recipe_sha256='0'*64, binary_sha256=BINARIES['virmill'])
        runner = make_run(self.native, args)
        for action in ('create', 'fifo-add', 'fifo-remove', 'media'):
            with self.subTest(action=action), self.assertRaisesRegex(RuntimeError, 'refuses'):
                runner.root(action)
        with self.assertRaisesRegex(RuntimeError, 'positive-only'):
            runner.root('policy-replace', phase='root-only')
        with self.assertRaisesRegex(RuntimeError, 'never changes'):
            runner.virsh('define', write=True)

    def test_original_generation_access_metadata_and_bytes_are_required(self):
        original = self.original()
        current = copy.deepcopy(original); current['stat']['ino'] = 17
        restored = copy.deepcopy(current); current['stat']['atimeNS'] += 1
        with mock.patch.dict(check_original.__globals__, ORIGINAL_POLICY_SHA=sha(policy_bytes(original))):
            check_original(self.native, current, original, restored)
            for key in POLICY_METADATA + ('ino', 'ctimeNS'):
                bad = copy.deepcopy(current); bad['stat'][key] += 1
                with self.subTest(field=key), self.assertRaises(RuntimeError):
                    check_original(self.native, bad, original, restored)
            bad = copy.deepcopy(current); bad['xattrs']['user.generated'] = '62'
            with self.assertRaises(RuntimeError):
                check_original(self.native, bad, original, restored)
            bad = copy.deepcopy(current); bad['bytes'] = base64.b64encode(b'{}').decode()
            with self.assertRaises(RuntimeError):
                check_original(self.native, bad, original, restored)

    def test_saved_grant_reconstruction_rejects_broader_scope_or_metadata(self):
        original = self.original()
        identity = {'actorUID': 1000, 'keyID': 'test-only', 'publicKey': 'generated-public-data'}
        decoded = {'apiVersion': 'virmill/v1', 'keys': {'test-only': 'generated-public-data'},
                   'roots': {'original': '/unchanged'}, 'actors': [1000]}
        original['bytes'] = base64.b64encode(json.dumps(decoded).encode()).decode()
        original['stat']['size'] = len(policy_bytes(original))
        expected = self.native.policy_variant(decoded, identity, VM, 107, 107, True)
        def receipt(value):
            grant = copy.deepcopy(original)
            raw = (json.dumps(value, sort_keys=True)+'\n').encode()
            grant['bytes'], grant['stat']['size'] = base64.b64encode(raw).decode(), len(raw)
            return grant
        ownership = {'user': 'qemu', 'group': 'qemu', 'uid': 107, 'gid': 107}
        grant = receipt(expected)
        self.assertEqual(check_grant(self.native, original, grant, identity, ownership), policy_bytes(grant))
        for key, value in (('allowCapture', True), ('maxBytes', 8193), ('maxMembers', 4),
                           ('actorUID', 1001), ('rootID', 'different'), ('resourceID', 'another-vm'),
                           ('keyID', 'another-key'), ('stateUID', 0), ('stateGID', 0)):
            bad = copy.deepcopy(expected); bad['auxiliary'][0][key] = value
            with self.subTest(field=key), self.assertRaisesRegex(RuntimeError, 'scope'):
                check_grant(self.native, original, receipt(bad), identity, ownership)
        for key in ('mode', 'uid', 'gid', 'links', 'mtimeNS'):
            bad = copy.deepcopy(grant); bad['stat'][key] += 1
            with self.subTest(metadata=key), self.assertRaisesRegex(RuntimeError, 'metadata'):
                check_grant(self.native, original, bad, identity, ownership)
        bad = copy.deepcopy(grant); bad['xattrs'] = {}
        with self.assertRaisesRegex(RuntimeError, 'metadata'):
            check_grant(self.native, original, bad, identity, ownership)

    def test_durable_intent_precedes_first_grant_attempt(self):
        events = []
        window, _ = self.seam()
        window.grant = b'{"generated":true}'
        window.run.save = lambda name, value: events.append(name)
        def refuse(phase, raw):
            events.append(phase)
            raise RuntimeError('simulated unknown acknowledgement')
        window.run.replace_policy = refuse
        with self.assertRaisesRegex(RuntimeError, 'simulated'):
            window.observe_tui()
        self.assertEqual(events, ['intent.json', 'grant'])
        self.assertTrue(window.report['grantAttempted'])
        # Failure to durably save intent prevents even attempting replacement.
        events.clear()
        def fail_save(name, value):
            events.append(name)
            raise OSError('simulated fsync failure')
        window.run.save = fail_save
        with self.assertRaises(OSError):
            window.observe_tui()
        self.assertEqual(events, ['intent.json'])

    def positive_report(self):
        report = {'recipe': TUI_NAME, 'mode': 'positive', 'status': 'passed', 'failures': [],
                  'recipeSHA256': TUI_SHA, 'nativeReportSHA256': NATIVE_REPORT_SHA, 'revision': REVISION,
                  'deploymentManifestSHA256': DEPLOYMENT_SHA, 'preparedPolicySHA256': 'a'*64,
                  'nativeFixture': {'recipe': NATIVE_NAME, 'status': 'passed', 'recipeSHA256': NATIVE_SHA,
                                    'reportSHA256': NATIVE_REPORT_SHA, 'vmID': VM},
                  'runtime': copy.deepcopy(RUNTIME), 'actualTUIObserved': True, 'pageCount': 18,
                  'checks': ['current pages', 'CLI/TUI parity'], 'terminal': {'rows': 24, 'columns': 80},
                  'noApplyOrLifecycleSubmitted': True, 'positivePolicyRestorationOwnedByParent': True}
        for key in ('policy', 'journal', 'guests', 'pools', 'media', 'files'):
            report[key+'Preserved'] = True
        for key in ('captureVerified', 'independentRestoreVerified', 'guestBootVerified', 'policyChangedByRecipe',
                    'auxiliaryPayloadBytesRead', 'mediaPayloadBytesRead'):
            report[key] = False
        return report

    def test_actual_tui_report_required_without_denial_or_false_proof(self):
        good = self.positive_report()
        validate_tui_report(good, 'a'*64)
        for key, value in (('status', 'inconclusive'), ('mode', 'denial'), ('actualTUIObserved', False),
                           ('policyPreserved', False), ('filesPreserved', False), ('pageCount', 0),
                           ('captureVerified', True), ('failures', ['failed after successful pages']),
                           ('preparedPolicySHA256', 'b'*64), ('nativeReportSHA256', 'b'*64)):
            bad = copy.deepcopy(good); bad[key] = value
            with self.subTest(field=key), self.assertRaises(RuntimeError):
                validate_tui_report(bad, 'a'*64)

    def test_tui_pass_does_not_make_failed_policy_window_pass(self):
        window, events = self.seam(drift=True)
        window.baselines = {}
        window.report['status'] = 'inconclusive'
        window.runtime = lambda: None
        window.finish()
        self.assertEqual(window.report['tuiResult'], 'passed')
        self.assertEqual(window.report['status'], 'inconclusive')
        self.assertTrue(window.report['manualReviewRequired'])
        self.assertNotIn('policy-replace', events)


if __name__ == '__main__':
    raise SystemExit(main())
