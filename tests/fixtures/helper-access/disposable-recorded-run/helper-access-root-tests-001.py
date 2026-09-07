import os,json,subprocess,pathlib,hashlib
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
binary=root/'helper-access-development.test'
expected='4274a74f26196ee0aa6415e06f50b96c9f58bae9cbded257d9c1e7ff39defcbf'
assert hashlib.sha256(binary.read_bytes()).hexdigest()==expected
print(json.dumps({'testBinarySHA256':expected,'scope':'generated temporary ACL files and helper journals only; native mapping is synthetic'}),flush=True)
p=subprocess.run(['sudo','-n','env','VIRMILL_TEST_DISPOSABLE_HELPER_ACCESS=1',str(binary),'-test.v','-test.run','^TestDisposableRootAccessGrantRevokeAndLostAcknowledgement$'],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
print(p.stdout,flush=True)
raise SystemExit(p.returncode)
