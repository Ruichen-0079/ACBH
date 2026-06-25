package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type RuntimeOptions struct {
	ListenAddr  string
	OpenBrowser bool
}

type DesktopRuntime struct {
	URL      string
	Manager  *OperationManager
	server   *http.Server
	listener net.Listener
}

func StartDesktopRuntime(ctx context.Context, opts Options, runtimeOpts RuntimeOptions) (*DesktopRuntime, error) {
	opts = withDefaults(opts)
	if runtimeOpts.ListenAddr == "" {
		runtimeOpts.ListenAddr = "127.0.0.1:0"
	}
	manager := NewOperationManager(opts)
	mux := http.NewServeMux()
	rt := &DesktopRuntime{Manager: manager}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, desktopHTML)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		status, _ := CurrentStatus(r.Context(), opts)
		writeJSON(w, map[string]any{"status": status, "operations": manager.Summary()})
	})
	mux.HandleFunc("/api/operations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, manager.Summary())
	})
	mux.HandleFunc("/api/operations/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			OperationID string `json:"operationId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		writeJSON(w, map[string]any{"ok": manager.Cancel(body.OperationID)})
	})
	registerOperationEndpoint(mux, manager, "/api/bootstrap", OperationOptions{
		Name: "Bootstrap", MutexClass: "bootstrap", Cancellable: true, Timeout: 90 * time.Second,
	}, func(ctx OperationContext) (any, error) {
		return RunBootstrap(ctx, opts)
	})
	registerOperationEndpoint(mux, manager, "/api/environment/check", OperationOptions{
		Name: "EnvironmentCheck", MutexClass: "read:environment", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("environment", "检查桌面运行环境", 1, 1)
		return CheckEnvironment(opts)
	})
	registerOperationEndpoint(mux, manager, "/api/status/refresh", OperationOptions{
		Name: "RefreshStatus", MutexClass: "read:status", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("status", "刷新服务器状态", 1, 1)
		return CurrentStatus(ctx, opts)
	})
	mux.HandleFunc("/api/network/configure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			Host            string `json:"host"`
			CoordinatorPort string `json:"coordinatorPort"`
			PublicGamePort  string `json:"publicGamePort"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{
			Name: "ConfigureNetwork", MutexClass: "config", Timeout: 30 * time.Second,
		}, func(ctx OperationContext) (any, error) {
			ctx.Progress("network", "保存公网配置", 1, 1)
			return ConfigureNetwork(opts, body.Host, body.CoordinatorPort, body.PublicGamePort)
		})
	})
	registerOperationEndpoint(mux, manager, "/api/server/start", OperationOptions{
		Name: "StartServer", MutexClass: "server:start-stop", Timeout: 3 * time.Minute,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("server", "启动 Minecraft 服务端", 1, 1)
		return StartServer(opts)
	})
	registerOperationEndpoint(mux, manager, "/api/server/stop", OperationOptions{
		Name: "StopServer", MutexClass: "server:start-stop", Timeout: 60 * time.Second,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("server", "停止 Minecraft 服务端", 1, 1)
		return StopServer(opts)
	})
	registerOperationEndpoint(mux, manager, "/api/invites/list", OperationOptions{
		Name: "ListInvites", MutexClass: "read:invites", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("invites", "刷新成员邀请", 1, 1)
		return SetupListInvites(ctx, opts)
	})
	mux.HandleFunc("/api/invites/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			ExpiresSeconds int  `json:"expiresSeconds"`
			OneTime        bool `json:"oneTime"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{
			Name: "CreateInvite", MutexClass: "invite", Timeout: 30 * time.Second,
		}, func(ctx OperationContext) (any, error) {
			ctx.Progress("invites", "生成邀请码", 1, 1)
			return SetupCreateInvite(ctx, opts, body.ExpiresSeconds, body.OneTime)
		})
	})
	mux.HandleFunc("/api/invites/revoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var body struct {
			InviteID string `json:"inviteId"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		startAndWrite(w, r, manager, OperationOptions{
			Name: "RevokeInvite", MutexClass: "invite", Timeout: 30 * time.Second,
		}, func(ctx OperationContext) (any, error) {
			ctx.Progress("invites", "撤销邀请码", 1, 1)
			return SetupRevokeInvite(ctx, opts, body.InviteID)
		})
	})
	registerOperationEndpoint(mux, manager, "/api/world/status", OperationOptions{
		Name: "WorldBackupStatus", MutexClass: "read:world", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("world", "刷新世界备份状态", 1, 1)
		return WorldBackupStatus(ctx, opts)
	})
	registerOperationEndpoint(mux, manager, "/api/world/backup", OperationOptions{
		Name: "WorldBackupCreate", MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("world", "创建世界备份", 0, 0)
		return WorldBackupCreate(ctx, opts, WorldBackupOptions{}, false, "")
	})
	mux.HandleFunc("/api/diagnostics/summary", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, map[string]any{
			"appDataDir": opts.AppDataDir,
			"debugLog":   manager.logPath,
			"operations": manager.Summary(),
		})
	})

	ln, err := net.Listen("tcp", runtimeOpts.ListenAddr)
	if err != nil {
		return nil, err
	}
	rt.listener = ln
	rt.URL = "http://" + ln.Addr().String()
	rt.server = &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = rt.server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := rt.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			manager.appendSummary("runtime", "server_error", "DesktopRuntime", err.Error())
		}
	}()
	_, _ = manager.Start(ctx, OperationOptions{
		Name: "Bootstrap", MutexClass: "bootstrap", Cancellable: true, Timeout: 90 * time.Second, Coalesce: true,
	}, func(opCtx OperationContext) (any, error) {
		return RunBootstrap(opCtx, opts)
	})
	if runtimeOpts.OpenBrowser {
		_ = openBrowser(rt.URL)
	}
	return rt, nil
}

func RunDesktopRuntime(ctx context.Context, opts Options, runtimeOpts RuntimeOptions, out io.Writer) error {
	rt, err := StartDesktopRuntime(ctx, opts, runtimeOpts)
	if err != nil {
		return err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "ACBH Desktop: %s\n", rt.URL)
	}
	<-ctx.Done()
	return nil
}

func registerOperationEndpoint(mux *http.ServeMux, manager *OperationManager, path string, opts OperationOptions, fn OperationFunc) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		startAndWrite(w, r, manager, opts, fn)
	})
}

func startAndWrite(w http.ResponseWriter, r *http.Request, manager *OperationManager, opts OperationOptions, fn OperationFunc) {
	snap, err := manager.Start(r.Context(), opts, fn)
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"ok": false, "errorCode": "operation_conflict", "message": err.Error()})
		return
	}
	writeJSON(w, snap)
}

func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1*1024*1024))
	if err := dec.Decode(out); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"ok": false, "errorCode": "bad_json", "message": err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.WriteHeader(http.StatusMethodNotAllowed)
	writeJSON(w, map[string]any{"ok": false, "errorCode": "method_not_allowed"})
}

func openBrowser(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

const desktopHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ACBH Desktop</title>
<style>
:root{font-family:Segoe UI,Microsoft YaHei,sans-serif;color:#1b1f23;background:#f7f7f4}
body{margin:0}
header{display:flex;align-items:center;justify-content:space-between;padding:14px 18px;background:#283138;color:#fff}
h1{font-size:18px;margin:0;font-weight:650}
main{display:grid;grid-template-columns:260px minmax(0,1fr);min-height:calc(100vh - 52px)}
nav{background:#e8ece8;padding:12px;border-right:1px solid #cfd6d2}
nav button,.toolbar button{width:100%;height:36px;margin:4px 0;border:1px solid #aeb8b2;background:#fff;border-radius:6px;text-align:left;padding:0 10px;color:#1b1f23}
nav button.active{background:#d5e6dc;border-color:#5a866f}
section{display:none;padding:18px;max-width:1100px}
section.active{display:block}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:12px}
.panel{border:1px solid #d4d8d4;background:#fff;border-radius:8px;padding:12px}
.panel h2{font-size:15px;margin:0 0 10px}
.toolbar{display:flex;gap:8px;flex-wrap:wrap}
.toolbar button{width:auto;text-align:center}
input{height:32px;border:1px solid #aeb8b2;border-radius:6px;padding:0 8px;max-width:100%}
label{display:block;font-size:12px;color:#46515a;margin:8px 0 4px}
pre{white-space:pre-wrap;overflow:auto;background:#f2f4f2;border:1px solid #d7ddd8;border-radius:6px;padding:10px;max-height:340px}
table{width:100%;border-collapse:collapse;font-size:13px}td,th{border-bottom:1px solid #e1e4e2;padding:7px;text-align:left}
@media(max-width:720px){main{grid-template-columns:1fr}nav{border-right:0;border-bottom:1px solid #cfd6d2}}
</style>
</head>
<body>
<header><h1>ACBH Desktop v0.4.0-alpha3</h1><div id="busy">启动中</div></header>
<main>
<nav>
<button data-tab="bootstrap" class="active">Bootstrap</button>
<button data-tab="server">服务器控制</button>
<button data-tab="invites">成员邀请</button>
<button data-tab="world">世界备份</button>
<button data-tab="diagnostics">诊断</button>
</nav>
<section id="bootstrap" class="active">
<div class="toolbar"><button onclick="post('/api/bootstrap')">重新初始化</button><button onclick="post('/api/environment/check')">环境检查</button><button onclick="post('/api/status/refresh')">刷新状态</button></div>
<div class="grid"><div class="panel"><h2>状态</h2><pre id="statusOut"></pre></div><div class="panel"><h2>公网配置</h2><label>Host</label><input id="host" placeholder="example.com"><label>Coordinator Port</label><input id="coordPort" value="6121"><label>Game Port</label><input id="gamePort" value="25565"><div class="toolbar"><button onclick="configureNetwork()">保存</button></div></div></div>
</section>
<section id="server"><div class="toolbar"><button onclick="post('/api/server/start')">启动</button><button onclick="post('/api/server/stop')">停止</button><button onclick="post('/api/status/refresh')">刷新</button></div><pre id="serverOut"></pre></section>
<section id="invites"><div class="toolbar"><button onclick="post('/api/invites/create',{expiresSeconds:1800,oneTime:true})">生成一次性邀请码</button><button onclick="post('/api/invites/list')">刷新列表</button></div><pre id="inviteOut"></pre></section>
<section id="world"><div class="toolbar"><button onclick="post('/api/world/status')">刷新备份状态</button><button onclick="confirmRun('/api/world/backup')">创建备份</button></div><pre id="worldOut"></pre></section>
<section id="diagnostics"><div class="toolbar"><button onclick="loadDiagnostics()">复制诊断摘要</button></div><pre id="diagOut"></pre><div class="panel"><h2>Operations</h2><table id="ops"></table></div></section>
</main>
<script>
const tabs=document.querySelectorAll('nav button');tabs.forEach(b=>b.onclick=()=>{tabs.forEach(x=>x.classList.remove('active'));document.querySelectorAll('section').forEach(x=>x.classList.remove('active'));b.classList.add('active');document.getElementById(b.dataset.tab).classList.add('active')});
async function api(path,opts){const r=await fetch(path,opts);return await r.json()}
async function post(path,body={}){const j=await api(path,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(body)});routeOut(path,j);setTimeout(refresh,500)}
function confirmRun(path){if(confirm('确认执行该操作？'))post(path)}
function routeOut(path,j){const text=JSON.stringify(j,null,2);if(path.includes('invite'))inviteOut.textContent=text;else if(path.includes('world'))worldOut.textContent=text;else if(path.includes('server'))serverOut.textContent=text;else statusOut.textContent=text}
function configureNetwork(){post('/api/network/configure',{host:host.value,coordinatorPort:coordPort.value,publicGamePort:gamePort.value})}
async function refresh(){const j=await api('/api/status');statusOut.textContent=JSON.stringify(j.status,null,2);busy.textContent=j.operations.busy?'处理中':'就绪';renderOps(j.operations.operations)}
function renderOps(ops){ops=ops||[];document.getElementById('ops').innerHTML='<tr><th>操作</th><th>状态</th><th>阶段</th><th>traceId</th></tr>'+ops.slice(0,20).map(o=>'<tr><td>'+esc(o.name)+'</td><td>'+esc(o.state)+'</td><td>'+esc(o.currentStage||'')+'</td><td>'+esc(o.traceId)+'</td></tr>').join('')}
async function loadDiagnostics(){const j=await api('/api/diagnostics/summary');const text=JSON.stringify(j,null,2);diagOut.textContent=text;try{await navigator.clipboard.writeText(text)}catch{}}
function esc(s){return String(s||'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]))}
refresh();setInterval(refresh,1500);
</script>
</body></html>`
