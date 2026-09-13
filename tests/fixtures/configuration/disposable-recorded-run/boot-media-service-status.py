import json,subprocess
result={}
for unit in ('virmill-host-helper.service','virmill-host-helper.socket'):
 r=subprocess.run(['systemctl','is-enabled',unit],capture_output=True,text=True);state=r.stdout.strip();assert state in ('disabled','static'),(unit,state,r.stderr)
 a=subprocess.run(['systemctl','is-active',unit],capture_output=True,text=True);assert a.stdout.strip()=='inactive',(unit,a.stdout,a.stderr)
 result[unit]={'enabledState':state,'activeState':a.stdout.strip()}
r=subprocess.run(['systemctl','--user','is-enabled','virmilld.service'],capture_output=True,text=True);assert r.stdout.strip()=='disabled',r.stdout
result['virmilld.service']={'enabledState':r.stdout.strip()}
print(json.dumps({'observedServices':result,'transientTestCoordinator':subprocess.check_output(['systemctl','--user','show','<test-vm-login>-8a092b4.service','--property=MainPID,ActiveState'],text=True)}),flush=True)
