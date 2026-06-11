import type { FastifyInstance, FastifyReply } from "fastify";

const dashboardHtml = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>ACBH 控制中心</title>
<style>
:root{--bg:#0a0f1e;--card:#111827;--border:rgba(148,163,184,.2);--text:#e5e7eb;--muted:#9ca3af;--accent:#6366f1;--green:#22c55e;--red:#ef4444}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:"PingFang SC","Microsoft YaHei","Noto Sans SC",system-ui,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;display:flex;flex-direction:column}
.panel{display:none}.panel.active{display:block}
button,input,select,textarea{font:inherit;border-radius:10px;border:1px solid var(--border);background:var(--card);color:var(--text);padding:9px 14px;font-size:14px}
button{cursor:pointer;background:var(--accent);border:0;font-weight:600;transition:opacity .2s}
button:hover{opacity:.85}
button.sec{background:var(--card);border:1px solid var(--border)}
button.sm{padding:5px 10px;font-size:12px;width:auto}
button.danger{background:var(--red)}
input,select,textarea{width:100%;margin-top:4px;margin-bottom:12px}
textarea{min-height:100px;font-family:ui-monospace,Consolas,monospace;font-size:12px;resize:vertical}
pre{white-space:pre-wrap;word-break:break-all;background:#020617;border-radius:10px;padding:12px;font-size:12px;max-height:300px;overflow:auto;color:#d1d5db}
label{display:block;font-size:13px;color:var(--muted);margin-bottom:2px}
h2{font-size:20px;margin-bottom:14px}h3{font-size:16px;margin-bottom:8px;color:var(--muted)}
.row{display:flex;gap:12px;flex-wrap:wrap}.row>*{flex:1;min-width:180px}

.header{display:flex;align-items:center;justify-content:space-between;padding:16px 24px;border-bottom:1px solid var(--border);flex-wrap:wrap;gap:12px}
.header h1{font-size:24px;background:linear-gradient(135deg,var(--accent),#a855f7);-webkit-background-clip:text;-webkit-text-fill-color:transparent}
.header .badge{padding:4px 12px;border-radius:999px;font-size:12px;font-weight:600}
.badge.ok{background:rgba(34,197,94,.15);color:var(--green)}
.badge.err{background:rgba(239,68,68,.15);color:var(--red)}
.header .slogan{font-size:13px;color:var(--muted)}

.tabs{display:flex;gap:0;border-bottom:1px solid var(--border);padding:0 16px;overflow-x:auto;flex-shrink:0}
.tab{padding:14px 20px;font-size:14px;cursor:pointer;border-bottom:3px solid transparent;color:var(--muted);white-space:nowrap;transition:.2s;background:none;border-radius:0;font-weight:500}
.tab:hover{color:var(--text)}
.tab.active{color:var(--text);border-bottom-color:var(--accent)}

.main{display:flex;flex:1;overflow:hidden}
.content{flex:1;padding:24px;overflow-y:auto;max-width:960px}
.side{width:260px;border-left:1px solid var(--border);padding:24px 16px;display:flex;flex-direction:column;gap:18px;overflow-y:auto;flex-shrink:0}
@media(max-width:900px){.main{flex-direction:column}.side{width:100%;border-left:0;border-top:1px solid var(--border);flex-direction:row;flex-wrap:wrap;gap:12px}}

.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:14px;margin-bottom:18px}
.card{border:1px solid var(--border);background:var(--card);border-radius:14px;padding:16px}
.card .val{font-size:22px;font-weight:700;margin-top:4px;word-break:break-all}
.card.green .val{color:var(--green)}
.card .lbl{font-size:12px;color:var(--muted)}
.card-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px}
.actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:14px}

.mascot{text-align:center;padding:10px}
.mascot svg{width:80px;height:80px;opacity:.7}
.mascot .name{font-size:14px;font-weight:700;margin-top:8px}
.mascot .sub{font-size:11px;color:var(--muted);margin-top:4px;line-height:1.5}

.warn{background:rgba(239,68,68,.08);border:1px solid rgba(239,68,68,.2);border-radius:10px;padding:12px;font-size:12px;color:var(--red);margin-bottom:12px;line-height:1.6}

.olist{counter-reset:step}
.olist li{counter-increment:step;margin:10px 0;font-size:13px;display:flex;align-items:flex-start;gap:8px;line-height:1.6}
.olist li::before{content:counter(step);display:inline-flex;align-items:center;justify-content:center;width:24px;height:24px;border-radius:50%;background:var(--accent);font-size:12px;font-weight:700;flex-shrink:0}
.olist li .text{flex:1}

.output-bar{background:var(--card);border:1px solid var(--border);border-radius:10px;padding:10px 16px;font-size:12px;color:var(--muted);margin-top:16px;display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.output-bar pre{flex:1;padding:0;background:none;font-size:12px;max-height:80px;margin:0}
</style>
</head>
<body>

<div class="header" id="top">
<div><h1>ACBH 控制中心</h1><span class="slogan">主机接管 · 制品同步 · 中继隧道</span></div>
<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
<span class="badge ok" id="healthBadge">检查中...</span>
<span style="font-size:12px;color:var(--muted)" id="statusText"></span>
</div>
</div>

<div class="tabs" id="tabBar">
<button class="tab active" data-tab="overview">\u603b\u89c8</button>
<button class="tab" data-tab="coordinator">Coordinator</button>
<button class="tab" data-tab="agent">Agent</button>
<button class="tab" data-tab="storage">Storage</button>
<button class="tab" data-tab="artifacts">\u5236\u54c1</button>
<button class="tab" data-tab="opencode">OpenCode</button>
<button class="tab" data-tab="checklist">\u68c0\u67e5\u6e05\u5355</button>
</div>

<div class="main">
<div class="content" id="contentArea">

<!-- \u603b\u89c8 -->
<div class="panel active" id="panel-overview">
<h2>\u603b\u89c8</h2>
<div class="cards" id="overviewCards">
<div class="card"><span class="lbl">Coordinator</span><div class="val" id="ovCoord">--</div></div>
<div class="card"><span class="lbl">\u7ec4 ID</span><div class="val" id="ovGroup">--</div></div>
<div class="card"><span class="lbl">\u4e3b\u673a ID</span><div class="val" id="ovHost">--</div></div>
<div class="card"><span class="lbl">Storage</span><div class="val" id="ovStorage">--</div></div>
<div class="card"><span class="lbl">\u6700\u65b0 world-snapshot</span><div class="val" id="ovWorld">--</div></div>
<div class="card"><span class="lbl">\u6700\u65b0 server-pack</span><div class="val" id="ovPack">--</div></div>
</div>
<h3>\u5feb\u6377\u64cd\u4f5c</h3>
<div class="actions">
<button onclick="refreshHealth()">\u5237\u65b0\u72b6\u6001</button>
<button class="sec" onclick="switchTab('coordinator');createGroup()">\u521b\u5efa\u7ec4</button>
<button class="sec" onclick="switchTab('agent')">\u751f\u6210 Agent \u767b\u5f55\u547d\u4ee4</button>
<button class="sec" onclick="switchTab('opencode')">\u751f\u6210 OpenCode \u6267\u884c\u63d0\u793a\u8bcd</button>
</div>
</div>

<!-- Coordinator -->
<div class="panel" id="panel-coordinator">
<h2>Coordinator</h2>
<label>\u534f\u8c03\u5668\u5730\u5740</label><input id="coordinatorUrl"/>
<button onclick="refreshHealth()">\u68c0\u67e5\u8fde\u63a5</button>
<button class="sec" onclick="refreshStorage()">\u5b58\u50a8\u72b6\u6001</button>
<pre id="healthOut"></pre>

<h3 style="margin-top:20px">\u521b\u5efa\u7ec4</h3>
<div class="row"><div><label>\u7ec4\u540d\u79f0</label><input id="groupName" value="Local Test"/></div><div><label>\u521b\u5efa\u8005\u540d\u79f0</label><input id="ownerName" value="Owner"/></div></div>
<button onclick="createGroup()">\u521b\u5efa\u7ec4</button>
<pre id="groupOut"></pre>

<h3 style="margin-top:20px">\u7ec4\u914d\u7f6e</h3>
<div class="row"><div><label>\u7ec4 ID</label><input id="groupId"/></div><div><label>Access Key</label><input id="accessKey"/></div></div>
<div class="row"><div><label>\u4e3b\u673a ID</label><input id="hostId" placeholder="Agent \u767b\u5f55\u540e\u81ea\u52a8\u586b\u5145"/></div><div><label>\u4e3b\u673a\u4ee4\u724c (Host Token)</label><input id="hostToken" placeholder="\u4ece Agent config.yaml \u590d\u5236"/></div></div>
<div class="warn"><strong>\u8b66\u544a</strong>\uff1a\u4ec5\u5728\u53ef\u4fe1\u672c\u673a\u4f7f\u7528\u3002\u4e0d\u8981\u5728\u516c\u5171\u7535\u8111\u4fdd\u5b58 accessKey \u6216 hostToken\u3002</div>
<div class="actions">
<button onclick="saveLocal()">\u4fdd\u5b58\u672c\u5730</button>
<button class="sec" onclick="loadState()">\u52a0\u8f7d\u7ec4\u72b6\u6001</button>
</div>
</div>

<!-- Agent -->
<div class="panel" id="panel-agent">
<h2>Agent \u5de5\u4f5c\u6d41</h2>

<div class="card" style="margin-bottom:16px">
<h3>Agent \u672c\u5730\u63a7\u5236</h3>
<div class="row"><div><label>Agent \u672c\u5730 API \u5730\u5740</label><input id="agentApiUrl" value="http://127.0.0.1:6122"/></div><div><label>\u672c\u5730\u63a7\u5236\u4ee4\u724c</label><input id="agentToken" placeholder="\u4ece acbh-agent control serve \u590d\u5236"/></div></div>
<div class="actions">
<button onclick="connectAgent()">\u8fde\u63a5\u672c\u673a Agent</button>
<button class="sec" onclick="disconnectAgent()">\u65ad\u5f00</button>
</div>
<div id="agentMode" style="margin-top:8px;font-size:13px;color:var(--muted)">\u547d\u4ee4\u6a21\u5f0f\uff1a\u672a\u8fde\u63a5\u672c\u673a Agent\uff0c\u4ec5\u751f\u6210\u547d\u4ee4</div>
<hr style="border-color:var(--border);margin:14px 0">

<h3>\u672c\u5730\u64cd\u4f5c</h3>
<div class="actions">
<button onclick="agentDoctor()">\u8fd0\u884c doctor</button>
<button class="sec" onclick="agentScan()">\u626b\u63cf server-pack</button>
<button class="sec" onclick="agentSafeSync()">safe-sync world</button>
<button class="sec" onclick="agentPush('server-pack')">push server-pack</button>
<button class="sec" onclick="agentPush('world')">push world</button>
<button class="sec" onclick="agentPull('server-pack')">pull server-pack</button>
<button class="sec" onclick="agentPull('world')">pull world</button>
</div>
</div>

<hr style="border-color:var(--border);margin:14px 0">
<h3>\u547d\u4ee4\u751f\u6210\uff08\u5907\u7528\uff09</h3>
<p style="color:var(--muted);font-size:13px;margin-bottom:12px">\u590d\u5236\u5230\u672c\u673a\u7ec8\u7aef\u6216\u4ea4\u7ed9 OpenCode \u6267\u884c\u3002</p>

<h3>\u57fa\u672c\u914d\u7f6e</h3>
<div class="row"><div><label>\u5e73\u53f0</label><select id="platform"><option>windows</option><option>linux</option><option>darwin</option></select></div><div><label>Shell</label><select id="shellType"><option>powershell</option><option>bash</option></select></div></div>
<div class="row"><div><label>Agent \u53ef\u6267\u884c\u6587\u4ef6</label><input id="agentExe" value=".\\acbh-agent-windows-amd64.exe"/></div><div><label>\u663e\u793a\u540d\u79f0</label><input id="displayName" value="Windows Host"/></div></div>
<div class="row"><div><label>\u8bbe\u5907\u540d\u79f0</label><input id="deviceName" value="MSI"/></div><div><label>\u5de5\u4f5c\u533a\u76ee\u5f55</label><input id="workspaceDir" value="C:\\ACBH-Test"/></div></div>

<h3>\u670d\u52a1\u5668\u76ee\u5f55</h3>
<div class="row"><div><label>Server A \u76ee\u5f55 (\u4e3b\u673a)</label><input id="serverDir" value="C:\\ACBH-Test\\server-a"/></div><div><label>Server B \u76ee\u5f55 (\u6062\u590d\u76ee\u6807)</label><input id="serverBDir" value="C:\\ACBH-Test\\server-b"/></div></div>

<h3>RCON \u914d\u7f6e</h3>
<div class="row"><div><label>RCON Host</label><input id="rconHost" value="127.0.0.1"/></div><div><label>RCON Port</label><input id="rconPort" value="25575"/></div><div><label>RCON Password</label><input id="rconPassword" value="acbh-test"/></div></div>

<h3>\u5236\u54c1 ID</h3>
<div class="row"><div><label>server-pack ID</label><input id="serverPackId" value="win-server-pack-001"/></div><div><label>world-snapshot ID</label><input id="worldSnapshotId" value="win-world-safe-001"/></div></div>

<div class="actions">
<button onclick="genLogin()">\u767b\u5f55\u547d\u4ee4</button>
<button class="sec" onclick="genDoctor()">doctor \u547d\u4ee4</button>
<button class="sec" onclick="genScan()">\u626b\u63cf server-pack</button>
<button class="sec" onclick="genSafeSync()">safe-sync \u547d\u4ee4</button>
<button class="sec" onclick="genPush()">push \u547d\u4ee4</button>
<button class="sec" onclick="genPull()">pull \u547d\u4ee4</button>
<button onclick="genAll()">\u4e00\u952e\u751f\u6210\u5b8c\u6574\u6d41\u7a0b</button>
</div>
<textarea id="agentCommands" readonly></textarea>
<button class="sec" onclick="copyText('agentCommands')">\u590d\u5236</button>
</div>

<!-- Storage -->
<div class="panel" id="panel-storage">
<h2>Storage</h2>
<p style="color:var(--muted);font-size:13px;margin-bottom:16px">Storage \u4fdd\u5b58\u4e0a\u4f20\u7684 manifest \u548c\u5bf9\u8c61\u6587\u4ef6\uff0c\u7528\u4e8e\u4e3b\u673a\u63a5\u7ba1\u540e\u7684\u62c9\u53d6\u6062\u590d\u3002</p>
<div class="cards" id="storageCards">
<div class="card"><span class="lbl">\u5b58\u50a8\u540e\u7aef</span><div class="val" id="stBackend">--</div></div>
<div class="card"><span class="lbl">\u5b58\u50a8\u6839\u76ee\u5f55</span><div class="val" id="stRoot">--</div></div>
<div class="card"><span class="lbl">\u72b6\u6001</span><div class="val" id="stReady">--</div></div>
</div>
<button onclick="refreshStoragePanel()">\u5237\u65b0\u5b58\u50a8\u72b6\u6001</button>
<pre id="storageOut" style="margin-top:12px"></pre>
</div>

<!-- \u5236\u54c1 -->
<div class="panel" id="panel-artifacts">
<h2>\u5236\u54c1\u7ba1\u7406</h2>
<div class="row"><div><label>\u5236\u54c1\u7c7b\u578b</label><select id="artifactKind"><option>server-pack</option><option>world-snapshot</option><option>admin-state</option></select></div><div><label>\u5236\u54c1 ID</label><input id="artifactId"/></div></div>
<div class="actions">
<button onclick="loadArtifacts()">\u5236\u54c1\u5217\u8868</button>
<button class="sec" onclick="loadLatest()">\u6700\u65b0\u5236\u54c1</button>
<button class="sec" onclick="loadManifest()">Manifest \u5185\u5bb9</button>
</div>
<pre id="artifactOut" style="margin-top:12px"></pre>
</div>

<!-- OpenCode -->
<div class="panel" id="panel-opencode">
<h2>OpenCode \u4ea4\u63a5</h2>
<p style="color:var(--muted);font-size:13px;margin-bottom:12px">\u751f\u6210\u4e00\u4e2a\u5b8c\u6574\u7684\u6267\u884c\u63d0\u793a\u8bcd\uff0c\u7c98\u8d34\u5230 OpenCode \u4e2d\u5373\u53ef\u81ea\u52a8\u6267\u884c\u3002</p>
<label>\u5de5\u4f5c\u533a\u76ee\u5f55</label><input id="ocWorkspace" value="C:\\ACBH-Test"/>
<button onclick="genOpenCode()">\u751f\u6210 OpenCode \u63d0\u793a\u8bcd</button>
<textarea id="opencodePrompt" readonly style="min-height:200px"></textarea>
<button class="sec" onclick="copyText('opencodePrompt')">\u590d\u5236</button>
</div>

<!-- \u68c0\u67e5\u6e05\u5355 -->
<div class="panel" id="panel-checklist">
<h2>\u68c0\u67e5\u6e05\u5355</h2>
<p style="color:var(--muted);font-size:13px;margin-bottom:16px">\u6309\u6b65\u9aa4\u9010\u9879\u5b8c\u6210 ACBH \u5b8c\u6574\u5de5\u4f5c\u6d41\u3002</p>
<ol class="olist">
<li><span class="text"><strong>\u542f\u52a8 Coordinator</strong><br><code>pnpm dev:coordinator</code></span></li>
<li><span class="text"><strong>\u6253\u5f00 Dashboard</strong><br>\u786e\u8ba4 Coordinator \u72b6\u6001\u4e3a\u7eff\u8272</span><button class="sm sec" onclick="refreshHealth()" style="margin-top:4px">\u68c0\u67e5</button></li>
<li><span class="text"><strong>\u521b\u5efa\u7ec4</strong><br>\u5728 Coordinator \u680f\u70b9\u51fb\u201c\u521b\u5efa\u7ec4\u201d</span><button class="sm sec" onclick="switchTab('coordinator');createGroup()" style="margin-top:4px">\u8df3\u8f6c</button></li>
<li><span class="text"><strong>\u5728 Agent \u673a\u5668\u767b\u5f55</strong><br>\u8fd0\u884c\u751f\u6210\u7684\u767b\u5f55\u547d\u4ee4</span><button class="sm sec" onclick="switchTab('agent')" style="margin-top:4px">\u8df3\u8f6c</button></li>
<li><span class="text"><strong>doctor \u68c0\u67e5\u73af\u5883</strong><br>\u786e\u8ba4 Java \u548c\u670d\u52a1\u5668\u76ee\u5f55\u53ef\u7528</span></li>
<li><span class="text"><strong>\u542f\u52a8 Minecraft \u670d\u52a1\u7aef</strong><br>\u786e\u4fdd RCON \u5df2\u542f\u7528 (server.properties: enable-rcon=true)</span></li>
<li><span class="text"><strong>\u626b\u63cf server-pack</strong><br>\u751f\u6210\u670d\u52a1\u5668\u5305\u6e05\u5355</span><button class="sm sec" onclick="switchTab('agent')" style="margin-top:4px">\u8df3\u8f6c</button></li>
<li><span class="text"><strong>safe-sync world</strong><br>RCON save-all flush \u540e\u626b\u63cf\u4e16\u754c\u5feb\u7167</span></li>
<li><span class="text"><strong>push \u4e0a\u4f20</strong><br>\u4e0a\u4f20 server-pack \u548c world-snapshot</span></li>
<li><span class="text"><strong>pull \u5230 server-b</strong><br>\u62c9\u53d6\u5236\u54c1\u5230\u6062\u590d\u76ee\u5f55</span></li>
<li><span class="text"><strong>\u626b\u63cf\u6062\u590d\u76ee\u5f55</strong><br><code>acbh-agent scan --server-dir &lt;server-b&gt; --output restore.manifest.json</code></span></li>
<li><span class="text"><strong>validate manifest</strong><br><code>acbh-agent manifest validate --file restore.manifest.json</code></span></li>
<li><span class="text"><strong>\u542f\u52a8\u6062\u590d\u540e\u7684\u670d\u52a1\u7aef\u9a8c\u8bc1</strong><br>\u7528\u5ba2\u6237\u7aef\u8fde\u63a5 server-b \u9a8c\u8bc1\u4e16\u754c\u5b8c\u6574\u6027</span></li>
</ol>
</div>

</div>

<div class="side" id="mascotCard">
<div class="mascot">
<svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
<circle cx="50" cy="55" r="35" fill="none" stroke="#6366f1" stroke-width="2.5"/>
<circle cx="35" cy="42" r="4" fill="#6366f1"/>
<circle cx="65" cy="42" r="4" fill="#6366f1"/>
<path d="M40 62 Q50 72 60 62" fill="none" stroke="#6366f1" stroke-width="2.5" stroke-linecap="round"/>
<path d="M50 20 L42 4" stroke="#6366f1" stroke-width="2" stroke-linecap="round"/>
<path d="M50 20 L58 4" stroke="#6366f1" stroke-width="2" stroke-linecap="round"/>
<polygon points="42,4 38,10 46,12" fill="#6366f1"/>
<polygon points="58,4 62,10 54,12" fill="#6366f1"/>
</svg>
<div class="name">ACBH \u770b\u677f\u52a9\u624b</div>
<div class="sub">\u6211\u4f1a\u5e2e\u4f60\u68c0\u67e5\u4e3b\u673a\u3001\u540c\u6b65\u548c\u63a5\u7ba1\u6d41\u7a0b\u3002</div>
</div>
<div class="output-bar" id="quickOutput">
<pre id="quickMsg">\u5c31\u7eea\u3002\u70b9\u51fb\u4e0a\u65b9\u680f\u5361\u5f00\u59cb\u64cd\u4f5c\u3002</pre>
</div>
</div>
</div>

<script>
var $=function(id){return document.getElementById(id);};
var keys=["coordinatorUrl","groupId","accessKey","hostId","hostToken","displayName","deviceName","platform","shellType","agentExe","serverDir","serverBDir","serverPackId","worldSnapshotId","workspaceDir","rconHost","rconPort","rconPassword","ocWorkspace","agentApiUrl","agentToken"];

function setMsg(v){$("quickMsg").textContent=v;}
function baseUrl(){return $("coordinatorUrl").value.replace(/\\/$/,"");}
function errMsg(e){return e.message||String(e);}

function saveLocal(){
  for(var i=0;i<keys.length;i++){
    var k=keys[i],el=$(k);
    if(el&&el.value!==undefined)localStorage.setItem("acbh."+k,el.value);
  }
  setMsg("\u5df2\u4fdd\u5b58\u672c\u5730\u8bbe\u7f6e\u3002");
}

function restoreLocal(){
  $("coordinatorUrl").value=localStorage.getItem("acbh.coordinatorUrl")||location.origin;
  for(var i=1;i<keys.length;i++){
    var v=localStorage.getItem("acbh."+keys[i]),el=$(keys[i]);
    if(v&&el)el.value=v;
  }
}

async function api(path,options){
  options=options||{};
  var h=Object.assign({"content-type":"application/json"},options.headers||{});
  var res=await fetch(baseUrl()+path,Object.assign({},options,{headers:h}));
  var t=await res.text();
  try{var body=JSON.parse(t);}catch(e){body=t;}
  if(!res.ok)throw new Error(res.status+" "+res.statusText+": "+(typeof body==="string"?body:JSON.stringify(body)));
  return body;
}

async function refreshHealth(){
  try{
    var b=await api("/health");
    $("healthBadge").textContent="\u5728\u7ebf";$("healthBadge").className="badge ok";
    $("statusText").textContent="Coordinator OK";
    $("healthOut").textContent=JSON.stringify(b,null,2);
    $("ovCoord").textContent="\u5728\u7ebf";$("ovCoord").parentElement.className="card green";
  }catch(e){
    $("healthBadge").textContent="\u79bb\u7ebf";$("healthBadge").className="badge err";
    $("statusText").textContent=errMsg(e);
    $("ovCoord").textContent="\u79bb\u7ebf";
  }
}

async function refreshStorage(){
  try{var b=await api("/v1/storage/info");$("healthOut").textContent=JSON.stringify(b,null,2);setMsg("\u5b58\u50a8\u72b6\u6001\u5df2\u5237\u65b0");}catch(e){$("healthOut").textContent=errMsg(e);}
}

async function refreshStoragePanel(){
  try{
    var b=await api("/v1/storage/info");
    $("storageOut").textContent=JSON.stringify(b,null,2);
    $("stBackend").textContent=b.backend||"--";$("stRoot").textContent=b.root||"--";$("stReady").textContent=b.ready?"\u5c31\u7eea":"--";$("stReady").parentElement.className="card"+(b.ready?" green":"");
    $("ovStorage").textContent=b.ready?"\u5c31\u7eea":"--";
    setMsg("\u5b58\u50a8\u72b6\u6001\u5df2\u5237\u65b0");
  }catch(e){setMsg(errMsg(e));}
}

async function createGroup(){
  try{
    var b=await api("/v1/groups",{method:"POST",body:JSON.stringify({name:$("groupName").value,ownerName:$("ownerName").value})});
    $("groupId").value=b.groupId;$("accessKey").value=b.accessKey;
    $("groupOut").textContent=JSON.stringify(b,null,2);
    $("ovGroup").textContent=b.groupId;$("ovHost").textContent="--";
    saveLocal();setMsg("\u7ec4\u5df2\u521b\u5efa: "+b.groupId);
  }catch(e){$("groupOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function loadState(){
  try{
    var gid=$("groupId").value;
    var b=await api("/v1/groups/"+encodeURIComponent(gid)+"/state");
    $("ovGroup").textContent=gid;
    if(b.currentHostId){$("ovHost").textContent=b.currentHostId;}
    setMsg("\u7ec4\u72b6\u6001\u5df2\u52a0\u8f7d");
  }catch(e){setMsg(errMsg(e));}
}

function hostHeaders(){
  var h={},hid=$("hostId").value,htk=$("hostToken").value;
  if(hid)h["x-acbh-host-id"]=hid;if(htk)h["x-acbh-host-token"]=htk;return h;
}

async function loadArtifacts(){
  try{
    var b=await api("/v1/groups/"+encodeURIComponent($("groupId").value)+"/artifacts",{headers:hostHeaders()});
    $("artifactOut").textContent=JSON.stringify(b,null,2);
    if(b.worldSnapshot&&b.worldSnapshot.length){var w=b.worldSnapshot[b.worldSnapshot.length-1];$("ovWorld").textContent=w.artifactId||"--";}
    if(b.serverPack&&b.serverPack.length){var p=b.serverPack[b.serverPack.length-1];$("ovPack").textContent=p.artifactId||"--";}
    setMsg("\u5236\u54c1\u5217\u8868\u5df2\u52a0\u8f7d");
  }catch(e){$("artifactOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function loadLatest(){
  try{
    var k=encodeURIComponent($("artifactKind").value);
    var b=await api("/v1/groups/"+encodeURIComponent($("groupId").value)+"/artifacts/latest?artifactKind="+k,{headers:hostHeaders()});
    $("artifactOut").textContent=JSON.stringify(b,null,2);setMsg("\u6700\u65b0\u5236\u54c1\u5df2\u52a0\u8f7d");
  }catch(e){$("artifactOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function loadManifest(){
  try{
    var g=encodeURIComponent($("groupId").value),k=encodeURIComponent($("artifactKind").value),a=encodeURIComponent($("artifactId").value);
    var b=await api("/v1/groups/"+g+"/artifacts/"+k+"/"+a+"/manifest",{headers:hostHeaders()});
    $("artifactOut").textContent=JSON.stringify(b,null,2);setMsg("Manifest \u5df2\u52a0\u8f7d");
  }catch(e){$("artifactOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

// Shell helpers
function exeName(){return $("agentExe").value||($("platform").value==="windows"?".\\acbh-agent-windows-amd64.exe":"./acbh-agent");}
function isBash(){return $("shellType").value==="bash";}
function shellCont(){var nl=String.fromCharCode(10);return isBash()?" \\"+nl+"  ":" "+String.fromCharCode(96)+nl+"  ";}
function shellJoin(lines){return lines.join(shellCont());}

function genLogin(){
  var e=exeName();var cmd=shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+$("groupId").value,"--access-key "+$("accessKey").value,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]);
  $("agentCommands").value=cmd;saveLocal();switchTab("agent");
}

function genDoctor(){
  $("agentCommands").value=exeName()+" doctor";switchTab("agent");
}

function genScan(){
  var e=exeName(),dir=$("serverDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value;
  var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json";
  $("agentCommands").value=shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]);
  switchTab("agent");
}

function genSafeSync(){
  var e=exeName(),dir=$("serverDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",world=$("worldSnapshotId").value,pack=$("serverPackId").value;
  var sep=isBash()?"/":"\\";var wm=dir+sep+"world.manifest.json";
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575",rpw=$("rconPassword").value||"acbh-test";
  $("agentCommands").value=shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--rcon-password "+rpw,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]);
  switchTab("agent");
}

function genPush(){
  var e=exeName(),dir=$("serverDir").value;
  var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  $("agentCommands").value=[e+" push --server-dir "+dir+" --manifest "+sp,e+" push --server-dir "+dir+" --manifest "+wm].join("\n\n");
  switchTab("agent");
}

function genPull(){
  var e=exeName(),bdir=$("serverBDir").value,pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  $("agentCommands").value=[e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir].join("\n\n");
  switchTab("agent");
}

function genAll(){
  var e=exeName(),dir=$("serverDir").value,bdir=$("serverBDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575",rpw=$("rconPassword").value||"acbh-test";
  var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  $("agentCommands").value=[
    shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+g,"--access-key "+$("accessKey").value,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]),
    e+" doctor",
    shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]),
    shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--rcon-password "+rpw,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]),
    e+" push --server-dir "+dir+" --manifest "+sp,
    e+" push --server-dir "+dir+" --manifest "+wm,
    e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,
    e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir
  ].join("\n\n");
  switchTab("agent");
}

function genOpenCode(){
  var sh=$("shellType").value,platform=$("platform").value;
  var lines=[
    "\u4f60\u662f\u5728\u672c\u5730\u673a\u5668\u4e0a\u8fd0\u884c\u7684 OpenCode\u3002",
    "",
    "\u76ee\u6807\uff1a\u6267\u884c\u5b8c\u6574\u7684 ACBH Agent \u5de5\u4f5c\u6d41\u5e76\u62a5\u544a\u7ed3\u679c\u3002",
    "",
    "\u73af\u5883\u4fe1\u606f\uff1a",
    "- \u5de5\u4f5c\u533a: "+$("ocWorkspace").value,
    "- Coordinator: "+baseUrl(),
    "- \u7ec4 ID: "+$("groupId").value,
    "- \u4e3b\u673a ID: "+($("hostId").value||"<host-id>"),
    "- \u5e73\u53f0: "+platform,
    "- Shell: "+sh,
    "",
    "\u89c4\u5219\uff1a",
    "- \u4e0d\u8981\u4fee\u6539\u6e90\u7801\uff0c\u9664\u975e\u7528\u6237\u660e\u786e\u8981\u6c42\u4ee3\u7801\u53d8\u66f4\u3002",
    "- \u6309\u987a\u5e8f\u6267\u884c\u547d\u4ee4\u3002",
    "- \u9047\u5230\u7b2c\u4e00\u4e2a\u9519\u8bef\u7acb\u523b\u505c\u6b62\u3002\u8f93\u51fa\u5931\u8d25\u547d\u4ee4\u3001stdout\u3001stderr\u3002",
    "- \u6210\u529f\u540e\u603b\u7ed3 included / ignored / unknown / deleted \u6587\u4ef6\u6570\u3002",
    "- \u603b\u7ed3 manifest validate\u3001push\u3001pull\u3001restore scan \u662f\u5426\u6210\u529f\u3002",
    "",
    "\u547d\u4ee4\uff1a",
    ""
  ];
  var cmd=getAllCommands();
  lines.push(cmd);
  $("opencodePrompt").value=lines.join("\n");
  saveLocal();switchTab("opencode");
}

function getAllCommands(){
  var e=exeName(),dir=$("serverDir").value,bdir=$("serverBDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575",rpw=$("rconPassword").value||"acbh-test";
  var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  return [
    shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+g,"--access-key "+$("accessKey").value,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]),
    e+" doctor",
    shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]),
    shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--rcon-password "+rpw,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]),
    e+" push --server-dir "+dir+" --manifest "+sp,
    e+" push --server-dir "+dir+" --manifest "+wm,
    e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,
    e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir
  ].join("\n\n");
}

var agentConnected=false;

function setAgentMode(connected){
  agentConnected=connected;
  var m=$("agentMode");
  if(connected){
    m.innerHTML='<span style="color:'+String.fromCharCode(35)+'86efac">\u2714 \u672c\u5730\u63a7\u5236\u6a21\u5f0f\uff1a\u5df2\u8fde\u63a5 Agent</span>';
  }else{
    m.innerHTML='<span style="color:'+String.fromCharCode(35)+'fbbf24">\u26a0 \u547d\u4ee4\u6a21\u5f0f\uff1a\u672a\u8fde\u63a5\u672c\u673a Agent\uff0c\u4ec5\u751f\u6210\u547d\u4ee4</span>';
  }
}

async function connectAgent(){
  var url=$("agentApiUrl").value.replace(/\\/$/,"");
  var tok=$("agentToken").value;
  if(!tok){setMsg("\u8bf7\u5148\u8f93\u5165\u672c\u5730\u63a7\u5236\u4ee4\u724c");return;}
  try{
    var r=await fetch(url+"/health");
    var b=await r.json();
    if(b.ok){
      setMsg("\u5df2\u8fde\u63a5 Agent: "+b.platform+" PID "+b.pid);
      setAgentMode(true);saveLocal();
    }else{
      setMsg("\u8fde\u63a5\u5931\u8d25: Agent health check returned ok=false");
    }
  }catch(e){setMsg("\u8fde\u63a5\u5931\u8d25: "+errMsg(e));setAgentMode(false);}
}

function disconnectAgent(){setAgentMode(false);setMsg("\u5df2\u65ad\u5f00 Agent \u8fde\u63a5\uff0c\u5207\u6362\u5230\u547d\u4ee4\u6a21\u5f0f");}

async function agentCall(path,body){
  var url=$("agentApiUrl").value.replace(/\\/$/,"");
  var tok=$("agentToken").value;
  var r=await fetch(url+path,{method:"POST",headers:{"Content-Type":"application/json","Authorization":"Bearer "+tok},body:JSON.stringify(body)});
  var t=await r.text();var b;
  try{b=JSON.parse(t);}catch(e){b={ok:false,error:t};}
  if(!r.ok&&!b.error)b.error=r.status+" "+r.statusText;
  return b;
}

async function agentDoctor(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("\u8fd0\u884c doctor...");
  try{
    var b=await agentCall("/v1/doctor",{serverDir:$("serverDir").value});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"doctor \u5b8c\u6210":"doctor \u5931\u8d25");
  }catch(e){setMsg(errMsg(e));}
}

async function agentScan(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("\u626b\u63cf server-pack...");
  try{
    var g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,dir=$("serverDir").value;
    var sep=isBash()?"/":"\\";var sp=dir+sep+"server-pack.manifest.json";
    var b=await agentCall("/v1/scan",{serverDir:dir,artifactKind:"server-pack",artifactId:pack,groupId:g,creatorHostId:h,output:sp});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"\u626b\u63cf\u5b8c\u6210: "+JSON.stringify(b.summary):"\u626b\u63cf\u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentSafeSync(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("safe-sync world...");
  try{
    var g=$("groupId").value,h=$("hostId").value||"<host-id>",world=$("worldSnapshotId").value,pack=$("serverPackId").value,dir=$("serverDir").value;
    var sep=isBash()?"/":"\\";var wm=dir+sep+"world.manifest.json";
    var rh=$("rconHost").value||"127.0.0.1",rp=parseInt($("rconPort").value)||25575,rpw=$("rconPassword").value||"acbh-test";
    var b=await agentCall("/v1/safe-sync",{serverDir:dir,rconHost:rh,rconPort:rp,rconPassword:rpw,artifactId:world,groupId:g,creatorHostId:h,serverPackVersion:pack,output:wm});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"safe-sync \u5b8c\u6210: "+JSON.stringify(b.summary):"safe-sync \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentPush(which){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("push "+which+"...");
  try{
    var dir=$("serverDir").value,sep=isBash()?"/":"\\";
    var mf=dir+sep+(which==="world"?"world":"server-pack")+".manifest.json";
    var b=await agentCall("/v1/push",{coordinatorUrl:baseUrl(),serverDir:dir,manifest:mf});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"push "+which+" \u5b8c\u6210":"push \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentPull(which){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("pull "+which+"...");
  try{
    var bdir=$("serverBDir").value,pack=$("serverPackId").value,world=$("worldSnapshotId").value;
    var aid=which==="world"?world:pack;
    var b=await agentCall("/v1/pull",{coordinatorUrl:baseUrl(),artifactKind:which==="world"?"world-snapshot":"server-pack",artifactId:aid,outputDir:bdir});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"pull "+which+" \u5b8c\u6210":"pull \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

function switchTab(name){
  var tabs=document.querySelectorAll(".tab");
  for(var i=0;i<tabs.length;i++){tabs[i].classList.remove("active");if(tabs[i].dataset.tab===name)tabs[i].classList.add("active");}
  var panels=document.querySelectorAll(".panel");
  for(var j=0;j<panels.length;j++)panels[j].classList.remove("active");
  var panel=$("panel-"+name);if(panel)panel.classList.add("active");
  saveLocal();
}

async function copyText(id){await navigator.clipboard.writeText($(id).value);setMsg("\u5df2\u590d\u5236\u5230\u526a\u8d34\u677f\u3002");}

document.getElementById("tabBar").addEventListener("click",function(e){
  var tab=e.target.closest(".tab");if(!tab)return;switchTab(tab.dataset.tab);
});

restoreLocal();refreshHealth();refreshStoragePanel();
</script>
</body>
</html>`;

export async function registerDashboardRoutes(app: FastifyInstance): Promise<void> {
  app.get("/dashboard", async (_request, reply: FastifyReply) => {
    reply.type("text/html; charset=utf-8");
    return dashboardHtml;
  });
}
