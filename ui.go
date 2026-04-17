package main

import (
	"encoding/json"
	"html/template"
	"net/http"
)

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>LongCat Proxy Dashboard</title>
  <style>
    :root {
      --bg: #07111f;
      --bg2: #0f1d33;
      --card: rgba(14, 28, 52, 0.72);
      --line: rgba(113, 194, 255, 0.18);
      --text: #eef7ff;
      --sub: #8eabd0;
      --cyan: #57e3ff;
      --green: #54f7ae;
      --orange: #ffb45c;
      --red: #ff6d8f;
      --blue: #7aa8ff;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      color: var(--text);
      font-family: "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(87, 227, 255, 0.18), transparent 28%),
        radial-gradient(circle at 80% 20%, rgba(84, 247, 174, 0.12), transparent 22%),
        radial-gradient(circle at bottom right, rgba(122, 168, 255, 0.15), transparent 24%),
        linear-gradient(135deg, var(--bg), var(--bg2));
      min-height: 100vh;
      overflow-x: hidden;
    }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      background-image:
        linear-gradient(rgba(113, 194, 255, 0.05) 1px, transparent 1px),
        linear-gradient(90deg, rgba(113, 194, 255, 0.05) 1px, transparent 1px);
      background-size: 28px 28px;
      mask-image: linear-gradient(to bottom, rgba(0,0,0,0.8), transparent 85%);
      pointer-events: none;
    }
    .wrap {
      width: min(1440px, calc(100% - 40px));
      margin: 0 auto;
      padding: 28px 0 40px;
    }
    .hero {
      display: flex;
      justify-content: space-between;
      align-items: end;
      gap: 24px;
      margin-bottom: 24px;
    }
    .title h1 {
      margin: 0;
      font-size: clamp(32px, 4vw, 54px);
      letter-spacing: 0.06em;
      text-transform: uppercase;
      text-shadow: 0 0 24px rgba(87, 227, 255, 0.24);
    }
    .title p, .meta {
      margin: 8px 0 0;
      color: var(--sub);
      font-size: 14px;
    }
    .meta strong { color: var(--cyan); }
    .cards {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 16px;
      margin-bottom: 16px;
    }
    .card, .panel {
      position: relative;
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 22px;
      backdrop-filter: blur(20px);
      box-shadow:
        0 22px 50px rgba(2, 11, 25, 0.4),
        inset 0 1px 0 rgba(255, 255, 255, 0.04);
      overflow: hidden;
    }
    .card::after, .panel::after {
      content: "";
      position: absolute;
      inset: 0;
      background: linear-gradient(135deg, rgba(255,255,255,0.06), transparent 35%);
      pointer-events: none;
    }
    .card {
      padding: 18px 20px;
      min-height: 132px;
      display: flex;
      flex-direction: column;
      justify-content: space-between;
    }
    .label {
      color: var(--sub);
      font-size: 13px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
    }
    .value {
      font-size: clamp(28px, 3vw, 44px);
      font-weight: 700;
      line-height: 1.1;
      margin: 12px 0 8px;
    }
    .hint { color: var(--sub); font-size: 13px; }
    .accent-cyan { color: var(--cyan); }
    .accent-green { color: var(--green); }
    .accent-orange { color: var(--orange); }
    .accent-red { color: var(--red); }
    .accent-blue { color: var(--blue); }
    .grid {
      display: grid;
      grid-template-columns: 1.45fr 1fr;
      gap: 16px;
      margin-bottom: 16px;
    }
    .panel {
      padding: 20px;
      min-height: 280px;
    }
    .panel h3 {
      margin: 0 0 18px;
      font-size: 16px;
      letter-spacing: 0.08em;
      color: var(--sub);
      text-transform: uppercase;
    }
    .stat-list {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }
    .stat-item {
      padding: 14px 16px;
      border-radius: 16px;
      background: rgba(255,255,255,0.03);
      border: 1px solid rgba(255,255,255,0.05);
    }
    .stat-item b {
      display: block;
      font-size: 22px;
      margin-top: 8px;
    }
    .bars {
      display: grid;
      gap: 12px;
    }
    .bar-row {
      display: grid;
      grid-template-columns: 160px 1fr 70px;
      gap: 12px;
      align-items: center;
      font-size: 13px;
    }
    .bar-track {
      width: 100%;
      height: 12px;
      border-radius: 999px;
      overflow: hidden;
      background: rgba(255,255,255,0.05);
      border: 1px solid rgba(255,255,255,0.06);
    }
    .bar-fill {
      height: 100%;
      border-radius: inherit;
      background: linear-gradient(90deg, rgba(87,227,255,0.95), rgba(122,168,255,0.95));
      box-shadow: 0 0 18px rgba(87,227,255,0.35);
    }
    .logs {
      display: grid;
      gap: 10px;
      max-height: 440px;
      overflow: auto;
      padding-right: 4px;
    }
    .log {
      padding: 12px 14px;
      border-radius: 14px;
      background: rgba(255,255,255,0.03);
      border: 1px solid rgba(255,255,255,0.05);
      font-family: Consolas, "SFMono-Regular", monospace;
      font-size: 13px;
    }
    .log .time { color: var(--sub); }
    .log .level { margin: 0 8px; font-weight: 700; }
    .footer {
      color: var(--sub);
      text-align: right;
      font-size: 12px;
    }
    .auth-mask {
      position: fixed;
      inset: 0;
      display: none;
      align-items: center;
      justify-content: center;
      padding: 24px;
      background: rgba(2, 10, 24, 0.72);
      backdrop-filter: blur(16px);
      z-index: 20;
    }
    .auth-mask.show {
      display: flex;
    }
    .auth-card {
      width: min(460px, 100%);
      padding: 24px;
      border-radius: 24px;
      background: rgba(10, 21, 40, 0.9);
      border: 1px solid rgba(87, 227, 255, 0.25);
      box-shadow: 0 30px 80px rgba(0, 0, 0, 0.45);
    }
    .auth-card h2 {
      margin: 0 0 10px;
      font-size: 26px;
      letter-spacing: 0.05em;
    }
    .auth-card p {
      margin: 0 0 18px;
      color: var(--sub);
      line-height: 1.6;
      font-size: 14px;
    }
    .auth-field {
      width: 100%;
      padding: 14px 16px;
      border: 1px solid rgba(255, 255, 255, 0.08);
      border-radius: 14px;
      background: rgba(255, 255, 255, 0.04);
      color: var(--text);
      outline: none;
      font-size: 14px;
    }
    .auth-field:focus {
      border-color: rgba(87, 227, 255, 0.65);
      box-shadow: 0 0 0 4px rgba(87, 227, 255, 0.12);
    }
    .auth-actions {
      display: flex;
      gap: 12px;
      margin-top: 16px;
    }
    .auth-btn {
      border: 0;
      border-radius: 14px;
      padding: 12px 16px;
      font-size: 14px;
      cursor: pointer;
      color: var(--text);
      background: linear-gradient(90deg, rgba(87,227,255,0.9), rgba(122,168,255,0.95));
      box-shadow: 0 12px 26px rgba(87, 227, 255, 0.22);
    }
    .auth-btn.secondary {
      background: rgba(255,255,255,0.08);
      box-shadow: none;
    }
    .auth-error {
      min-height: 22px;
      margin-top: 12px;
      color: var(--red);
      font-size: 13px;
    }
    .meta .auth-state {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      color: var(--sub);
    }
    .meta .auth-state button {
      border: 0;
      border-radius: 999px;
      padding: 6px 10px;
      cursor: pointer;
      background: rgba(255,255,255,0.08);
      color: var(--text);
    }
    @media (max-width: 1100px) {
      .cards { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .grid { grid-template-columns: 1fr; }
    }
    @media (max-width: 680px) {
      .wrap { width: min(100% - 24px, 1440px); }
      .hero { flex-direction: column; align-items: start; }
      .cards, .stat-list { grid-template-columns: 1fr; }
      .bar-row { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <section class="hero">
      <div class="title">
        <h1>LongCat Proxy Command</h1>
        <p>炫酷监控大屏，实时展示 Key 池状态、请求频率与 Token 消耗</p>
      </div>
      <div class="meta">
        <div>上游格式: <strong id="upstream">-</strong></div>
        <div>最后刷新: <strong id="updatedAt">-</strong></div>
        <div class="auth-state">前端认证: <strong id="authState">未连接</strong> <button id="logoutBtn" type="button">清除 Key</button></div>
      </div>
    </section>

    <section class="cards">
      <article class="card">
        <div class="label">总 Key</div>
        <div class="value accent-cyan" id="totalKeys">0</div>
        <div class="hint">来自 key.txt 的实时统计</div>
      </article>
      <article class="card">
        <div class="label">活跃</div>
        <div class="value accent-green" id="activeKeys">0</div>
        <div class="hint">当前可直接轮询使用</div>
      </article>
      <article class="card">
        <div class="label">冷却中</div>
        <div class="value accent-orange" id="cooldownKeys">0</div>
        <div class="hint">触发频控或瞬时异常</div>
      </article>
      <article class="card">
        <div class="label">禁用</div>
        <div class="value accent-red" id="disabledKeys">0</div>
        <div class="hint">认证失败后不再参与轮询</div>
      </article>
    </section>

    <section class="cards">
      <article class="card">
        <div class="label">RPM</div>
        <div class="value accent-blue" id="rpm">0</div>
        <div class="hint">最近一分钟请求数</div>
      </article>
      <article class="card">
        <div class="label">总请求</div>
        <div class="value accent-cyan" id="totalRequests">0</div>
        <div class="hint">累计进入代理的请求</div>
      </article>
      <article class="card">
        <div class="label">总输入 Token</div>
        <div class="value accent-green" id="inputTokens">0</div>
        <div class="hint">已累计的 prompt tokens</div>
      </article>
      <article class="card">
        <div class="label">总输出 Token</div>
        <div class="value accent-orange" id="outputTokens">0</div>
        <div class="hint">已累计的 completion tokens</div>
      </article>
    </section>

    <section class="grid">
      <article class="panel">
        <h3>核心指标</h3>
        <div class="stat-list">
          <div class="stat-item"><span class="label">成功请求</span><b class="accent-green" id="successRequests">0</b></div>
          <div class="stat-item"><span class="label">失败请求</span><b class="accent-red" id="failedRequests">0</b></div>
          <div class="stat-item"><span class="label">运行时长</span><b class="accent-cyan" id="uptime">0s</b></div>
          <div class="stat-item"><span class="label">成功率</span><b class="accent-blue" id="successRate">0%</b></div>
        </div>
      </article>
      <article class="panel">
        <h3>HTTP 状态分布</h3>
        <div class="bars" id="statusBars"></div>
      </article>
    </section>

    <section class="grid">
      <article class="panel">
        <h3>模型调用热度</h3>
        <div class="bars" id="modelBars"></div>
      </article>
      <article class="panel">
        <h3>实时日志</h3>
        <div class="logs" id="logs"></div>
      </article>
    </section>

    <div class="footer">Powered by LongCat API2API Dashboard</div>
  </div>

  <div class="auth-mask" id="authMask">
    <div class="auth-card">
      <h2>Dashboard Access</h2>
      <p>页面本身可以打开，但统计数据需要前端携带 API Key。输入后会仅保存在当前浏览器的本地存储中。</p>
      <input class="auth-field" id="apiKeyInput" type="password" placeholder="输入 Bearer API Key 或 X-API-Key 对应值">
      <div class="auth-actions">
        <button class="auth-btn" id="saveKeyBtn" type="button">连接大屏</button>
        <button class="auth-btn secondary" id="cancelKeyBtn" type="button">暂不连接</button>
      </div>
      <div class="auth-error" id="authError"></div>
    </div>
  </div>

  <script>
    const nf = new Intl.NumberFormat("zh-CN");
    const storageKey = "longcat-dashboard-api-key";
    const authMask = document.getElementById("authMask");
    const apiKeyInput = document.getElementById("apiKeyInput");
    const authError = document.getElementById("authError");
    const authState = document.getElementById("authState");
    const logoutBtn = document.getElementById("logoutBtn");
    const saveKeyBtn = document.getElementById("saveKeyBtn");
    const cancelKeyBtn = document.getElementById("cancelKeyBtn");

    function getSavedApiKey() {
      return localStorage.getItem(storageKey) || "";
    }

    function setSavedApiKey(value) {
      if (value) {
        localStorage.setItem(storageKey, value);
      } else {
        localStorage.removeItem(storageKey);
      }
      updateAuthState();
    }

    function updateAuthState() {
      authState.textContent = getSavedApiKey() ? "已认证" : "未连接";
    }

    function showAuth(message) {
      if (message) {
        authError.textContent = message;
      }
      const shouldPrefill = !authMask.classList.contains("show") || !apiKeyInput.value.trim();
      if (shouldPrefill) {
        apiKeyInput.value = getSavedApiKey();
      }
      authMask.classList.add("show");
      updateAuthState();
      if (document.activeElement !== apiKeyInput) {
        setTimeout(function() { apiKeyInput.focus(); }, 0);
      }
    }

    function hideAuth() {
      authError.textContent = "";
      authMask.classList.remove("show");
      updateAuthState();
    }

    function renderBars(el, rows, emptyText) {
      if (!rows || !rows.length) {
        el.innerHTML = '<div class="hint">' + emptyText + '</div>';
        return;
      }
      const max = Math.max(...rows.map(x => x.value), 1);
      el.innerHTML = rows.map(function(row) {
        const width = Math.max(8, row.value / max * 100);
        return '<div class="bar-row">' +
          '<div>' + escapeHtml(row.name) + '</div>' +
          '<div class="bar-track"><div class="bar-fill" style="width:' + width + '%"></div></div>' +
          '<div>' + nf.format(row.value) + '</div>' +
        '</div>';
      }).join("");
    }

    function renderLogs(el, logs) {
      if (!logs || !logs.length) {
        el.innerHTML = '<div class="hint">暂无日志</div>';
        return;
      }
      el.innerHTML = logs.map(function(item) {
        return '<div class="log">' +
          '<span class="time">[' + escapeHtml(formatTime(item.time)) + ']</span>' +
          '<span class="level">' + escapeHtml(item.level) + '</span>' +
          '<span>' + escapeHtml(item.message) + '</span>' +
        '</div>';
      }).join("");
    }

    function escapeHtml(input) {
      return String(input ?? "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;");
    }

    function formatTime(iso) {
      const d = new Date(iso);
      if (Number.isNaN(d.getTime())) return iso;
      return d.toLocaleString("zh-CN", { hour12: false });
    }

    function formatDuration(seconds) {
      const s = Math.max(0, Math.floor(seconds));
      const h = Math.floor(s / 3600);
      const m = Math.floor((s % 3600) / 60);
      const ss = s % 60;
      if (h > 0) return h + "h " + m + "m " + ss + "s";
      if (m > 0) return m + "m " + ss + "s";
      return ss + "s";
    }

    async function refresh() {
      const resp = await fetch("/api/stats", { cache: "no-store", credentials: "same-origin" });
      if (resp.status === 401) {
        showAuth("请输入有效的 API Key 后再加载统计数据");
        return;
      }
      if (!resp.ok) {
        throw new Error("load stats failed: " + resp.status);
      }
      const data = await resp.json();
      hideAuth();

      document.getElementById("upstream").textContent = data.upstream_format || "-";
      document.getElementById("updatedAt").textContent = formatTime(data.timestamp);
      document.getElementById("totalKeys").textContent = nf.format(data.total_keys || 0);
      document.getElementById("activeKeys").textContent = nf.format(data.active_keys || 0);
      document.getElementById("cooldownKeys").textContent = nf.format(data.cooldown_keys || 0);
      document.getElementById("disabledKeys").textContent = nf.format(data.disabled_keys || 0);
      document.getElementById("rpm").textContent = nf.format(data.rpm || 0);
      document.getElementById("totalRequests").textContent = nf.format(data.total_requests || 0);
      document.getElementById("inputTokens").textContent = nf.format(data.total_input_tokens || 0);
      document.getElementById("outputTokens").textContent = nf.format(data.total_output_tokens || 0);
      document.getElementById("successRequests").textContent = nf.format(data.success_requests || 0);
      document.getElementById("failedRequests").textContent = nf.format(data.failed_requests || 0);
      document.getElementById("uptime").textContent = formatDuration(data.uptime_seconds || 0);

      const total = (data.success_requests || 0) + (data.failed_requests || 0);
      const rate = total > 0 ? (((data.success_requests || 0) / total) * 100).toFixed(2) : "0.00";
      document.getElementById("successRate").textContent = rate + "%";

      renderBars(document.getElementById("statusBars"), data.status_code_usage, "暂无状态码数据");
      renderBars(document.getElementById("modelBars"), data.model_usage, "暂无模型调用数据");
      renderLogs(document.getElementById("logs"), data.recent_logs);
    }

    saveKeyBtn.addEventListener("click", function() {
      const value = apiKeyInput.value.trim();
      if (!value) {
        authError.textContent = "API Key 不能为空";
        return;
      }
      fetch("/api/auth/login", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json"
        },
        body: JSON.stringify({ api_key: value })
      })
        .then(function(resp) {
          if (!resp.ok) {
            throw new Error(resp.status === 401 ? "API Key 不正确" : "登录失败: " + resp.status);
          }
          setSavedApiKey(value);
          return refresh();
        })
        .catch(function(err) {
          authError.textContent = err && err.message ? err.message : "认证失败";
        });
    });

    cancelKeyBtn.addEventListener("click", function() {
      hideAuth();
    });

    logoutBtn.addEventListener("click", function() {
      fetch("/api/auth/logout", {
        method: "POST",
        credentials: "same-origin"
      }).finally(function() {
        setSavedApiKey("");
        showAuth("已清除本地登录状态");
      });
    });

    apiKeyInput.addEventListener("keydown", function(event) {
      if (event.key === "Enter") {
        saveKeyBtn.click();
      }
    });

    updateAuthState();
    refresh().catch(function(err) {
      console.error(err);
      if (!getSavedApiKey()) {
        showAuth("输入 API Key 后即可加载大屏数据");
      }
    });
    setInterval(() => refresh().catch(console.error), 3000);
  </script>
</body>
</html>`))

func dashboardHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTemplate.Execute(w, nil)
}

func statsHandler(stats *StatsTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats.Snapshot())
	}
}
