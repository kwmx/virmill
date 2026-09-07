import hashlib,json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
source=pathlib.Path.home()/'images/kali-linux-2026.2-qemu-amd64.7z'
assert os.getuid()!=0
output=root/'sources/kali-qemu'
output.mkdir(parents=True,mode=0o700,exist_ok=False)
args=['/usr/bin/prlimit','--as=4294967296','--fsize=17179869184','--cpu=600','--nofile=128','--nproc=128','--','/usr/bin/bwrap','--die-with-parent','--unshare-all','--new-session','--ro-bind','/usr','/usr','--symlink','usr/lib','/lib','--symlink','usr/lib64','/lib64','--proc','/proc','--dev','/dev','--tmpfs','/tmp','--dir','/input','--ro-bind',str(source),'/input/source.7z','--bind',str(output),'/output','--chdir','/output','/usr/bin/7z','x','-y','-o/output','/input/source.7z']
start=time.monotonic()
r=subprocess.run(args,capture_output=True,text=True,timeout=900)
(root/'extraction.log').write_text(r.stdout+r.stderr)
print(r.stdout+r.stderr,flush=True)
assert r.returncode==0,r.returncode
files=list(output.iterdir()); assert len(files)==1 and files[0].name=='kali-linux-2026.2-qemu-amd64.qcow2' and files[0].is_file() and not files[0].is_symlink()
def sha(p):
 with p.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
archive_digest=sha(source); assert archive_digest=='c7c35588d05277c482c908bf7a136d348f76ffa68700b04ff53c0b217e6bd071'
manifest={'archive':source.name,'archiveSHA256':archive_digest,'file':files[0].name,'bytes':files[0].stat().st_size,'sha256':sha(files[0]),'elapsedSeconds':round(time.monotonic()-start,2),'extractor':'7zip-26.02-1.fc44.x86_64','confinement':'unprivileged bwrap, all namespaces unshared, read-only archive, private output only, resource limits'}
files[0].chmod(0o400)
(root/'extracted-qemu.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(manifest,indent=2))
