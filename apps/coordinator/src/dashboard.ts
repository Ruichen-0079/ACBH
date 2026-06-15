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
<button class="tab" data-tab="election">Election / Takeover</button>
<button class="tab" data-tab="events">Events</button>
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
<div class="row"><div><label>\u7ec4 ID</label><input id="groupId"/></div><div><label>Access Key</label><input id="accessKey" type="password" autocomplete="off"/><div class="actions"><button class="sm sec" onclick="toggleSecret('accessKey')">\u77ed\u6682\u663e\u793a</button><button class="sm sec" onclick="copySecret('accessKey')">\u590d\u5236</button></div></div></div>
<div class="row"><div><label>Member ID</label><input id="memberId" placeholder="\u521b\u5efa\u7ec4\u540e\u81ea\u52a8\u586b\u5145"/></div><div><label>\u4e3b\u673a ID</label><input id="hostId" placeholder="Agent \u767b\u5f55\u540e\u81ea\u52a8\u586b\u5145"/></div><div><label>\u4e3b\u673a\u4ee4\u724c (Host Token)</label><input id="hostToken" type="password" autocomplete="off" placeholder="\u4ece Agent config.yaml \u590d\u5236"/><div class="actions"><button class="sm sec" onclick="toggleSecret('hostToken')">\u77ed\u6682\u663e\u793a</button></div></div></div>
<div class="row"><div><label>Heartbeat \u72b6\u6001</label><select id="heartbeatStatus"><option>standby</option><option>online</option><option>hosting</option><option>unhealthy</option><option>offline</option></select></div><div><label>Agent Version</label><input id="agentVersion" value="0.1.0"/></div></div>
<div class="warn"><strong>\u8b66\u544a</strong>\uff1a\u4ec5\u5728\u53ef\u4fe1\u672c\u673a\u4f7f\u7528\u3002\u4e0d\u8981\u5728\u516c\u5171\u7535\u8111\u4fdd\u5b58 accessKey \u6216 hostToken\u3002</div>
<div class="actions">
<button onclick="saveLocal()">\u4fdd\u5b58\u672c\u5730</button>
<button class="danger" onclick="forgetSecrets()">\u6e05\u9664\u51ed\u636e</button>
<button class="sec" onclick="registerHost()">Register host</button>
<button class="sec" onclick="sendHeartbeat()">Send heartbeat</button>
<button class="sec" onclick="loadState()">\u52a0\u8f7d\u7ec4\u72b6\u6001</button>
</div>
<pre id="groupStateOut" style="margin-top:12px"></pre>
</div>

<!-- Agent -->
<div class="panel" id="panel-agent">
<h2>Agent \u5de5\u4f5c\u6d41</h2>

<div class="card" style="margin-bottom:16px">
<h3>Agent \u672c\u5730\u63a7\u5236</h3>
<div class="row"><div><label>Agent \u672c\u5730 API \u5730\u5740</label><input id="agentApiUrl" value="http://127.0.0.1:6122" oninput="checkLocalControlUrl()"/></div><div><label>\u672c\u5730\u63a7\u5236\u4ee4\u724c</label><input id="agentToken" type="password" autocomplete="off" placeholder="\u4ece control-token \u6587\u4ef6\u8bfb\u53d6"/><div class="actions"><button class="sm sec" onclick="toggleSecret('agentToken')">\u77ed\u6682\u663e\u793a</button></div></div></div>
<div class="warn" id="agentUrlWarning" style="display:none">\u8b66\u544a\uff1a\u8be5 Local Control \u5730\u5740\u4e0d\u662f loopback\u3002\u8fdc\u7a0b\u63a7\u5236\u4f1a\u6269\u5927\u51ed\u636e\u6cc4\u9732\u548c\u670d\u52a1\u5668\u64cd\u4f5c\u98ce\u9669\u3002</div>
<div class="actions">
<button onclick="connectAgent()">\u8fde\u63a5\u672c\u673a Agent</button>
<button class="sec" onclick="disconnectAgent()">\u65ad\u5f00</button>
</div>
<div id="agentMode" style="margin-top:8px;font-size:13px;color:var(--muted)">\u547d\u4ee4\u6a21\u5f0f\uff1a\u672a\u8fde\u63a5\u672c\u673a Agent\uff0c\u4ec5\u751f\u6210\u547d\u4ee4</div>
<hr style="border-color:var(--border);margin:14px 0">

<h3>\u672c\u5730\u64cd\u4f5c</h3>
<div class="actions">
<button onclick="agentDoctor()">\u8fd0\u884c doctor</button>
<button class="sec" onclick="agentScan('server-pack')">\u626b\u63cf server-pack</button>
<button class="sec" onclick="agentScan('server-runtime')">\u626b\u63cf server-runtime</button>
<button class="sec" onclick="agentValidateManifest()">Validate manifest</button>
<button class="sec" onclick="agentSafeSync()">safe-sync world</button>
<button class="sec" onclick="agentPush('server-pack')">push server-pack</button>
<button class="sec" onclick="agentPush('world')">push world</button>
<button class="sec" onclick="agentPush('server-runtime')">push server-runtime</button>
<button class="sec" onclick="agentPull('server-pack')">pull server-pack</button>
<button class="sec" onclick="agentPull('world')">pull world</button>
<button class="sec" onclick="agentPull('server-runtime')">pull latest server-runtime + verify</button>
</div>
<p style="color:var(--muted);font-size:12px;margin-top:10px">server-pack \u5305\u542b\u670d\u52a1\u7aef\u542f\u52a8\u6240\u9700\u6587\u4ef6\uff1aeula.txt &middot; server.properties &middot; mods &middot; config &middot; libraries &middot; .fabric/server &middot; \u9876\u5c42\u542f\u52a8 jar</p>
</div>

<div class="card" style="margin-bottom:16px">
<h3>\u670d\u52a1\u5668\u63a7\u5236</h3>
<div class="row"><div><label>\u670d\u52a1\u7aef\u76ee\u5f55</label><input id="srvDir" value=""/></div><div><label>\u542f\u52a8 Jar</label><input id="srvJar" value="fabric-server-launch.jar"/></div></div>
<div class="row"><div><label>Java \u8def\u5f84</label><input id="srvJava" value="java"/></div><div><label>JVM \u53c2\u6570</label><input id="srvJvmArgs" value="-Xmx2G -Xms1G"/></div></div>
<div class="row"><div><label>Server \u53c2\u6570</label><input id="srvArgs" value="nogui"/></div><div><label>RCON \u5bc6\u7801</label><input id="srvRconPassword" type="password"/></div></div>
<div class="actions">
<button onclick="agentSrvStatus()">\u67e5\u770b\u72b6\u6001</button>
<button class="sec" onclick="agentSrvStart()">\u542f\u52a8\u670d\u52a1\u5668</button>
<button class="danger" onclick="agentSrvStop()">\u505c\u6b62\u670d\u52a1\u5668</button>
</div>
<pre id="srvOut" style="margin-top:12px;max-height:200px;overflow-y:auto"></pre>
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
<label>Manifest \u8def\u5f84</label><input id="manifestPath" value="C:\\ACBH-Test\\server-a\\server-pack.manifest.json"/>

<h3>RCON \u914d\u7f6e</h3>
<div class="row"><div><label>RCON Host</label><input id="rconHost" value="127.0.0.1"/></div><div><label>RCON Port</label><input id="rconPort" value="25575"/></div><div><label>RCON Password</label><input id="rconPassword" type="password" autocomplete="off"/></div></div>

<h3>\u5236\u54c1 ID</h3>
<div class="row"><div><label>server-pack ID</label><input id="serverPackId" value="win-server-pack-001"/></div><div><label>world-snapshot ID</label><input id="worldSnapshotId" value="win-world-safe-001"/></div></div>
<div class="row"><div><label>server-runtime ID</label><input id="serverRuntimeId" value="runtime-001"/></div></div>

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
<div class="row"><div><label>\u5236\u54c1\u7c7b\u578b</label><select id="artifactKind"><option>server-runtime</option><option>server-pack</option><option>world-snapshot</option><option>admin-state</option></select></div><div><label>\u5236\u54c1 ID</label><input id="artifactId"/></div></div>
<div class="actions">
<button onclick="loadArtifacts()">\u5236\u54c1\u5217\u8868</button>
<button class="sec" onclick="loadLatest()">\u6700\u65b0\u5236\u54c1</button>
<button class="sec" onclick="loadManifest()">Manifest \u5185\u5bb9</button>
</div>
<pre id="artifactOut" style="margin-top:12px"></pre>
</div>

<!-- Election / Takeover -->
<div class="panel" id="panel-election">
<h2>Election / Takeover</h2>
<div class="warn">\u6545\u969c\u63a5\u7ba1\u4f1a\u6539\u53d8 current host \u548c generation\uff0c\u4e0d\u662f\u666e\u901a\u91cd\u542f\u3002\u8bf7\u5148\u786e\u8ba4\u539f\u4e3b\u673a\u72b6\u6001\u3002</div>
<div class="row"><div><label>Election reason</label><select id="electionReason"><option>manual</option><option>heartbeat-timeout</option><option>no-current-host</option></select></div><div><label>Assignment ID</label><input id="assignmentId"/></div></div>
<div class="row"><div><label>Takeover Token</label><input id="takeoverToken" type="password" autocomplete="off"/><div class="actions"><button class="sm sec" onclick="toggleSecret('takeoverToken')">\u77ed\u6682\u663e\u793a</button></div></div><div><label>Failure reason</label><input id="takeoverFailureReason" value="local takeover failed"/></div></div>
<div class="actions">
<button onclick="refreshElection()">Refresh election status</button>
<button class="sec" onclick="runElection()">Run election</button>
<button class="sec" onclick="checkElectionTimeout()">Check timeout</button>
<button class="sec" onclick="pollTakeover()">Poll assignment</button>
<button class="sec" onclick="acceptTakeover()">Accept takeover</button>
<button class="sec" onclick="completeTakeover()">Complete takeover</button>
<button class="danger" onclick="failTakeover()">Fail takeover</button>
</div>
<pre id="electionOut" style="margin-top:12px"></pre>
</div>

<!-- Events -->
<div class="panel" id="panel-events">
<h2>Events / Logs</h2>
<p style="color:var(--muted);font-size:13px;margin-bottom:12px">\u663e\u793a\u5f53\u524d\u9875\u9762\u6700\u8fd1\u64cd\u4f5c\u6458\u8981\u3002\u51ed\u636e\u4f1a\u5728\u8bb0\u5f55\u524d\u906e\u853d\uff0c\u4e0d\u4f1a\u5199\u5165\u6d4f\u89c8\u5668\u5b58\u50a8\u3002</p>
<div class="actions"><button class="sec" onclick="clearEvents()">Clear events</button></div>
<pre id="eventsOut" style="margin-top:12px"></pre>
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
<li><span class="text"><strong>\u542f\u52a8\u672c\u5730 Agent \u63a7\u5236\u670d\u52a1</strong><br><code>acbh-agent control serve</code> \u5e76\u4ece control-token \u6587\u4ef6\u8bfb\u53d6\u51ed\u636e</span></li>
<li><span class="text"><strong>\u5728 Dashboard \u8fde\u63a5\u672c\u5730 Agent API</strong><br>\u8f93\u5165 token \u540e\u70b9\u51fb\u201c\u8fde\u63a5\u672c\u673a Agent\u201d</span><button class="sm sec" onclick="switchTab('agent')" style="margin-top:4px">\u8df3\u8f6c</button></li>
<li><span class="text"><strong>\u786e\u8ba4\u670d\u52a1\u5668\u72b6\u6001</strong><br>\u70b9\u51fb\u201c\u67e5\u770b\u72b6\u6001\u201d\u786e\u8ba4\u670d\u52a1\u5668\u672a\u8fd0\u884c</span></li>
<li><span class="text"><strong>\u542f\u52a8\u670d\u52a1\u5668</strong><br>\u70b9\u51fb\u201c\u542f\u52a8\u670d\u52a1\u5668\u201d\u542f\u52a8 Minecraft \u670d\u52a1\u7aef</span></li>
<li><span class="text"><strong>\u505c\u6b62\u670d\u52a1\u5668</strong><br>\u6d4b\u8bd5\u540e\u70b9\u51fb\u201c\u505c\u6b62\u670d\u52a1\u5668\u201d\u5173\u95ed\u670d\u52a1\u7aef</span></li>
<li><span class="text"><strong>\u626b\u63cf server-pack</strong><br>\u751f\u6210\u670d\u52a1\u5668\u5305\u6e05\u5355</span><button class="sm sec" onclick="switchTab('agent')" style="margin-top:4px">\u8df3\u8f6c</button></li>
<li><span class="text"><strong>safe-sync world</strong><br>RCON save-all flush \u540e\u626b\u63cf\u4e16\u754c\u5feb\u7167</span></li>
<li><span class="text"><strong>push \u4e0a\u4f20</strong><br>\u4e0a\u4f20 server-pack \u548c world-snapshot</span></li>
<li><span class="text"><strong>pull \u5230 server-b</strong><br>\u62c9\u53d6\u5236\u54c1\u5230\u6062\u590d\u76ee\u5f55</span></li>
<li><span class="text"><strong>\u626b\u63cf\u6062\u590d\u76ee\u5f55</strong><br><code>acbh-agent scan --server-dir &lt;server-b&gt; --output restore.manifest.json</code></span></li>
<li><span class="text"><strong>validate manifest</strong><br><code>acbh-agent manifest validate --file restore.manifest.json</code></span></li>
<li><span class="text"><strong>\u786e\u8ba4 server.properties \u5b58\u5728</strong><br>\u786e\u8ba4\u6062\u590d\u76ee\u5f55\u5305\u542b <code>server.properties</code></span></li>
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
var persistedKeys=["coordinatorUrl","groupId","memberId","hostId","displayName","deviceName","platform","shellType","agentExe","serverDir","serverBDir","serverPackId","worldSnapshotId","serverRuntimeId","workspaceDir","rconHost","rconPort","ocWorkspace","agentApiUrl","srvDir","srvJava","srvJar","srvJvmArgs","srvArgs","manifestPath","agentVersion"];
var secretKeys=["accessKey","hostToken","agentToken","rconPassword","srvRconPassword","takeoverToken","playerToken","localControlToken","secret"];
var recentEvents=[];

function redact(v){
  var text=String(v===undefined?"":v);
  for(var i=0;i<secretKeys.length;i++){
    var el=$(secretKeys[i]),secret=el&&el.value?el.value:"";
    if(secret&&secret.length>=4)text=text.split(secret).join("[redacted]");
  }
  return text;
}
function renderEvents(){
  var out=$("eventsOut");
  if(out)out.textContent=recentEvents.length?recentEvents.join("\\n"):"No events yet.";
}
function addEvent(v){
  recentEvents.unshift(new Date().toLocaleTimeString()+"  "+redact(v));
  recentEvents=recentEvents.slice(0,50);
  renderEvents();
}
function clearEvents(){recentEvents=[];renderEvents();setMsg("Events cleared.");}
function setMsg(v){var safe=redact(v);$("quickMsg").textContent=safe;addEvent(safe);}
function baseUrl(){return $("coordinatorUrl").value.replace(/\\/$/,"");}
function errMsg(e){return e&&e.message?e.message:"Request failed";}

function toggleSecret(id){
  var el=$(id);if(!el)return;
  el.type=el.type==="password"?"text":"password";
  if(el.type==="text")setTimeout(function(){if(el)el.type="password";},10000);
}

async function copySecret(id){
  var el=$(id);
  if(!el||!el.value){setMsg("No credential to copy.");return;}
  await navigator.clipboard.writeText(el.value);
  setMsg("Credential copied. It remains in page memory only.");
}

function isLoopbackControlUrl(value){
  try{
    var u=new URL(value);
    var h=u.hostname.toLowerCase();
    return u.protocol==="http:"&&(h==="localhost"||h==="127.0.0.1"||h==="::1"||h.indexOf("127.")===0);
  }catch(e){return false;}
}

function checkLocalControlUrl(){
  var safe=isLoopbackControlUrl($("agentApiUrl").value);
  $("agentUrlWarning").style.display=safe?"none":"block";
  return safe;
}

function saveLocal(){
  for(var i=0;i<persistedKeys.length;i++){
    var k=persistedKeys[i],el=$(k);
    if(el&&el.value!==undefined)localStorage.setItem("acbh."+k,el.value);
  }
  setMsg("\u5df2\u4fdd\u5b58\u975e\u654f\u611f\u8bbe\u7f6e\u3002\u51ed\u636e\u4ec5\u4fdd\u7559\u5728\u5f53\u524d\u9875\u9762\u5185\u5b58\u4e2d\u3002");
}

function restoreLocal(){
  for(var s=0;s<secretKeys.length;s++)localStorage.removeItem("acbh."+secretKeys[s]);
  $("coordinatorUrl").value=localStorage.getItem("acbh.coordinatorUrl")||location.origin;
  for(var i=1;i<persistedKeys.length;i++){
    var v=localStorage.getItem("acbh."+persistedKeys[i]),el=$(persistedKeys[i]);
    if(v&&el)el.value=v;
  }
}

function forgetSecrets(){
  clearSecretValues();
  agentConnected=false;setAgentMode(false);
  setMsg("\u5df2\u6e05\u9664\u5f53\u524d\u9875\u9762\u51ed\u636e\u548c\u65e7\u7248\u672c\u5730\u5b58\u50a8\u3002");
}

function clearSecretValues(){
  for(var i=0;i<secretKeys.length;i++){
    localStorage.removeItem("acbh."+secretKeys[i]);
    var el=$(secretKeys[i]);if(el)el.value="";
  }
}

function authError(status){
  if(status===401||status===403){
    clearSecretValues();
    agentConnected=false;setAgentMode(false);
    setMsg("\u51ed\u636e\u65e0\u6548\u6216\u5df2\u8fc7\u671f\uff0c\u5df2\u6e05\u9664\u9875\u9762\u5185\u5b58\u4e2d\u7684\u51ed\u636e\uff0c\u8bf7\u91cd\u65b0\u8f93\u5165\u3002");
  }
}

async function api(path,options){
  options=options||{};
  var h=Object.assign({"content-type":"application/json"},options.headers||{});
  var res=await fetch(baseUrl()+path,Object.assign({},options,{headers:h}));
  var t=await res.text();
  try{var body=JSON.parse(t);}catch(e){body={};}
  if(!res.ok){
    authError(res.status);
    var code=body&&body.code?" ["+body.code+"]":"";
    var message=body&&body.message?body.message:res.statusText;
    throw new Error(res.status+code+" "+message);
  }
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
    $("groupId").value=b.groupId;$("accessKey").value=b.accessKey;$("memberId").value=b.ownerMemberId||"";
    $("groupOut").textContent=JSON.stringify({groupId:b.groupId,ownerMemberId:b.ownerMemberId,accessKey:"[stored in memory only]"},null,2);
    $("ovGroup").textContent=b.groupId;$("ovHost").textContent="--";
    saveLocal();setMsg("\u7ec4\u5df2\u521b\u5efa: "+b.groupId);
  }catch(e){$("groupOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function registerHost(){
  try{
    setMsg("Registering host...");
    var b=await api("/v1/hosts/register",{method:"POST",body:JSON.stringify({
      groupId:$("groupId").value,
      accessKey:$("accessKey").value,
      memberId:$("memberId").value,
      deviceName:$("deviceName").value,
      platform:$("platform").value,
      agentVersion:$("agentVersion").value||"0.1.0"
    })});
    $("hostId").value=b.hostId;$("hostToken").value=b.hostToken;
    $("ovHost").textContent=b.hostId;
    $("groupOut").textContent=JSON.stringify({hostId:b.hostId,hostToken:"[stored in memory only]"},null,2);
    saveLocal();setMsg("Host registered: "+b.hostId);
  }catch(e){$("groupOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function sendHeartbeat(){
  try{
    setMsg("Sending heartbeat...");
    var b=await api("/v1/hosts/heartbeat",{method:"POST",body:JSON.stringify({
      groupId:$("groupId").value,
      hostId:$("hostId").value,
      hostToken:$("hostToken").value,
      status:$("heartbeatStatus").value
    })});
    $("groupStateOut").textContent=JSON.stringify(b,null,2);
    setMsg("Heartbeat sent.");
  }catch(e){$("groupStateOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function loadState(){
  try{
    var gid=$("groupId").value;
    var h=hostHeaders();
    if(!h["x-acbh-host-token"]&&$("accessKey").value)h["x-acbh-access-key"]=$("accessKey").value;
    var b=await api("/v1/groups/"+encodeURIComponent(gid)+"/state",{headers:h});
    $("groupStateOut").textContent=JSON.stringify(b,null,2);
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
    $("artifactOut").textContent=JSON.stringify(Object.assign({latest:true},b),null,2);setMsg("\u6700\u65b0\u5236\u54c1\u5df2\u52a0\u8f7d");
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
function exeName(){return $("agentExe").value||($("platform").value==="windows"?"."+String.fromCharCode(92)+"acbh-agent-windows-amd64.exe":"./acbh-agent");}
function isBash(){return $("shellType").value==="bash";}
function shellCont(){var nl=String.fromCharCode(10);return isBash()?" "+String.fromCharCode(92)+nl+"  ":" "+String.fromCharCode(96)+nl+"  ";}
function shellJoin(lines){return lines.join(shellCont());}
function secretEnvSetup(name,label){
  if(isBash())return 'read -rsp "'+label+': " '+name+"; export "+name+"; echo";
  return '$acbhSecret = Read-Host "'+label+'" -AsSecureString\\n$env:'+name+' = [System.Net.NetworkCredential]::new("", $acbhSecret).Password';
}

function genLogin(){
  var e=exeName();var cmd=shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+$("groupId").value,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]);
  $("agentCommands").value=secretEnvSetup("ACBH_ACCESS_KEY","ACBH access key")+"\\n"+cmd;saveLocal();switchTab("agent");
}

function genDoctor(){
  $("agentCommands").value=exeName()+" doctor";switchTab("agent");
}

function genScan(){
  var e=exeName(),dir=$("serverDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value;
  var sep=isBash()?"/":String.fromCharCode(92);var sp=dir+sep+"server-pack.manifest.json";
  $("agentCommands").value=shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]);
  switchTab("agent");
}

function genSafeSync(){
  var e=exeName(),dir=$("serverDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",world=$("worldSnapshotId").value,pack=$("serverPackId").value;
  var sep=isBash()?"/":String.fromCharCode(92);var wm=dir+sep+"world.manifest.json";
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575";
  $("agentCommands").value=secretEnvSetup("ACBH_RCON_PASSWORD","RCON password")+"\\n"+shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]);
  switchTab("agent");
}

function genPush(){
  var e=exeName(),dir=$("serverDir").value;
  var sep=isBash()?"/":String.fromCharCode(92);var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  $("agentCommands").value=[e+" push --server-dir "+dir+" --manifest "+sp,e+" push --server-dir "+dir+" --manifest "+wm].join("\\n\\n");
  switchTab("agent");
}

function genPull(){
  var e=exeName(),bdir=$("serverBDir").value,pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  $("agentCommands").value=[e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir].join("\\n\\n");
  switchTab("agent");
}

function genAll(){
  var e=exeName(),dir=$("serverDir").value,bdir=$("serverBDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575";
  var sep=isBash()?"/":String.fromCharCode(92);var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  $("agentCommands").value=[
    secretEnvSetup("ACBH_ACCESS_KEY","ACBH access key"),
    shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+g,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]),
    e+" doctor",
    shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]),
    secretEnvSetup("ACBH_RCON_PASSWORD","RCON password"),
    shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]),
    e+" push --server-dir "+dir+" --manifest "+sp,
    e+" push --server-dir "+dir+" --manifest "+wm,
    e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,
    e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir
  ].join("\\n\\n");
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
  $("opencodePrompt").value=lines.join("\\n");
  saveLocal();switchTab("opencode");
}

function getAllCommands(){
  var e=exeName(),dir=$("serverDir").value,bdir=$("serverBDir").value,g=$("groupId").value,h=$("hostId").value||"<host-id>",pack=$("serverPackId").value,world=$("worldSnapshotId").value;
  var rh=$("rconHost").value||"127.0.0.1",rp=$("rconPort").value||"25575";
  var sep=isBash()?"/":String.fromCharCode(92);var sp=dir+sep+"server-pack.manifest.json",wm=dir+sep+"world.manifest.json";
  return [
    secretEnvSetup("ACBH_ACCESS_KEY","ACBH access key"),
    shellJoin([e+" login","--coordinator "+baseUrl(),"--group-id "+g,'--name "'+$("displayName").value+'"','--device-name "'+$("deviceName").value+'"',"--platform "+$("platform").value]),
    e+" doctor",
    shellJoin([e+" scan","--server-dir "+dir,"--artifact-kind server-pack","--artifact-id "+pack,"--group-id "+g,"--creator-host-id "+h,"--output "+sp]),
    secretEnvSetup("ACBH_RCON_PASSWORD","RCON password"),
    shellJoin([e+" safe-sync","--server-dir "+dir,"--rcon-host "+rh,"--rcon-port "+rp,"--artifact-id "+world,"--group-id "+g,"--creator-host-id "+h,"--server-pack-version "+pack,"--output "+wm]),
    e+" push --server-dir "+dir+" --manifest "+sp,
    e+" push --server-dir "+dir+" --manifest "+wm,
    e+" pull --artifact-kind server-pack --artifact-id "+pack+" --output-dir "+bdir,
    e+" pull --artifact-kind world-snapshot --artifact-id "+world+" --output-dir "+bdir
  ].join("\\n\\n");
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
  if(!checkLocalControlUrl()&&!confirm("This Local Control URL is not loopback. Connect only on a trusted network. Continue?")){setMsg("Remote Local Control connection cancelled.");return;}
  try{
    var r=await fetch(url+"/health");
    var b=await r.json();
    if(b.ok){
      var auth=await fetch(url+"/v1/doctor",{method:"POST",headers:{"Content-Type":"application/json","Authorization":"Bearer "+tok},body:"{}"});
      if(auth.status===401||auth.status===403){
        $("agentToken").value="";setAgentMode(false);setMsg("Local Control credential is invalid; enter it again.");return;
      }
      if(!auth.ok)throw new Error("Local Control authentication check failed");
      setMsg("\u5df2\u8fde\u63a5 Agent: "+b.platform+" PID "+b.pid);
      setAgentMode(true);saveLocal();
    }else{
      setMsg("\u8fde\u63a5\u5931\u8d25: Agent health check returned ok=false");
    }
  }catch(e){setMsg("\u8fde\u63a5\u5931\u8d25: "+errMsg(e));setAgentMode(false);}
}

function disconnectAgent(){setAgentMode(false);setMsg("\u5df2\u65ad\u5f00 Agent \u8fde\u63a5\uff0c\u51ed\u636e\u4ecd\u4ec5\u4fdd\u7559\u5728\u5185\u5b58\u4e2d");}

async function agentCall(path,body){
  var url=$("agentApiUrl").value.replace(/\\/$/,"");
  var tok=$("agentToken").value;
  var r=await fetch(url+path,{method:"POST",headers:{"Content-Type":"application/json","Authorization":"Bearer "+tok},body:JSON.stringify(body)});
  var t=await r.text();var b;
  try{b=JSON.parse(t);}catch(e){b={ok:false};}
  if(!r.ok){
    if(r.status===401||r.status===403){
      $("agentToken").value="";setAgentMode(false);
      b={ok:false,error:"Local control credential is invalid; enter it again",code:"local_control_auth_failed"};
    }else{
      b={ok:false,error:(b&&b.error)||"Local Agent request failed",code:(b&&b.code)||"local_control_request_failed"};
    }
  }
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

async function agentScan(kind){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  kind=kind||"server-pack";setMsg("\u626b\u63cf "+kind+"...");
  try{
    var g=$("groupId").value,h=$("hostId").value||"<host-id>",dir=$("serverDir").value;
    var sep=isBash()?"/":String.fromCharCode(92),id=kind==="server-runtime"?$("serverRuntimeId").value:$("serverPackId").value;
    var sp=dir+sep+(kind==="server-runtime"?"server-runtime":"server-pack")+".manifest.json",generation;
    if(kind==="server-runtime"){
      var state=await api("/v1/groups/"+encodeURIComponent(g)+"/election/status",{headers:hostHeaders()});
      generation=state.currentHostGeneration;
    }
    var b=await agentCall("/v1/scan",{serverDir:dir,artifactKind:kind,artifactId:id,groupId:g,creatorHostId:h,generation:generation,output:sp});
    if(b.ok)$("manifestPath").value=sp;
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"\u626b\u63cf\u5b8c\u6210: "+JSON.stringify(b.summary):"\u626b\u63cf\u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentValidateManifest(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  var path=$("manifestPath").value;
  if(!path){setMsg("Manifest path is required.");return;}
  setMsg("Validating manifest...");
  try{
    var b=await agentCall("/v1/manifest/validate",{path:path});
    $("agentCommands").value=redact(JSON.stringify(b,null,2));
    switchTab("agent");setMsg(b.ok?"Manifest is valid.":"Manifest validation failed: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentSafeSync(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("safe-sync world...");
  try{
    var g=$("groupId").value,h=$("hostId").value||"<host-id>",world=$("worldSnapshotId").value,pack=$("serverPackId").value,dir=$("serverDir").value;
    var sep=isBash()?"/":String.fromCharCode(92);var wm=dir+sep+"world.manifest.json";
    var rh=$("rconHost").value||"127.0.0.1",rp=parseInt($("rconPort").value)||25575,rpw=$("rconPassword").value;
    if(!rpw){setMsg("RCON password is required for safe-sync.");return;}
    var b=await agentCall("/v1/safe-sync",{serverDir:dir,rconHost:rh,rconPort:rp,rconPassword:rpw,artifactId:world,groupId:g,creatorHostId:h,serverPackVersion:pack,output:wm});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"safe-sync \u5b8c\u6210: "+JSON.stringify(b.summary):"safe-sync \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentPush(which){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("push "+which+"...");
  try{
    var dir=$("serverDir").value,sep=isBash()?"/":String.fromCharCode(92);
    var mf=dir+sep+(which==="world"?"world":which==="server-runtime"?"server-runtime":"server-pack")+".manifest.json";
    var b=await agentCall("/v1/push",{coordinatorUrl:baseUrl(),serverDir:dir,manifest:mf});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"push "+which+" \u5b8c\u6210":"push \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentPull(which){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  if(!confirm("Pull and restore "+which+" into the configured output directory? Existing files may be replaced.")){setMsg("Pull cancelled.");return;}
  var allowNonEmpty=false;
  if(which==="server-runtime"){
    allowNonEmpty=confirm("server-runtime restore requires an empty directory by default. Explicitly allow replacing files in a non-empty directory?");
  }
  setMsg("pull "+which+"...");
  try{
    var bdir=$("serverBDir").value,pack=$("serverPackId").value,world=$("worldSnapshotId").value;
    var aid=which==="server-runtime"?"latest":which==="world"?world:pack;
    var artifactKind=which==="server-runtime"?"server-runtime":which==="world"?"world-snapshot":"server-pack";
    var b=await agentCall("/v1/pull",{coordinatorUrl:baseUrl(),artifactKind:artifactKind,artifactId:aid,outputDir:bdir,allowNonEmpty:allowNonEmpty});
    $("agentCommands").value=JSON.stringify(b,null,2);
    switchTab("agent");setMsg(b.ok?"pull "+which+" \u5b8c\u6210":"pull \u5931\u8d25: "+(b.error||""));
  }catch(e){setMsg(errMsg(e));}
}

async function agentSrvStatus(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("\u67e5\u770b\u670d\u52a1\u5668\u72b6\u6001...");
  try{
    var dir=$("srvDir").value||$("serverDir").value;
    var b=await agentCall("/v1/server/status",{serverDir:dir});
    $("srvOut").textContent=JSON.stringify(b,null,2);
    if(b.running)setMsg("\u670d\u52a1\u5668\u8fd0\u884c\u4e2d PID "+b.state.pid);
    else if(b.stale)setMsg("\u670d\u52a1\u5668\u72b6\u6001\u5f02\u5e38: "+(b.reason||"\u8fdb\u7a0b\u6216\u72b6\u6001\u65e0\u6cd5\u9a8c\u8bc1"));
    else setMsg(b.message||"\u670d\u52a1\u5668\u672a\u8fd0\u884c");
  }catch(e){$("srvOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function agentSrvStart(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  setMsg("\u542f\u52a8\u670d\u52a1\u5668...");
  try{
    var dir=$("srvDir").value||$("serverDir").value;
    if(!dir){setMsg("\u9519\u8bef\uff1a\u8bf7\u5148\u8f93\u5165\u670d\u52a1\u7aef\u76ee\u5f55");return;}
    var jvmArgsStr=$("srvJvmArgs").value.trim();
    var jvmArgs=jvmArgsStr?jvmArgsStr.split(/\\s+/):["-Xmx2G","-Xms1G"];
    var serverArgsStr=$("srvArgs").value.trim();
    var serverArgs=serverArgsStr?serverArgsStr.split(/\\s+/):["nogui"];
    var b=await agentCall("/v1/server/start",{
      serverDir:dir,
      javaPath:$("srvJava").value||"java",
      jarPath:$("srvJar").value||"fabric-server-launch.jar",
      jvmArgs:jvmArgs,
      serverArgs:serverArgs,
      rconPassword:$("srvRconPassword").value||""
    });
    $("srvOut").textContent=JSON.stringify(b,null,2);
    setMsg(b.ok?(b.message||"\u670d\u52a1\u5668\u5df2\u542f\u52a8"):("\u542f\u52a8\u5931\u8d25: "+(b.error||"")));
  }catch(e){$("srvOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

async function agentSrvStop(){
  if(!agentConnected){setMsg("\u8bf7\u5148\u8fde\u63a5\u672c\u673a Agent");return;}
  if(!confirm("Stop the managed Minecraft server?")){setMsg("Server stop cancelled.");return;}
  setMsg("\u505c\u6b62\u670d\u52a1\u5668...");
  try{
    var dir=$("srvDir").value||$("serverDir").value;
    var b=await agentCall("/v1/server/stop",{
      serverDir:dir,
      rconPassword:$("srvRconPassword").value||""
    });
    $("srvOut").textContent=JSON.stringify(b,null,2);
    setMsg(b.ok?(b.stopped?(b.message||"\u670d\u52a1\u5668\u5df2\u505c\u6b62"):(b.message||"\u670d\u52a1\u5668\u672a\u8fd0\u884c")):("\u505c\u6b62\u5931\u8d25: "+(b.error||"")));
  }catch(e){$("srvOut").textContent=errMsg(e);setMsg(errMsg(e));}
}

function hostAuthBody(extra){
  return Object.assign({
    groupId:$("groupId").value,
    hostId:$("hostId").value,
    hostToken:$("hostToken").value
  },extra||{});
}

async function refreshElection(){
  try{
    setMsg("Loading election status...");
    var gid=encodeURIComponent($("groupId").value);
    var b=await api("/v1/groups/"+gid+"/election/status",{headers:hostHeaders()});
    $("electionOut").textContent=JSON.stringify(b,null,2);
    if(b.activeTakeoverAssignment&&b.activeTakeoverAssignment.assignmentId)$("assignmentId").value=b.activeTakeoverAssignment.assignmentId;
    setMsg("Election status loaded.");
  }catch(e){$("electionOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function runElection(){
  if(!confirm("Run a fault-takeover election now? This may offer takeover to another host.")){setMsg("Election cancelled.");return;}
  try{
    setMsg("Running election...");
    var gid=$("groupId").value;
    var b=await api("/v1/groups/"+encodeURIComponent(gid)+"/election/run",{method:"POST",body:JSON.stringify(hostAuthBody({reason:$("electionReason").value}))});
    $("electionOut").textContent=JSON.stringify(b,null,2);
    if(b.assignment&&b.assignment.assignmentId)$("assignmentId").value=b.assignment.assignmentId;
    setMsg("Election completed.");
  }catch(e){$("electionOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function checkElectionTimeout(){
  if(!confirm("Check heartbeat timeout and start takeover election if required?")){setMsg("Timeout check cancelled.");return;}
  try{
    setMsg("Checking election timeout...");
    var gid=$("groupId").value;
    var b=await api("/v1/groups/"+encodeURIComponent(gid)+"/election/check-timeout",{method:"POST",body:JSON.stringify(hostAuthBody())});
    $("electionOut").textContent=JSON.stringify(b,null,2);
    setMsg("Election timeout check completed.");
  }catch(e){$("electionOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function pollTakeover(){
  try{
    setMsg("Polling takeover assignment...");
    var b=await api("/v1/hosts/takeover/poll",{method:"POST",body:JSON.stringify(hostAuthBody())});
    if(b.assignment){
      $("assignmentId").value=b.assignment.assignmentId||"";
      if(b.assignment.takeoverToken)$("takeoverToken").value=b.assignment.takeoverToken;
    }
    var visible=JSON.parse(JSON.stringify(b));
    if(visible.assignment&&visible.assignment.takeoverToken)visible.assignment.takeoverToken="[stored in memory only]";
    $("electionOut").textContent=JSON.stringify(visible,null,2);
    setMsg(b.assignment?"Takeover assignment received.":"No takeover assignment.");
  }catch(e){$("electionOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

async function takeoverAction(path,extra,verb){
  if(!confirm(verb+" this fault-takeover assignment?")){setMsg(verb+" cancelled.");return;}
  try{
    setMsg(verb+" takeover...");
    var body=hostAuthBody({
      assignmentId:$("assignmentId").value,
      takeoverToken:$("takeoverToken").value
    });
    var b=await api(path,{method:"POST",body:JSON.stringify(Object.assign(body,extra||{}))});
    $("electionOut").textContent=JSON.stringify(b,null,2);
    setMsg("Takeover "+verb.toLowerCase()+" completed.");
  }catch(e){$("electionOut").textContent=redact(errMsg(e));setMsg(errMsg(e));}
}

function acceptTakeover(){return takeoverAction("/v1/hosts/takeover/accept",{},"Accept");}
function completeTakeover(){return takeoverAction("/v1/hosts/takeover/complete",{},"Complete");}
function failTakeover(){return takeoverAction("/v1/hosts/takeover/fail",{failureReason:$("takeoverFailureReason").value},"Fail");}

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

restoreLocal();checkLocalControlUrl();renderEvents();refreshHealth();refreshStoragePanel();
</script>
</body>
</html>`;

export async function registerDashboardRoutes(app: FastifyInstance): Promise<void> {
  app.get("/dashboard", async (_request, reply: FastifyReply) => {
    reply.type("text/html; charset=utf-8");
    return dashboardHtml;
  });
}
