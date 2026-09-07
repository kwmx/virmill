import json,os,pathlib,shutil,subprocess
print(json.dumps({'setfacl':shutil.which('setfacl'),'getfacl':shutil.which('getfacl'),'sudoNoninteractive':subprocess.run(['sudo','-n','true']).returncode,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}))
