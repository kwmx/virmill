import hashlib,json,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
items=[]
for p in sorted((pathlib.Path.home()/'images').iterdir()):
 if not p.is_file() or p.is_symlink():continue
 with p.open('rb') as f:digest=hashlib.file_digest(f,'sha256').hexdigest()
 item={'name':p.name,'bytes':p.stat().st_size,'sha256':digest,'origin':'owner-supplied; publisher authentication not established'}
 if p.suffix in ('.7z','.ova'):
  r=subprocess.run(['7z','l','-slt',str(p)],capture_output=True,text=True,timeout=120)
  (root/(p.name+'.listing.txt')).write_text(r.stdout+r.stderr)
  item['listingExitCode']=r.returncode
  item['listingSHA256']=hashlib.sha256((r.stdout+r.stderr).encode()).hexdigest()
 items.append(item);print(json.dumps(item),flush=True)
(root/'source-media.json').write_text(json.dumps(items,indent=2)+'\n')
