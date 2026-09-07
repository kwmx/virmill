import os,json,subprocess,pathlib,hashlib
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
binary=root/'packages/b7fe053/helper-b7fe053.test'
expected='be028c2f7cd78815de2679998859a9dfba4229a5cc4bf7c3c51dd4ccd51fa62a'
assert hashlib.sha256(binary.read_bytes()).hexdigest()==expected
print(json.dumps({'testBinarySHA256':expected,'sourceRevision':'b7fe053','sourceDigest':'c7071d1aaaf146058986ec06b887d8d59f8e33072dbec44cfb4596bcccf617f0','scope':'generated temporary ACL files and helper journals only; native mapping is synthetic'}),flush=True)
p=subprocess.run(['sudo','-n','env','VIRMILL_TEST_DISPOSABLE_HELPER_ACCESS=1',str(binary),'-test.v','-test.run','^TestDisposableRootAccessGrantRevokeAndLostAcknowledgement$'],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
print(p.stdout,flush=True)
raise SystemExit(p.returncode)
