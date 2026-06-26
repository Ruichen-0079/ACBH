package desktop

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	session  string
}

func StartDesktopRuntime(ctx context.Context, opts Options, runtimeOpts RuntimeOptions) (*DesktopRuntime, error) {
	opts = withDefaults(opts)
	if runtimeOpts.ListenAddr == "" {
		runtimeOpts.ListenAddr = "127.0.0.1:0"
	}
	manager := NewOperationManager(opts)
	mux := http.NewServeMux()
	rt := &DesktopRuntime{Manager: manager, session: randomSessionToken()}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if token := r.URL.Query().Get("session"); token != "" {
			if !secureCompare(token, rt.session) {
				http.Error(w, "invalid session", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     "acbh_desktop_session",
				Value:    rt.session,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   8 * 60 * 60,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if !rt.authorized(r) {
			http.Error(w, "session required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, desktopHTML)
	})
	api := rt.secureAPI(mux)
	api("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		status, _ := CurrentStatus(r.Context(), opts)
		writeJSON(w, map[string]any{"status": status, "operations": manager.Summary()})
	})
	api("/api/operations", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, manager.Summary())
	})
	api("/api/operations/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/operations/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		if snap, ok := manager.Get(id); ok {
			writeJSON(w, snap)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"ok": false, "errorCode": "operation_not_found"})
	})
	api("/api/operations/cancel", func(w http.ResponseWriter, r *http.Request) {
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
	registerOperationEndpoint(api, manager, "/api/bootstrap", OperationOptions{
		Name: "Bootstrap", MutexClass: "bootstrap", Cancellable: true, Timeout: 90 * time.Second,
	}, func(ctx OperationContext) (any, error) {
		return RunBootstrap(ctx, opts)
	})
	registerOperationEndpoint(api, manager, "/api/environment/check", OperationOptions{
		Name: "EnvironmentCheck", MutexClass: "read:environment", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("environment", "检查桌面运行环境", 1, 1)
		return CheckEnvironment(opts)
	})
	registerOperationEndpoint(api, manager, "/api/status/refresh", OperationOptions{
		Name: "RefreshStatus", MutexClass: "read:status", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("status", "刷新服务器状态", 1, 1)
		return CurrentStatus(ctx, opts)
	})
	api("/api/network/configure", func(w http.ResponseWriter, r *http.Request) {
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
	registerOperationEndpoint(api, manager, "/api/server/start", OperationOptions{
		Name: "StartServer", MutexClass: "server:start-stop", Timeout: 3 * time.Minute,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("server", "启动 Minecraft 服务端", 1, 1)
		return StartServer(opts)
	})
	registerOperationEndpoint(api, manager, "/api/server/stop", OperationOptions{
		Name: "StopServer", MutexClass: "server:start-stop", Timeout: 60 * time.Second,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("server", "停止 Minecraft 服务端", 1, 1)
		return StopServer(opts)
	})
	registerOperationEndpoint(api, manager, "/api/invites/list", OperationOptions{
		Name: "ListInvites", MutexClass: "read:invites", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("invites", "刷新成员邀请", 1, 1)
		return SetupListInvites(ctx, opts)
	})
	api("/api/invites/create", func(w http.ResponseWriter, r *http.Request) {
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
	api("/api/invites/revoke", func(w http.ResponseWriter, r *http.Request) {
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
	registerOperationEndpoint(api, manager, "/api/world/status", OperationOptions{
		Name: "WorldBackupStatus", MutexClass: "read:world", Timeout: 30 * time.Second, Coalesce: true,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("world", "刷新世界备份状态", 1, 1)
		return WorldBackupStatus(ctx, opts)
	})
	registerOperationEndpoint(api, manager, "/api/world/backup", OperationOptions{
		Name: "WorldBackupCreate", MutexClass: "backup-restore", Cancellable: true, Timeout: 30 * time.Minute,
	}, func(ctx OperationContext) (any, error) {
		ctx.Progress("world", "创建世界备份", 0, 0)
		return WorldBackupCreate(ctx, opts, WorldBackupOptions{}, false, "")
	})
	registerAlpha4Endpoints(api, manager, opts)
	api("/api/diagnostics/summary", func(w http.ResponseWriter, r *http.Request) {
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
	rt.URL = "http://" + ln.Addr().String() + "/?session=" + url.QueryEscape(rt.session)
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

type routeRegistrar func(string, http.HandlerFunc)

func registerOperationEndpoint(register routeRegistrar, manager *OperationManager, path string, opts OperationOptions, fn OperationFunc) {
	register(path, func(w http.ResponseWriter, r *http.Request) {
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

func (rt *DesktopRuntime) secureAPI(mux *http.ServeMux) routeRegistrar {
	return func(path string, next http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !rt.authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				writeJSON(w, map[string]any{"ok": false, "errorCode": "session_required"})
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if !rt.validMutatingRequest(r) {
					w.WriteHeader(http.StatusForbidden)
					writeJSON(w, map[string]any{"ok": false, "errorCode": "csrf_rejected"})
					return
				}
			}
			next(w, r)
		})
	}
}

func (rt *DesktopRuntime) authorized(r *http.Request) bool {
	if token := r.Header.Get("X-ACBH-Desktop-Session"); token != "" && secureCompare(token, rt.session) {
		return true
	}
	cookie, err := r.Cookie("acbh_desktop_session")
	return err == nil && secureCompare(cookie.Value, rt.session)
}

func (rt *DesktopRuntime) validMutatingRequest(r *http.Request) bool {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return false
	}
	host := r.Host
	if host == "" || !isLoopbackHost(host) {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Host != host || !isLoopbackHost(parsed.Host) {
		return false
	}
	return true
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func randomSessionToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d%s", time.Now().UnixNano(), randomHex(8))
	}
	return hex.EncodeToString(buf)
}

func secureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

const desktopHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ACBH Desktop</title>
<style>
:root{font-family:Segoe UI,Microsoft YaHei,sans-serif;color:#20252b;background:#f5f6f1}
body{margin:0}
header{display:flex;align-items:center;justify-content:space-between;padding:12px 18px;background:#263238;color:#fff}
h1{font-size:18px;margin:0;font-weight:650;letter-spacing:0}
main{display:grid;grid-template-columns:236px minmax(0,1fr);min-height:calc(100vh - 50px)}
nav{background:#e5ebe6;padding:10px;border-right:1px solid #c8d1ca}
nav button,.toolbar button{height:34px;border:1px solid #aab5ad;background:#fff;border-radius:6px;padding:0 10px;color:#20252b}
nav button{width:100%;margin:4px 0;text-align:left}
nav button.active{background:#d2e7db;border-color:#4e8068}
section{display:none;padding:16px;max-width:1180px}
section.active{display:block}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:10px}
.panel{border:1px solid #d3d9d2;background:#fff;border-radius:8px;padding:12px}
.panel h2{font-size:15px;margin:0 0 8px}
.toolbar{display:flex;gap:8px;flex-wrap:wrap;margin:8px 0}
label{display:block;font-size:12px;color:#50606b;margin:8px 0 4px}
input,textarea,select{box-sizing:border-box;width:100%;border:1px solid #aeb8b2;border-radius:6px;padding:7px;background:#fff;color:#20252b}
textarea{min-height:72px;resize:vertical}
.status{font-size:13px;line-height:1.5;padding:8px;border:1px solid #d9ded8;background:#f8faf8;border-radius:6px;min-height:34px}
.muted{color:#66737c;font-size:12px}
table{width:100%;border-collapse:collapse;font-size:13px}td,th{border-bottom:1px solid #e1e4e2;padding:7px;text-align:left;vertical-align:top}
.pill{display:inline-block;border:1px solid #b7c4ba;background:#eef6ef;border-radius:999px;padding:2px 8px;font-size:12px}
@media(max-width:760px){main{grid-template-columns:1fr}nav{border-right:0;border-bottom:1px solid #c8d1ca}}
</style>
</head>
<body>
<header><h1>ACBH Desktop v0.4.0-alpha4</h1><div id="busy">启动中</div></header>
<main>
<nav>
<button data-tab="wizard" class="active">首次配置</button>
<button data-tab="server">服务器</button>
<button data-tab="backup">备份</button>
<button data-tab="ops">操作队列</button>
<button data-tab="diagnostics">诊断</button>
</nav>
<section id="wizard" class="active">
<div class="toolbar"><button onclick="post('/api/bootstrap')">初始化</button><button onclick="post('/api/environment/check')">环境检查</button><button onclick="loadConfig()">读取配置</button></div>
<div class="grid">
<div class="panel"><h2>网络</h2><label>公网 Host</label><input id="host" placeholder="example.com"><label>Coordinator 端口</label><input id="coordPort" value="6121"><label>Minecraft 入口端口</label><input id="gamePort" value="25565"><div class="toolbar"><button onclick="networkTest()">测试</button><button onclick="networkSave()">保存</button></div><div id="networkState" class="status"></div></div>
<div class="panel"><h2>服务器组</h2><label>Coordinator URL</label><input id="groupUrl" placeholder="http://example.com:6121"><label>Group 名称</label><input id="groupName" value="ACBH Server"><label>显示名</label><input id="displayName" placeholder="Owner"><label>邀请码</label><input id="inviteCode" placeholder="ACBH-XXXXXX-XXXXXX"><div class="toolbar"><button onclick="groupCreate()">创建</button><button onclick="groupJoin()">加入</button><button onclick="whoami()">身份</button></div><div id="groupState" class="status"></div></div>
</div>
</section>
<section id="server"><div class="grid"><div class="panel"><h2>目录与启动项</h2><label>服务器目录</label><input id="serverDir" placeholder="C:\Minecraft\Server"><label>启动文件</label><input id="launchPath" placeholder="server.jar 或 start.bat"><div class="toolbar"><button onclick="serverInspect()">检测目录</button><button onclick="selectLaunch()">选择启动项</button><button onclick="serverPreflight()">预检</button></div><div id="serverState" class="status"></div></div><div class="panel"><h2>控制</h2><div class="toolbar"><button onclick="post('/api/server/start')">启动</button><button onclick="post('/api/server/stop')">停止</button><button onclick="post('/api/status/refresh')">刷新</button></div><div id="serverRunState" class="status"></div></div></div></section>
<section id="backup"><div class="grid"><div class="panel"><h2>备份 Profile</h2><label>Profile ID</label><input id="profileId" value="manual"><label>名称</label><input id="profileName" value="手动选择目录"><label>需备份文件夹，每行一个</label><textarea id="backupRoots" placeholder="C:\Minecraft\Server\world"></textarea><label>排除规则</label><textarea id="excludePatterns">session.lock
*.tmp
*.log</textarea><div class="toolbar"><button onclick="saveProfile()">保存 Profile</button><button onclick="loadProfiles()">刷新 Profile</button></div><div id="profileState" class="status"></div></div><div class="panel"><h2>快照</h2><div class="toolbar"><button onclick="profileScan()">扫描</button><button onclick="profileCreate()">创建快照</button><button onclick="profileRestore()">恢复最新</button><button onclick="post('/api/world/status')">远端状态</button></div><div id="backupState" class="status"></div></div></div><div class="panel"><h2>Profiles</h2><table id="profilesTable"></table></div></section>
<section id="ops"><div class="toolbar"><button onclick="refresh()">刷新</button></div><table id="opsTable"></table><div id="lastResult" class="status"></div></section>
<section id="diagnostics"><div class="toolbar"><button onclick="loadDiagnostics()">复制诊断摘要</button></div><div id="diagOut" class="status"></div></section>
</main>
<script>
const tabs=document.querySelectorAll('nav button');tabs.forEach(b=>b.onclick=()=>{tabs.forEach(x=>x.classList.remove('active'));document.querySelectorAll('section').forEach(x=>x.classList.remove('active'));b.classList.add('active');document.getElementById(b.dataset.tab).classList.add('active')});
async function api(path,opts){const r=await fetch(path,opts);const j=await r.json().catch(()=>({ok:false,message:r.statusText}));if(!r.ok)throw j;return j}
async function post(path,body={}){try{const j=await api(path,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify(body)});showOperation(j,path);setTimeout(refresh,500);return j}catch(e){showError(path,e)}}
async function put(path,body={}){try{const j=await api(path,{method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify(body)});showOperation(j,path);return j}catch(e){showError(path,e)}}
function networkTest(){post('/api/network/test',{host:host.value,coordinatorPort:coordPort.value,publicGamePort:gamePort.value})}
function networkSave(){put('/api/config/network',{host:host.value,coordinatorPort:coordPort.value,publicGamePort:gamePort.value})}
function groupCreate(){post('/api/group/create',{groupName:groupName.value,displayName:displayName.value,coordinatorUrl:groupUrl.value})}
function groupJoin(){post('/api/group/join',{inviteCode:inviteCode.value,displayName:displayName.value,coordinatorUrl:groupUrl.value})}
async function whoami(){try{const j=await api('/api/group/whoami');groupState.innerHTML=kv(j)}catch(e){showError('/api/group/whoami',e)}}
function serverInspect(){post('/api/server/inspect',{path:serverDir.value})}
function selectLaunch(){post('/api/server/select-launch-entry',{path:launchPath.value})}
function serverPreflight(){post('/api/server/preflight')}
async function saveProfile(){const roots=backupRoots.value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean).map((p,i)=>({rootId:'root-'+(i+1),displayName:p.split(/[\\\/]/).pop()||('root-'+(i+1)),kind:'manual-folder',sourcePath:p,restorePath:p,required:true,consistencyGroup:'manual',excludePatterns:excludePatterns.value.split(/\r?\n/).map(x=>x.trim()).filter(Boolean),followSymlinks:false}));await api('/api/backup/profiles',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({profileId:profileId.value,name:profileName.value,roots})}).then(j=>{profileState.innerHTML='已保存 '+esc(j.profiles.length)+' 个 profile';loadProfiles()}).catch(e=>showError('/api/backup/profiles',e))}
function profileScan(){post('/api/backup/profiles/'+encodeURIComponent(profileId.value)+'/scan')}
function profileCreate(){post('/api/backup/profiles/'+encodeURIComponent(profileId.value)+'/create')}
function profileRestore(){post('/api/backup/profiles/'+encodeURIComponent(profileId.value)+'/restore',{snapshotId:'latest'})}
async function loadProfiles(){try{const j=await api('/api/backup/profiles');profilesTable.innerHTML='<tr><th>ID</th><th>名称</th><th>Roots</th></tr>'+(j.profiles||[]).map(p=>'<tr><td>'+esc(p.profileId)+'</td><td>'+esc(p.name)+'</td><td>'+esc((p.roots||[]).map(r=>r.displayName).join(', '))+'</td></tr>').join('')}catch(e){showError('/api/backup/profiles',e)}}
async function loadConfig(){try{const j=await api('/api/config');const d=j.desktop||{},s=j.setup||{},a=j.agent||{};host.value=d.publicEntry?d.publicEntry.split(':')[0]:'';groupUrl.value=d.coordinatorUrl||s.coordinatorUrl||a.coordinatorUrl||'';serverDir.value=d.lastServerDir||(a.server&&a.server.dir)||'';profileState.innerHTML='配置已读取';}catch(e){showError('/api/config',e)}}
async function refresh(){try{const j=await api('/api/status');busy.textContent=j.operations.busy?'处理中':'就绪';serverRunState.innerHTML=statusLine(j.status);renderOps(j.operations.operations)}catch(e){busy.textContent='未授权'}}
function renderOps(ops){ops=ops||[];opsTable.innerHTML='<tr><th>操作</th><th>状态</th><th>阶段</th><th>结果</th></tr>'+ops.slice(0,30).map(o=>'<tr><td>'+esc(o.name)+'</td><td><span class="pill">'+esc(o.state)+'</span></td><td>'+esc(o.currentStage||'')+'</td><td>'+esc(o.terminalResult?o.terminalResult.outcome:'')+'</td></tr>').join('')}
async function loadDiagnostics(){try{const j=await api('/api/diagnostics/summary');const text='AppData: '+j.appDataDir+'<br>Log: '+j.debugLog+'<br>Operations: '+(j.operations.operations||[]).length;diagOut.innerHTML=text;try{await navigator.clipboard.writeText(JSON.stringify(j,null,2))}catch{}}catch(e){showError('/api/diagnostics/summary',e)}}
function showOperation(j,path){lastResult.innerHTML=kv(j);if(path.includes('network'))networkState.innerHTML=kv(j);else if(path.includes('group'))groupState.innerHTML=kv(j);else if(path.includes('server'))serverState.innerHTML=kv(j);else if(path.includes('backup')||path.includes('world'))backupState.innerHTML=kv(j)}
function showError(path,e){const msg=esc(e.message||e.errorCode||'请求失败');lastResult.innerHTML=msg;if(path.includes('network'))networkState.innerHTML=msg;else if(path.includes('group'))groupState.innerHTML=msg;else if(path.includes('server'))serverState.innerHTML=msg;else if(path.includes('backup'))backupState.innerHTML=msg}
function kv(o){if(o.operationId)return '操作已排队：'+esc(o.name)+' / '+esc(o.operationId); if(o.terminalResult)return kv(o.terminalResult); const rows=[]; for(const k of Object.keys(o||{}).slice(0,8)){const v=o[k]; if(typeof v!=='object')rows.push('<b>'+esc(k)+'</b>: '+esc(v))} return rows.join('<br>')||'完成'}
function statusLine(s){return 'Agent '+esc(s.agentStatus||'unknown')+' · Server '+esc(s.minecraftServerStatus||'unknown')+' · '+esc(s.serverDir||'未选择服务器目录')}
function esc(s){return String(s||'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]))}
loadConfig();loadProfiles();refresh();setInterval(refresh,1500);
</script>
</body></html>`
