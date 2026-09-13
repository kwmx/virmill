import os,json,subprocess,pathlib
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
def run(args):
 p=subprocess.run(args,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);print(json.dumps({'argv':args,'exitCode':p.returncode,'output':p.stdout}),flush=True);return p
run(['id'])
run(['sudo','-n','true'])
run(['date','-u','+%FT%TZ'])
run(['systemctl','--user','show','<test-vm-login>-12d7bba.service','-p','ActiveState','-p','SubState'])
run(['sudo','-n','virsh','-c','qemu:///system','list','--all'])
run(['rpm','-q','openssl','libvirt-libs','acl'])
run(['sudo','-n','systemctl','is-active','virmill-host-helper.socket','virmill-host-helper.service'])
for path in ['/etc/virmill/helper-policy.json','/run/virmill-host-helper','/var/lib/virmill-host-helper']:
 run(['sudo','-n','stat','-c','%F %a %u %g %n',path])
