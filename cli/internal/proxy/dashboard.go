package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// DashboardHandler serves the local observability UI and its JSON API.
func DashboardHandler(h *History) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/calls", func(w http.ResponseWriter, r *http.Request) {
		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		writeJSON(w, h.Recent(limit))
	})

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.Stats())
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(dashboardHTML))
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Neuron — MCP activity</title>
<style>
  :root { color-scheme: dark; }
  body { font: 13px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; background:#0b0d10; color:#d7dae0; margin:0; padding:24px; }
  h1 { font-size:15px; margin:0 0 4px; color:#7dd3fc; }
  .sub { color:#6b7280; margin-bottom:20px; }
  table { width:100%; border-collapse:collapse; }
  th, td { text-align:left; padding:6px 10px; border-bottom:1px solid #1f242c; vertical-align:top; }
  th { color:#9ca3af; font-weight:600; position:sticky; top:0; background:#0b0d10; }
  td.status { color:#4ade80; }
  tr.err td.status { color:#f87171; }
  .args { color:#9ca3af; max-width:420px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .cards { display:flex; gap:12px; margin-bottom:20px; flex-wrap:wrap; }
  .card { border:1px solid #1f242c; border-radius:8px; padding:10px 14px; min-width:160px; }
  .card b { display:block; font-size:18px; color:#e5e7eb; }
  .card span { color:#6b7280; }
</style>
</head>
<body>
<h1>Neuron — MCP activity</h1>
<div class="sub">Every tool call routed through the Neuron proxy. <span id="updated"></span></div>
<div class="cards" id="cards"></div>
<table>
  <thead><tr><th>Time</th><th>Server</th><th>Method</th><th>Tool</th><th>Status</th><th>ms</th><th>Tokens</th><th>Args / error</th></tr></thead>
  <tbody id="rows"><tr><td colspan="8">No calls recorded yet.</td></tr></tbody>
</table>
<script>
async function refresh() {
  try {
    const calls = await fetch('/api/calls?limit=200').then(r => r.json());
    const stats = await fetch('/api/stats').then(r => r.json());
    renderStats(stats || []);
    renderCalls(calls || []);
    document.getElementById('updated').textContent = 'Updated ' + new Date().toLocaleTimeString();
  } catch (e) { /* keep the last good view */ }
}
function renderStats(stats) {
  const cards = document.getElementById('cards');
  if (!stats.length) { cards.innerHTML = ''; return; }
  cards.innerHTML = stats.map(function (s) {
    return '<div class="card"><b>' + esc(s.server) + '</b>' +
      '<span>' + s.calls + ' calls · ' + s.errors + ' errors</span><br>' +
      '<span>' + Math.round(s.avg_ms) + ' ms avg · ' + (s.tokens_in + s.tokens_out) + ' tokens</span></div>';
  }).join('');
}
function renderCalls(calls) {
  const rows = document.getElementById('rows');
  if (!calls.length) { rows.innerHTML = '<tr><td colspan="8">No calls recorded yet.</td></tr>'; return; }
  rows.innerHTML = calls.slice().reverse().map(function (c) {
    const detail = c.status === 'error' ? (c.error || '') : (c.args || '');
    return '<tr class="' + (c.status === 'error' ? 'err' : '') + '">' +
      '<td>' + new Date(c.time).toLocaleTimeString() + '</td>' +
      '<td>' + esc(c.server) + '</td>' +
      '<td>' + esc(c.method) + '</td>' +
      '<td>' + esc(c.tool || '') + '</td>' +
      '<td class="status">' + esc(c.status) + '</td>' +
      '<td>' + c.duration_ms + '</td>' +
      '<td>' + (c.tokens_in + c.tokens_out) + '</td>' +
      '<td class="args" title="' + esc(detail) + '">' + esc(detail) + '</td></tr>';
  }).join('');
}
function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, function (ch) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch];
  });
}
refresh();
setInterval(refresh, 2000);
</script>
</body>
</html>`
