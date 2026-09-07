import hashlib,json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};source=root/'sources/multidisk-probe-v1/probe.ova'
with source.open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()=='1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0'
def run(name,args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660)
 (root/(name+'.json')).write_text(r.stdout);print(r.stdout+r.stderr,flush=True);assert r.returncode==0
 return json.loads(r.stdout)['data']
report=run('multidisk-inspect',['import','inspect',str(source)])
request={'destination':str(root/'prepared/multidisk-probe-v1'),'systemID':'virmill-multidisk-probe','disks':[{'id':'boot','format':'vmdk','maximumVirtualBytes':16<<20},{'id':'data','format':'vmdk','maximumVirtualBytes':3<<30}]}
(root/'multidisk-import-input.json').write_text(json.dumps(request,indent=2)+'\n')
plan=run('multidisk-import-plan',['import','prepare',str(source),'--input',json.dumps(request),'--plan'])
assert plan['operation']=='import.prepare' and plan['review']['systemID']=='virmill-multidisk-probe'
print(json.dumps({'inspectionAndPreparationPreview':'passed','sourceSHA256':'1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0','planID':plan['planID'],'planDigest':plan['planDigest'],'operationApplied':False}),flush=True)
