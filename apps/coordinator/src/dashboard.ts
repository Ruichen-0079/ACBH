import type { FastifyInstance, FastifyReply } from "fastify";

const dashboardHtml = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>ACBH Dashboard</title>
<style>
:root{color-scheme:dark;font-family:Inter,system-ui,sans-serif;background:#0b1020;color:#e5e7eb}
body{margin:0;min-height:100vh;background:radial-gradient(circle at top left,#1f2a44,#0b1020 40rem)}
header{padding:28px 28px 10px}h1{margin:0;font-size:32px}p{color:#9ca3af}
main{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:16px;padding:16px 28px 32px}
section{border:1px solid rgba(148,163,184,.22);background:rgba(15,23,42,.78);border-radius:18px;padding:18px}
label{display:block;margin:10px 0 6px;color:#cbd5e1;font-size:13px}
input,select,textarea,button{width:100%;border-radius:12px;border:1px solid rgba(148,163,184,.28);background:#111827;color:#f9fafb;padding:10px 12px;font:inherit}
textarea{min-height:120px;font-family:ui-monospace,Consolas,monospace;font-size:12px}
button{margin-top:12px;background:linear-gradient(135deg,#2563eb,#7c3aed);border:0;cursor:pointer;font-weight:700}
.secondary{background:#1f2937;border:1px solid rgba(148,163,184,.28)}
pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#020617;border-radius:12px;padding:12px;color:#d1d5db;font-size:12px;max-height:360px;overflow:auto}
.full{grid-column:1/-1}.pill{display:inline-block;padding:6px 10px;border-radius:999px;background:rgba(34,197,94,.12);color:#86efac;font-size:13px}
.checklist{list-style:none;padding:0}.checklist li{margin:6px 0;font-size:13px;color:#cbd5e1}.checklist code{background:#020617;padding:2px 6px;border-radius:6px;font-size:12px;color:#86efac}
</style>
</head>
<body>
<header><div class="pill" id="health">Checking coordinator...</div><h1>ACBH Dashboard</h1><p>Quick setup, group state, artifact visibility, OpenCode handoff, and copy-paste Agent commands.</p></header>
<main>
<section><h2>Coordinator</h2><label>Coordinator URL</label><input id="coordinatorUrl"/><button onclick="refreshHealth()">Refresh health</button><button class="secondary" onclick="refreshStorage()">Refresh storage</button><pre id="healthOut"></pre></section>
<section><h2>Create group</h2><label>Group name</label><input id="groupName" value="Local Test"/><label>Owner name</label><input id="ownerName" value="Owner"/><button onclick="createGroup()">Create group</button><pre id="groupOut"></pre></section>
<section><h2>Group config</h2><label>Group ID</label><input id="groupId"/><label>Access key</label><input id="accessKey"/><label>Host ID</label><input id="hostId" placeholder="Filled after agent login"/><label>Host token</label><input id="hostToken" placeholder="From agent config.yaml"/><button onclick="saveLocal()">Save locally</button><button class="secondary" onclick="loadState()">Load group state</button></section>
<section><h2>Agent login command</h2><label>Display name</label><input id="displayName" value="Windows Host"/><label>Device name</label><input id="deviceName" value="MSI"/><label>Platform</label><select id="platform"><option>windows</option><option>linux</option><option>darwin</option></select><label>Shell type</label><select id="shellType"><option>powershell</option><option>bash</option></select><button onclick="generateLogin()">Generate login command</button><textarea id="loginCmd" readonly></textarea><button class="secondary" onclick="copyText('loginCmd')">Copy</button></section>
<section><h2>Command generator</h2><label>Server directory</label><input id="serverDir" value="C:\\ACBH-Test\\server-a"/><label>Server B directory (pull target)</label><input id="serverBDir" value="C:\\ACBH-Test\\server-b"/><label>Server pack artifact ID</label><input id="serverPackId" value="win-server-pack-001"/><label>World snapshot artifact ID</label><input id="worldSnapshotId" value="win-world-safe-001"/><button onclick="generateCommands()">Generate commands</button><textarea id="commands" readonly></textarea><button class="secondary" onclick="copyText('commands')">Copy</button></section>
<section><h2>OpenCode handoff</h2><p>Generate a task prompt for OpenCode so it can run Agent commands and report logs.</p><label>Workspace directory</label><input id="workspaceDir" value="C:\\ACBH-Test"/><button onclick="generateOpenCodePrompt()">Generate OpenCode prompt</button><textarea id="opencodePrompt" readonly></textarea><button class="secondary" onclick="copyText('opencodePrompt')">Copy</button></section>
<section><h2>External smoke test checklist</h2><ol class="checklist"><li>1. <button class="secondary" style="width:auto" onclick="createGroup()">Create group</button> &mdash; fills Group ID and Access key above</li><li>2. <button class="secondary" style="width:auto" onclick="generateLogin()">Generate login</button> &mdash; run the generated login command on Host A</li><li>3. <code>acbh-agent doctor</code> &mdash; verify Java and server directory</li><li>4. <button class="secondary" style="width:auto" onclick="switchTo('commands')">Scan server-pack</button> &mdash; run the scan command below</li><li>5. <button class="secondary" style="width:auto" onclick="switchTo('commands')">Safe-sync world</button> &mdash; requires RCON on 127.0.0.1:25575</li><li>6. <button class="secondary" style="width:auto" onclick="switchTo('commands')">Push both manifests</button> &mdash; run both push commands</li><li>7. <code>acbh-agent pull ... --output-dir &lt;server-b&gt;</code> &mdash; pull to server-b</li><li>8. <code>acbh-agent scan ... --server-dir &lt;server-b&gt; --output restore.manifest.json</code></li><li>9. <code>acbh-agent manifest validate --file restore.manifest.json</code></li></ol></section>
<section><h2>Artifacts</h2><label>Artifact kind</label><select id="artifactKind"><option>server-pack</option><option>world-snapshot</option><option>admin-state</option></select><label>Artifact ID</label><input id="artifactId"/><button onclick="loadArtifacts()">List artifacts</button><button class="secondary" onclick="loadLatest()">Load latest</button><button class="secondary" onclick="loadManifest()">Load manifest</button></section>
<section class="full"><h2>Output</h2><pre id="output">Ready.</pre></section>
</main>
<script>
var $=function(id){return document.getElementById(id);};
var keys=["coordinatorUrl","groupId","accessKey","hostId","hostToken","displayName","deviceName","platform","shellType","serverDir","serverBDir","serverPackId","worldSnapshotId","workspaceDir"];
function baseUrl(){return $("coordinatorUrl").value.replace(/\\/$/,"");}
function setOutput(v){$("output").textContent=typeof v==="string"?v:JSON.stringify(v,null,2);}
function saveLocal(){for(var i=0;i<keys.length;i++)localStorage.setItem("acbh."+keys[i],$(keys[i]).value);setOutput("Saved local dashboard settings.");}
function restoreLocal(){$("coordinatorUrl").value=localStorage.getItem("acbh.coordinatorUrl")||location.origin;for(var i=1;i<keys.length;i++){var v=localStorage.getItem("acbh."+keys[i]);if(v)$(keys[i]).value=v;}}
function errMsg(e){return e.message||String(e);}
async function api(path,options){options=options||{};var headers=Object.assign({"content-type":"application/json"},options.headers||{});var res=await fetch(baseUrl()+path,Object.assign({},options,{headers:headers}));var text=await res.text();var body;try{body=JSON.parse(text);}catch(e){body=text;}if(!res.ok)throw new Error(String(res.status)+" "+res.statusText+": "+(typeof body==="string"?body:JSON.stringify(body)));return body;}
async function refreshHealth(){try{var b=await api("/health");$("health").textContent="Coordinator OK";$("healthOut").textContent=JSON.stringify(b,null,2);}catch(e){$("health").textContent="Coordinator error";$("healthOut").textContent=errMsg(e);}}
async function refreshStorage(){try{setOutput(await api("/v1/storage/info"));}catch(e){setOutput(errMsg(e));}}
async function createGroup(){try{var b=await api("/v1/groups",{method:"POST",body:JSON.stringify({name:$("groupName").value,ownerName:$("ownerName").value})});$("groupId").value=b.groupId;$("accessKey").value=b.accessKey;$("groupOut").textContent=JSON.stringify(b,null,2);saveLocal();}catch(e){$("groupOut").textContent=errMsg(e);}}
async function loadState(){try{setOutput(await api("/v1/groups/"+encodeURIComponent($("groupId").value)+"/state"));}catch(e){setOutput(errMsg(e));}}
function hostHeaders(){var h={};var hid=$("hostId").value;var htk=$("hostToken").value;if(hid)h["x-acbh-host-id"]=hid;if(htk)h["x-acbh-host-token"]=htk;return h;}
async function loadArtifacts(){try{setOutput(await api("/v1/groups/"+encodeURIComponent($("groupId").value)+"/artifacts",{headers:hostHeaders()}));}catch(e){setOutput(errMsg(e));}}
async function loadLatest(){try{var k=encodeURIComponent($("artifactKind").value);setOutput(await api("/v1/groups/"+encodeURIComponent($("groupId").value)+"/artifacts/latest?artifactKind="+k,{headers:hostHeaders()}));}catch(e){setOutput(errMsg(e));}}
async function loadManifest(){try{var g=encodeURIComponent($("groupId").value),k=encodeURIComponent($("artifactKind").value),a=encodeURIComponent($("artifactId").value);setOutput(await api("/v1/groups/"+g+"/artifacts/"+k+"/"+a+"/manifest",{headers:hostHeaders()}));}catch(e){setOutput(errMsg(e));}}
function exeName(){return $("platform").value==="windows"?".\\acbh-agent-windows-amd64.exe":"./acbh-agent";}
function isBash(){return $("shellType").value==="bash";}
function shellCont(){var nl=String.fromCharCode(10);return isBash()?" \\"+nl+"  ":" "+String.fromCharCode(96)+nl+"  ";}
function shellJoin(lines){return lines.join(shellCont());}
function generateLogin(){var e=exeName();$("loginCmd").value=shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+$("groupId").value,"--access-key "+$("accessKey").value,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]);saveLocal();}
function generateCommands(){var e=exeName(),dir=$("serverDir").value,bdir=$("serverBDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,world=$("worldSnapshotId").value;var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";$("commands").value=[shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]),shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host 127.0.0.1","--rcon-port 25575","--rcon-password acbh-test","--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]),e+" push --server-dir "+dir+" --manifest "+sp,e+" push --server-dir "+dir+" --manifest "+wm,e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir].join("\n\n");saveLocal();}
function generateOpenCodePrompt(){if(!$("commands").value.trim())generateCommands();var sh=$("shellType").value;var lines=["You are OpenCode on this local machine.","","Goal: run the full ACBH Agent smoke workflow below and report results concisely.","","Context:","- Workspace: "+$("workspaceDir").value,"- Coordinator: "+baseUrl(),"- Group ID: "+$("groupId").value,"- Host ID: "+($("hostId").value||"<host-id>"),"- Shell: "+sh,"","Rules:","- Do NOT edit source code unless explicitly asked to make a code change.","- Run every command exactly as written.","- Stop immediately on the first error. Show the failing command and its full stdout+stderr.","- On success, report the result concisely.","","Reporting requirements:","- Which commands succeeded or failed.","- scan output: included / ignored / unknown / deleted file counts.","- manifest validate: pass or fail.","- push results: number of uploaded objects, coordinator status.","- pull results: number of written files, downloaded objects, total bytes restored.","","Commands ("+sh+"):","",$("commands").value];$("opencodePrompt").value=lines.join("\n");saveLocal();}
function switchTo(id){var el=document.getElementById(id);if(el)el.scrollIntoView({behavior:"smooth"});}
async function copyText(id){await navigator.clipboard.writeText($(id).value);setOutput("Copied.");}
restoreLocal();refreshHealth();
<\/script>
</body>
</html>`;

export async function registerDashboardRoutes(app: FastifyInstance): Promise<void> {
  app.get("/dashboard", async (_request, reply: FastifyReply) => {
    reply.type("text/html; charset=utf-8");
    return dashboardHtml;
  });
}
