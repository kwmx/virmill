import hashlib,json,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';recipe=root/'fixture-recipes/multidisk-probe-v1'
expected={'boot.S': 'e9e96f13d40ca5bcd7f1a1b9ddb0fbfc0e0330868d8fd74277e71b4abc0d41ab', 'build.py': '654ec71d62308bd6e0d08dff10742e017f7be6df1150852b3925d034eb85b638'}
for name,digest in expected.items():
 assert hashlib.sha256((recipe/name).read_bytes()).hexdigest()==digest
subprocess.run(['python3',str(recipe/'build.py'),'--destination',str(root/'sources/multidisk-probe-v1'),'--payload-mib','1024'],check=True)
