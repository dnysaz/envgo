package server

import (
	"encoding/json"
	"net/http"

	"envbridge/internal/history"
)

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>envGo Dashboard</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 2rem auto; max-width: 900px; padding: 0 1rem; color:#111; }
  h1 { font-size: 1.4rem; } h2 { font-size: 1rem; margin-top: 2rem; }
  .pill { display:inline-block; background:#eef; border:1px solid #ccd; border-radius:999px; padding:.1rem .6rem; margin:.15rem; font-size:.8rem; }
  table { width:100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align:left; padding:.45rem .5rem; border-bottom:1px solid #eee; }
  th { color:#666; font-weight:600; }
  code { background:#f5f5f5; padding:.1rem .3rem; border-radius:4px; }
  .s2 { color:#080; font-weight:600; } .s4 { color:#a60; font-weight:600; } .s5 { color:#c00; font-weight:600; }
  .muted { color:#888; }
</style>
</head>
<body>
<h1>envGo Dashboard</h1>
<p class="muted">Metadata only. Secret values are never shown or sent to this page.</p>
<div id="meta"></div>
<h2>Loaded variables <span class="muted">(names only)</span></h2>
<div id="vars"></div>
<h2>Recent proxy requests</h2>
<table>
<thead><tr><th>Time</th><th>Method</th><th>Host</th><th>Status</th><th>ms</th><th>Vars</th><th>Error</th></tr></thead>
<tbody id="rows"><tr><td colspan="7" class="muted">Loading...</td></tr></tbody>
</table>
<script>
function esc(s){ var d=document.createElement("div"); d.textContent=s==null?"":String(s); return d.innerHTML; }
function statusClass(s){ if(s>=500) return "s5"; if(s>=400) return "s4"; if(s>=200&&s<300) return "s2"; return ""; }
async function refresh(){
  var res = await fetch("/__envgo_dashboard/data");
  var data = await res.json();
  document.getElementById("meta").innerHTML =
    "Env file: <code>"+esc(data.env_path)+"</code> &middot; " +
    "variables: <b>"+data.env_count+"</b> &middot; requests logged: <b>"+data.requests.length+"</b>";
  document.getElementById("vars").innerHTML = (data.vars||[]).map(function(v){
    return "<span class=\"pill\">"+esc(v)+"</span>";
  }).join("") || "<span class=\"muted\">none</span>";
  var rows = (data.requests||[]).map(function(r){
    var t = new Date(r.time).toLocaleTimeString();
    return "<tr><td>"+esc(t)+"</td><td>"+esc(r.method)+"</td><td>"+esc(r.host)+
      "</td><td class=\""+statusClass(r.status)+"\">"+esc(r.status)+"</td><td>"+esc(r.ms)+
      "</td><td>"+esc((r.variables||[]).join(", "))+"</td><td class=\"muted\">"+esc(r.error)+"</td></tr>";
  }).join("");
  document.getElementById("rows").innerHTML = rows ||
    "<tr><td colspan=\"7\" class=\"muted\">No requests yet.</td></tr>";
}
refresh();
setInterval(refresh, 2000);
</script>
</body>
</html>`

func (s *Server) dashboardAllowed(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
	w.Header().Set("Cache-Control", "no-store")
	if !s.hostAllowed(r.Host) || !s.originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) serveDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.dashboardAllowed(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (s *Server) serveDashboardData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.dashboardAllowed(w, r) {
		return
	}
	var vars []string
	if s.opts.EnvNames != nil {
		vars = s.opts.EnvNames()
	}
	var requests []history.Entry
	if s.opts.History != nil {
		requests = s.opts.History.List()
	}
	if requests == nil {
		requests = []history.Entry{}
	}
	payload := map[string]any{
		"env_path":  s.opts.EnvPath,
		"env_count": len(vars),
		"vars":      vars,
		"requests":  requests,
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}
