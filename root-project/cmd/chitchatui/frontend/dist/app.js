const state = {
  common: {loaded:false, snapshot:null, source:"", token:0, running:false},
  executionRequest:0,
  snapshot: null,
  show: {loaded:false, dirty:false, pending:false, known:false, revision:0, snapshot:null, request:0, edits:0},
  diagnostics: { selected:"", chatRun:"", conversation:"", all:false, token:0, snapshot:null, key:"", rendered:"", busy:false },
  currentTab: "home",
  browserFormLoaded: false,
  browserRunPending: false,
  browserActivityVersion: 0,
  chat: { version: -1, selected: "", manual: false, pending: false, dirty: false, token: 0, rendered: "" },
};

function backend() {
  return window.go && window.go.main && window.go.main.App;
}

async function invoke(method, ...args) {
  try {
    const api = backend();
    if (!api || typeof api[method] !== "function") {
      throw new Error(`Wails binding not ready: ${method}`);
    }
    return await api[method](...args);
  } catch (error) {
    reportFrontendError(`Backend ${method} failed`, error);
    throw error;
  }
}

function runtimeEvents() {
  return window.runtime || {};
}

// Never serialize backend arguments: settings can contain credentials.
function reportFrontendError(context, error) {
  try {
    const runtime = runtimeEvents();
    if (typeof runtime.LogError === "function") {
      const detail = error && error.stack ? error.stack : String(error);
      runtime.LogError(`Frontend | ${context} | ${detail}`);
    }
  } catch (_) { /* Do not recurse when the logging bridge is unavailable. */ }
}

function setRunning(running) {
  state.common.running=!!running;
  const pill = document.getElementById("running-pill");
  pill.textContent = running ? "Running" : "Stopped";
  pill.className = `pill ${running ? "running" : "stopped"}`;
  renderSharedLock();refreshExecutionPreview();
  const cleanButton = document.getElementById("clean-profiles");
  if (cleanButton) {
    cleanButton.disabled = !!running || !!state.show.snapshot?.browsers?.some(b => b.status !== "closed");
    cleanButton.title = running ? "Stop the bot before cleaning profile data." : "";
  }
}

// The transcript has its own event/API; it never parses technical log text.
function renderChatTranscript(snapshot) {
  const box = document.getElementById("chat-messages");
  const select = document.getElementById("chat-session");
  const follow = box.scrollHeight - box.scrollTop - box.clientHeight < 64;
  const oldTop = box.scrollTop;
  const changedConversation = state.chat.rendered !== snapshot.selected;
  select.replaceChildren();
  const automatic = document.createElement("option");
  automatic.value = ""; automatic.textContent = "Latest active chat"; select.append(automatic);
  const sessions = snapshot.sessions || [];
  for (const session of sessions) {
    const option = document.createElement("option");
    option.value = session.id;
    option.textContent = session.label + (session.active ? "" : " (ended)");
    select.append(option);
  }
  if (state.chat.manual && !sessions.some(s => s.id === state.chat.selected)) {
    state.chat.manual = false; state.chat.selected = "";
  }
  select.value = state.chat.manual ? state.chat.selected : "";
  select.disabled = sessions.length === 0;
  const current = sessions.find(s => s.id === snapshot.selected);
  const nextRun=current?.diagnostic_run || "", nextConversation=current?.conversation || "";
  if (state.diagnostics.chatRun!==nextRun || state.diagnostics.conversation!==nextConversation) {
    state.diagnostics.chatRun=nextRun;state.diagnostics.conversation=nextConversation;
    hideDiagnosticExport();
    queueMicrotask(refreshHomeDiagnostics);
  }
  const note = document.getElementById("chat-history-note");
  const notices = [];
  if (current && current.dropped) notices.push(`${current.dropped} older messages outside the retained display window.`);
  if (snapshot.evicted) notices.push(`${snapshot.evicted} older conversations no longer retained.`);
  note.textContent = notices.join(" "); note.hidden = notices.length === 0;
  const fragment = document.createDocumentFragment();
  const seen = new Set();
  for (const message of [...(snapshot.messages || [])].sort((a,b) => a.order - b.order)) {
    if (!message.id || seen.has(message.id) || !["user", "bot"].includes(message.role)) continue;
    seen.add(message.id);
    const row = document.createElement("div");
    row.className = `home-chat-row home-chat-${message.role}`;
    row.dataset.messageId = message.id;
    const label = document.createElement("strong");
    label.textContent = message.role === "user" ? "User:" : "Bot:";
    const text = document.createElement("span");
    text.textContent = message.text; // Never innerHTML; messages may contain markup.
    row.append(label, text); fragment.append(row);
  }
  if (!fragment.childNodes.length) {
    const empty = document.createElement("p"); empty.className = "home-chat-empty";
    empty.textContent = "Waiting for identified browser messages."; fragment.append(empty);
  }
  box.replaceChildren(fragment);
  state.chat.rendered = snapshot.selected || "";
  box.scrollTop = changedConversation || follow ? box.scrollHeight : oldTop;
}

async function refreshChatTranscript() {
  // Fake/older backends need not expose the new optional display API.
  if (!backend() || typeof backend().GetLiveChatSnapshot !== "function") return;
  if (state.chat.pending) { state.chat.dirty = true; return; }
  state.chat.pending = true; state.chat.dirty = false;
  const token = ++state.chat.token;
  try {
    const snapshot = await invoke("GetLiveChatSnapshot", state.chat.manual ? state.chat.selected : "");
    if (token === state.chat.token && snapshot.version >= state.chat.version) {
      state.chat.version = snapshot.version;
      renderChatTranscript(snapshot);
    }
  } catch (_) { /* invoke already forwards diagnostics to logs.txt. */ }
  finally {
    state.chat.pending = false;
    if (state.chat.dirty) { state.chat.dirty = false; queueMicrotask(refreshChatTranscript); }
  }
}

function browserStatus(message, isError = false) {
  const status = document.getElementById("browser-status");
  status.textContent = `Status: ${message}`;
  status.classList.toggle("browser-error", isError);
}

function setBrowserRunPending(pending) {
  state.browserRunPending = pending;
  document.querySelectorAll(".browser-run").forEach((button) => {
    button.disabled = pending || (button.dataset.backend === "show_mode" && (state.show.dirty || state.show.pending));
  });
  document.getElementById("browser-options").setAttribute("aria-busy", String(pending));
}

async function runBrowserOption(option) {
  if (state.browserRunPending) return;
  if (option.key === "show_mode" && (state.show.dirty || state.show.pending)) { browserStatus("Save shared settings before opening a browser.",true); return; }
  const count = Number(document.getElementById("fresh-browser-count").value);
  if (!Number.isInteger(count) || count < 1 || count > 5) {
    browserStatus("Browser count must be between 1 and 5.", true);
    return;
  }
  const settings = {
    backend: option.key,
    profile: document.getElementById("browser-profile").value.trim() || "default",
    headless: option.key === "show_mode" ? false : document.getElementById("browser-headless").checked,
  };
  setBrowserRunPending(true);
  const activityVersion = state.browserActivityVersion;
  browserStatus(`Requesting ${count} fresh ${option.label} browser(s)...`);
  try {
    await invoke("RunBrowser", settings, count);refreshExecutionPreview();
    // A fast worker may already have emitted a failure or login status.
    if (state.browserActivityVersion === activityVersion) {
      browserStatus(`${option.label}: launch request accepted (${count}). Waiting for browser activity...`);
    }
  } catch (error) {
    browserStatus(error.message || String(error), true);
  } finally {
    setBrowserRunPending(false);
  }
}

function renderBrowserOptions(options) {
  const root = document.getElementById("browser-options");
  root.innerHTML = "";
  document.getElementById("browser-available").textContent = `Available: ${options.length}`;
  options.forEach((option, index) => {
    const row = document.createElement("div");
    row.className = "browser-choice browser-run-row";
    row.innerHTML = `
      <span class="browser-number">${String(index + 1).padStart(2, "0")}</span>
      <div class="browser-details">
        <div class="browser-choice-title">${escapeHtml(option.label)}</div>
        <div class="browser-choice-description">${escapeHtml(option.description)}</div>
      </div>
      <button type="button" class="primary-button browser-run" aria-label="Run ${escapeHtml(option.label)}">Run</button>
    `;
    row.querySelector("button").dataset.backend = option.key;
    if (option.key === "show_mode") {row.querySelector("button").textContent = "Open Browser / Login";row.querySelector("button").setAttribute("aria-label","Open Show Mode browser for login");}
    row.querySelector("button").addEventListener("click", () => runBrowserOption(option));
    root.appendChild(row);
  });
  setBrowserRunPending(state.browserRunPending);
}

function renderHomeStats(stats) {
  const home = stats || {};
  document.getElementById("stat-bot-accounts").textContent = String(home.bot_accounts || 0);
  document.getElementById("stat-running-accounts").textContent = String(home.running_accounts || 0);
  document.getElementById("stat-user-send").textContent = String(home.user_send_count || 0);
  document.getElementById("stat-traffic-users").textContent = String(home.traffic_users || 0);
  document.getElementById("stat-snap-tg-user").textContent = String(home.snap_tg_sent_count || 0);
  document.getElementById("stat-messages-sent").textContent = String(home.messages_sent || 0);
  document.getElementById("stat-dead-accounts").textContent = String(home.dead_accounts || 0);
  document.getElementById("stat-skipped-accounts").textContent = String(home.skipped_accounts || 0);
}

function evaAISettings() {
  return {
    ai_enabled: document.getElementById("evachat-ai-enabled").checked,
    ai_base_url: document.getElementById("evachat-ai-base-url").value.trim(),
    ai_model: document.getElementById("evachat-ai-model").value.trim(),
    ai_api_key: document.getElementById("evachat-ai-api-key").value.trim(),
  };
}

function setChatMode(mode) {
  const validMode = (mode === "folder" || mode === "eva_chat_flow") ? mode : "single";
  ["single", "folder", "evachat"].forEach((m) => {
    const radio = document.getElementById(`chat-mode-${m}`);
    const label = document.getElementById(`mode-option-${m}`);
    const section = document.getElementById(`section-${m}-mode`);
    if (radio) radio.checked = ((m === "evachat" ? "eva_chat_flow" : m) === validMode);
    if (label) label.classList.toggle("active", (m === "evachat" ? "eva_chat_flow" : m) === validMode);
    if (section) section.style.display = ((m === "evachat" ? "eva_chat_flow" : m) === validMode) ? "block" : "none";
  });
  const useLoaded = document.getElementById("use-loaded-sequences");
  if (useLoaded) useLoaded.checked = (validMode === "folder");
}

function fillForms(snapshot) {
  // Subsequent state refreshes must not discard current, unsaved form values.
  if (!state.browserFormLoaded) {
    document.getElementById("browser-profile").value = snapshot.browser.profile || "default";
    document.getElementById("browser-headless").checked = !!snapshot.browser.headless;
    state.browserFormLoaded = true;
  }
  
  const currentMode = snapshot.message.chat_mode || ((snapshot.message.use_loaded_sequences && snapshot.message.loaded_sequences && snapshot.message.loaded_sequences.length > 0) ? "folder" : "single");
  setChatMode(currentMode);

  document.getElementById("loaded-sequences-status").textContent = snapshot.message.loaded_sequences ? `${snapshot.message.loaded_sequences.length} sequences loaded` : "0 sequences loaded";
  document.getElementById("message-sequence").value = snapshot.message.sequence || "";
  const evaStatus = document.getElementById("evachat-status");
  if (evaStatus) evaStatus.textContent = currentMode === "eva_chat_flow" ? "EVAChat Flow will load the editable data/eva_chat TXT pack when the bot starts." : "TXT data is initialized when EVAChat Flow starts.";
  document.getElementById("message-usernames").value = snapshot.message.usernames || "";
  document.getElementById("evachat-ai-enabled").checked = !!snapshot.message.ai_enabled;
  document.getElementById("evachat-ai-base-url").value = snapshot.message.ai_base_url || "";
  document.getElementById("evachat-ai-model").value = snapshot.message.ai_model || "";
  document.getElementById("evachat-ai-api-key").value = snapshot.message.ai_api_key || "";
  document.getElementById("proxy-enabled").checked = !!snapshot.proxy.enabled;
  document.getElementById("proxy-entries").value = snapshot.proxy.entries || "";

  renderBrowserOptions(snapshot.browser_options || []);
  renderHomeStats(snapshot.home);
  setRunning(!!snapshot.running);
  if (snapshot.license_invalid) {
    const overlay = document.getElementById('license-overlay');
    overlay.style.display = 'flex';
    document.getElementById('license-message').textContent = snapshot.license_reason || "Your license is invalid or expired.";
    document.getElementById('license-key-input').value = snapshot.license_key || "UNKNOWN";
    return;
  }

  document.querySelector('.shell').classList.add('visible');

  const messageLineCount = String(snapshot.message.sequence || "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean).length;
  document.getElementById("home-summary").textContent =
    `Backend: ${snapshot.browser.backend} | Profiles auto-start | Message steps: ${messageLineCount}`;
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

async function refreshState() {
  const snapshot = await invoke("GetInitialState");
  state.snapshot = snapshot;
  fillForms(snapshot);
  await refreshChatTranscript();
  await refreshSharedSettings();
}

async function autoRefreshStats() {
  try {
    const snapshot = await invoke("GetInitialState");
    renderHomeStats(snapshot.home);
    setRunning(!!snapshot.running);
  } catch (err) {
    // ignore
  }
}

function activateTab(tabName) {
  state.currentTab = tabName;

  document.querySelectorAll(".nav-button").forEach((button) => {
    button.classList.toggle("active", button.dataset.tab === tabName);
  });
  document.querySelectorAll(".tab-panel").forEach((panel) => {
    panel.classList.toggle("active", panel.dataset.panel === tabName);
  });
}

function bindTabs() {
  document.querySelectorAll(".nav-button").forEach((button) => {
    button.addEventListener("click", () => activateTab(button.dataset.tab));
  });
}

function proxyInputLines() {
  const raw = document.getElementById("proxy-entries").value;
  if (document.getElementById("proxy-default-scheme").value !== "socks5") return raw;
  return raw.split(/\r?\n/).map(line => {
    const text = line.trim();
    return text && !/^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? "socks5://" + text : line;
  }).join("\n");
}

async function testProxyEntries() {
  const button = document.getElementById("test-proxy");
  if (button.disabled) return;
  const raw = proxyInputLines();
  const status = document.getElementById("proxy-test-status");
  const results = document.getElementById("proxy-test-results");
  results.replaceChildren();
  if (!raw.trim()) { status.textContent = "Enter at least one proxy to test."; return; }
  button.disabled = true;
  button.textContent = "Testing…";
  status.textContent = "Testing proxy authentication and HTTPS access… (up to 12 seconds per proxy; four at a time)";
  try {
    const rows = await invoke("TestProxyEntries", raw);
    let passed = 0, reachable = 0;
    for (const row of rows || []) {
      if (row.ok) passed++;
      if (row.tunnel_ok) reachable++;
      const item = document.createElement("p");
      item.className = row.ok ? "proxy-test-pass" : row.tunnel_ok ? "proxy-test-warn" : "proxy-test-fail";
      // textContent only: proxy/provider text is never treated as markup.
      item.textContent = `Line ${row.line}: ${row.ok ? "PASS" : row.tunnel_ok ? "REACHABLE / SITE RESTRICTED" : "FAIL"} — ${row.label || "Invalid entry"} — ${row.latency_ms} ms${row.status ? ` — HTTP ${row.status}` : ""} — ${row.detail}`;
      if (row.provider_message) item.textContent += `\nProvider said: “${row.provider_message}”`;
      if (row.proxy_authenticate) item.textContent += `\nAuth challenge: ${row.proxy_authenticate}`;
      if (row.hint) item.textContent += `\nNext step: ${row.hint}`;
      results.appendChild(item);
    }
    status.textContent = `${passed}/${(rows || []).length} passed; ${reachable} HTTPS tunnels verified. A successful test is not a browser-login guarantee.`;
    if (proxyInputLines() !== raw) status.textContent += " List edited: these results are for the previous tested values; test again.";
  } catch (error) {
    status.textContent = error.message || String(error);
  } finally {
    button.disabled = false;
    button.textContent = "Test Proxy";
  }
}

function bindForms() {
  document.getElementById("test-proxy").addEventListener("click", testProxyEntries);
  // Enter must not submit/reload the page or implicitly choose a backend.
  document.getElementById("browser-form").addEventListener("submit", (event) => {
    event.preventDefault();
  });

  document.querySelectorAll('input[name="chat-mode"]').forEach((radio) => {
    radio.addEventListener("change", (e) => {
      setChatMode(e.target.value);
    });
  });

  ["single", "folder", "evachat"].forEach((mode) => {
    const label = document.getElementById(`mode-option-${mode}`);
    if (label) {
      label.addEventListener("click", () => {
        setChatMode(mode === "evachat" ? "eva_chat_flow" : mode);
      });
    }
  });

  document.getElementById("load-folder-btn").addEventListener("click", async () => {
    const summary = document.getElementById("home-summary");
    try {
      const sequences = await invoke("LoadMessageFolder");
      if (sequences && sequences.length > 0) {
        summary.textContent = `Loaded ${sequences.length} message sequences from folder.`;
        setChatMode("folder");
        await refreshState();
        setChatMode("folder");
      }
    } catch (error) {
      summary.textContent = error.message || String(error);
    }
  });

  document.getElementById("clean-loaded-btn").addEventListener("click", async () => {
    const summary = document.getElementById("home-summary");
    try {
      await invoke("ClearLoadedMessages");
      summary.textContent = "Loaded sequences cleared from DB.";
      setChatMode("single");
      await refreshState();
      setChatMode("single");
    } catch (error) {
      summary.textContent = error.message || String(error);
    }
  });

  document.getElementById("evachat-ai-test-btn").addEventListener("click", async () => {
    const status = document.getElementById("evachat-ai-status");
    status.textContent = "Testing AI API...";
    try {
      const result = await invoke("TestEVAChatAI", {
        chat_mode: "eva_chat_flow",
        sequence: document.getElementById("message-sequence").value,
        usernames: document.getElementById("message-usernames").value,
        ...evaAISettings(),
      });
      status.textContent = result || "AI OK";
    } catch (error) {
      status.textContent = "AI test failed: " + (error.message || String(error));
    }
  });

  document.getElementById("open-evachat-folder-btn").addEventListener("click", async () => {
    const summary = document.getElementById("home-summary");
    try {
      const result = await invoke("OpenEVAChatDataFolder");
      if (result) summary.textContent = result;
    } catch (error) {
      if (summary) summary.textContent = error.message || String(error);
    }
  });

  document.getElementById("message-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const selectedMode = document.querySelector('input[name="chat-mode"]:checked')?.value || "single";
    await invoke("SaveMessageSettings", {
      chat_mode: selectedMode,
      use_loaded_sequences: selectedMode === "folder",
      sequence: document.getElementById("message-sequence").value,
      usernames: document.getElementById("message-usernames").value,
      ...evaAISettings(),
    });
    await refreshState();
    setChatMode(selectedMode);
  });

  document.getElementById("proxy-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const summary = document.getElementById("home-summary");
    try {
      await invoke("SaveProxySettings", {
        enabled: document.getElementById("proxy-enabled").checked,
        entries: proxyInputLines(),
      });
      await refreshState();
    } catch (error) {
      summary.textContent = error.message || String(error);
    }
  });

  bindTimingSettings();
  document.getElementById("general-form").addEventListener("submit", saveSharedSettings);
}

function bindActions() {
  document.getElementById("start-bot").addEventListener("click", async () => {
    if (!sharedSettingsReady()) {document.getElementById("standard-save-status").textContent="Save shared settings before Start.";activateTab("setting");return;}
    await invoke("StartBot");
    await refreshState();
  });

  document.getElementById("stop-bot").addEventListener("click", async () => {
    await invoke("StopBot");
    await refreshState();
  });

  document.getElementById("clean-profiles").addEventListener("click", async () => {
    if(!window.confirm("Remove saved browser profiles and login sessions? This cannot be undone."))return;
    const summary = document.getElementById("home-summary");
    try {
      await invoke("CleanProfiles");
      await refreshState();
      summary.textContent = "Profile data cleaned. Login profiles and worker ownership data were removed.";
    } catch (error) {
      summary.textContent = error.message || String(error);
    }
  });

  document.getElementById("refresh-state").addEventListener("click", async () => {
    await refreshState();
  });

  document.getElementById("clear-logs").addEventListener("click", async () => {
    await invoke("ClearLogs");
    await refreshState();
  });


}

function bindEvents() {
  const runtime = runtimeEvents();
  document.getElementById("chat-session").addEventListener("change", (event) => {
    state.chat.manual = !!event.target.value;
    state.chat.selected = event.target.value;
    state.chat.token++; // Ignore an older in-flight response for another selection.
    refreshChatTranscript();
  });

  if (typeof runtime.EventsOn === "function") {
    runtime.EventsOn("ui:chat_changed", (event) => {
      if (event && event.version > state.chat.version) refreshChatTranscript();
    });
    runtime.EventsOn("ui:log", (entry) => {
      if (typeof entry.message === "string" && /\[[^\]]+ fresh \d+\/\d+\]/.test(entry.message)
          && !entry.message.includes("] Removed temporary browser data from ")) {
        state.browserActivityVersion += 1;
        browserStatus(entry.message, entry.level === "error");
      }
    });
    runtime.EventsOn("ui:running", (running) => {
      setRunning(!!running);
    });
    runtime.EventsOn("ui:license_invalid", (data) => {
      document.querySelector('.shell').classList.remove('visible');
      const overlay = document.getElementById('license-overlay');
      overlay.style.display = 'flex';
      document.getElementById('license-message').textContent = data.reason || "Your license became invalid or expired.";
      document.getElementById('license-key-input').value = data.key || "UNKNOWN";
    });
  }

  const copyBtn = document.getElementById('copy-license-btn');
  if (copyBtn) {
    copyBtn.addEventListener('click', () => {
      const input = document.getElementById('license-key-input');
      input.select();
      input.setSelectionRange(0, 99999);
      navigator.clipboard.writeText(input.value).then(() => {
        const originalText = copyBtn.textContent;
        copyBtn.textContent = 'Copied!';
        setTimeout(() => copyBtn.textContent = originalText, 2000);
      });
    });
  }
}

async function bootstrap() {
  bindTabs();
  bindForms();
  bindActions();
  bindWorkspace();
  bindHomeDiagnostics();
  bindEvents();
  activateTab("home");

  let attempts = 0;
  while (attempts < 50) {
    attempts += 1;
    try {
      await refreshState();
      await refreshShowControl();
      setInterval(refreshShowControl, 1000);
      setInterval(autoRefreshStats, 3000);
      await refreshHomeDiagnostics();
      setInterval(refreshHomeDiagnostics, 1000);
      setInterval(refreshChatTranscript, 1000); // replay after missed events / closure
      return;
    } catch (error) {
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
  }

  document.getElementById("home-summary").textContent = "Failed to connect to Wails bindings.";
}

window.addEventListener("error", (event) => {
  reportFrontendError("Uncaught error", event.error || event.message);
});
window.addEventListener("unhandledrejection", (event) => {
  reportFrontendError("Unhandled promise rejection", event.reason);
});

window.addEventListener("DOMContentLoaded", () => {
  bootstrap().catch((error) => {
    reportFrontendError("Bootstrap failed", error);
    document.getElementById("home-summary").textContent = error.message || String(error);
  });
});

function validateTimingPairs(pairs) {
  for (const [minID, maxID] of pairs) {
    const min = Number(document.getElementById(minID).value);
    const max = Number(document.getElementById(maxID).value);
    if (!Number.isInteger(min) || !Number.isInteger(max) || min < 0 || max < min || max > 86400) {
      throw new Error("Use whole seconds: 0 ≤ min ≤ max ≤ 86400.");
    }
  }
}
function bindTimingSettings() {
 for(const id of ["standard-policy-mode","standard-policy-timeout"])document.getElementById(id).addEventListener("change",updateStandardPolicy);
  document.getElementById("refresh-show-control").addEventListener("click",refreshShowControl);
  document.getElementById("settings-source").addEventListener("change",event=>chooseSharedSource(event.target.value));
  for(const event of ["input","change"])document.getElementById("general-form").addEventListener(event,()=>{
    state.show.dirty=true;state.show.edits++;
    document.getElementById("standard-save-status").textContent="Unsaved changes — Save before starting either mode.";
    renderShowControl();
  });
}

async function refreshShowControl() {
  const request = ++state.show.request;
  try {
    const snapshot = await invoke("GetShowControlState");
    if (request !== state.show.request) return;
    state.show.snapshot = snapshot; state.show.revision = snapshot.revision; state.show.known = true;
    document.getElementById("show-control-status").textContent = snapshot.active ? "Chat is active. Stop it before saving settings." : "Login stays idle until you press Start.";
  } catch (error) {
    if (request !== state.show.request) return;
    state.show.known = false;
    document.getElementById("show-control-status").textContent = "Status unavailable: " + (error.message || String(error)) + ". Refresh before Start.";
  }
  renderShowControl();
}
function showSettingsSummary(settings) {
  if (!settings) return "Chat is stopped. Saved settings will be loaded at Start.";
  return `Running settings: Reply delay: ${settings.reply_delay_min_seconds}–${settings.reply_delay_max_seconds}s · Next chat wait: ${settings.next_chat_min_seconds}–${settings.next_chat_max_seconds}s · User silent timeout: ${settings.no_reply_min_seconds}–${settings.no_reply_max_seconds}s`;
}
function renderShowControl() {
  const snap = state.show.snapshot;
  renderSharedLock();
  setBrowserRunPending(state.browserRunPending);
  const pill = document.getElementById("show-status-pill");
  pill.textContent = snap?.active ? "Active" : "Stopped";
  pill.className = snap?.active ? "pill running" : "pill stopped";
  const renderKey = JSON.stringify([snap?.browsers, state.show.pending, state.show.known, state.show.dirty, state.show.revision, sharedSettingsReady()]);
  if (state.show.renderKey === renderKey) return;
  state.show.renderKey = renderKey;
  const box = document.getElementById("show-sessions"); box.replaceChildren();
  for (const browser of snap?.browsers || []) {
    const card = document.createElement("article"); card.className = "show-session-card";
    const title=document.createElement("strong");title.textContent=browser.profile;
    const status=document.createElement("p");status.textContent=`${browser.status} · ${browser.account || "Waiting for login"}`;
    card.append(title,status);
    if(browser.blocked_by){const note=document.createElement("p");note.textContent=`Account in use: ${browser.blocked_by}`;card.append(note);}
    const details=document.createElement("details"),summary=document.createElement("summary"),body=document.createElement("p");summary.textContent="Run details";
    body.textContent=[browser.execution ? `Mode: ${browser.execution.mode} · Driver: ${browser.execution.driver} · Source: ${browser.execution.source} · Revision: ${browser.execution.revision}` : "",browser.detail,browser.decision,browser.message_mode,showSettingsSummary(browser.settings)].filter(Boolean).join(" · ");details.append(summary,body);card.append(details);
    const start = document.createElement("button");start.type = "button";start.className = "primary-button";start.textContent = "Start Show Chat";
    start.disabled = !sharedSettingsReady() || !state.show.known || state.show.dirty || state.show.pending || browser.status !== "ready" || !!browser.blocked_by;
    start.addEventListener("click", () => controlShowChat(browser.id,true));
    const stop = document.createElement("button");stop.type = "button";stop.className = "secondary-button";stop.textContent = "Stop Show Chat";
    stop.disabled = state.show.pending || !["starting","running","stopping"].includes(browser.status);
    stop.addEventListener("click", () => controlShowChat(browser.id,false));
    const actions = document.createElement("div");actions.className = "show-session-actions";actions.append(start,stop);card.append(actions);box.append(card);
  }
  if (!snap?.browsers?.length) {const p = document.createElement("p");p.textContent = "No browser connected. Open a Show Mode browser and log in to begin.";box.append(p);}
}
async function controlShowChat(id,start) {
  const status = document.getElementById("show-control-status");
  if (state.show.pending) return;
  if (start && (!sharedSettingsReady() || state.show.dirty || !state.show.known)) {status.textContent = "Save settings and refresh status before Start.";return;}
  state.show.pending = true;renderShowControl();
  try {
    if (start) await invoke("StartShowChat", id, state.show.revision);
    else await invoke("StopShowChat", id);
    await refreshShowControl();
  } catch (error) {status.textContent = error.message || String(error);}
  finally {state.show.pending = false;renderShowControl();}
}


function diagnosticSelection() {
  const d=state.diagnostics, run=d.selected || d.chatRun;
  return [run, !d.all && run===d.chatRun ? d.conversation : ""];
}
function diagnosticKey() { return JSON.stringify([state.diagnostics.selected,...diagnosticSelection()]); }
function diagnosticButtons() {
  const d=state.diagnostics, available=d.key===diagnosticKey() && d.snapshot?.runs?.some(r=>r.id===d.snapshot.selected);
  for (const id of ["diagnostic-copy","diagnostic-clear"]) document.getElementById(id).disabled=d.busy || !available;
  for (const id of ["diagnostic-run","diagnostic-all"]) document.getElementById(id).disabled=d.busy;
}
async function refreshHomeDiagnostics() {
  if (!document.getElementById("diagnostic-events")) return;
  if (!backend() || typeof backend().GetHomeDiagnostics!=="function") {
    document.getElementById("diagnostic-note").textContent="Diagnostics require the updated backend. Standard Bot is not instrumented.";return;
  }
  const d=state.diagnostics;if(d.busy)return;
  const key=diagnosticKey();if(d.pendingKey===key)return;
  const token=++d.token,args=diagnosticSelection();d.pendingKey=key;diagnosticButtons();
  try {
    const snapshot=await invoke("GetHomeDiagnostics",...args);
    if(token!==d.token || key!==diagnosticKey())return;
    d.snapshot=snapshot;d.key=key;renderHomeDiagnostics(snapshot,args[1]);
  } catch (_) {
    if(token===d.token)document.getElementById("diagnostic-note").textContent="Diagnostics unavailable; no chat control was changed.";
  }
  finally {if(token===d.token)d.pendingKey=null;}
  diagnosticButtons();
}
const diagnosticMetricLabels={selected_ms:"Selected delay",configured_min_ms:"Configured min",configured_max_ms:"Configured max",actual_wait_ms:"Actual intentional wait",timer_elapsed_ms:"Timer armed → event elapsed",queue_ms:"Queue wait",detected_to_send_ms:"Detected → send started",detected_to_confirmed_ms:"Detected → confirmed total",send_ms:"Send / confirmation elapsed",pending:"Pending rows",count:"Rows"};
function renderHomeDiagnostics(snapshot,conversation) {
  const d=state.diagnostics, runs=snapshot.runs || [],run=runs.find(r=>r.id===snapshot.selected);
  const select=document.getElementById("diagnostic-run");select.replaceChildren();
  const automatic=document.createElement("option");automatic.value="";automatic.textContent="Follow selected chat / latest run";select.append(automatic);
  for(const r of [...runs].reverse()) {
    const option=document.createElement("option");option.value=r.id;
    option.textContent=`${r.browser} · ${r.id} · ${r.scope}${r.ended ? " (ended)" : ""}`;select.append(option);
  }
  if(d.selected && !runs.some(r=>r.id===d.selected)) {
    const missing=document.createElement("option");missing.value=d.selected;missing.textContent="Selected run no longer retained";select.append(missing);
  }
  select.value=d.selected;
  document.getElementById("diagnostic-settings").textContent=run ? [run.mode,`${run.scope} · ${run.ended ? "Ended" : "Open"} · ${run.id}`,`Browser: ${run.browser} · Profile: ${run.profile} · Account: ${run.account || "Waiting for login"}`,`Message engine: ${run.message_mode || "Not started"} · Saved revision: ${run.revision}`,`Started: ${run.started}${run.ended ? " · Ended: "+run.ended : ""}`,run.settings ? showSettingsSummary(run.settings).replace("Running settings:",run.ended ? "Stopped run settings:" : "Run settings:") : "Browser/login lifecycle only — select a Chat run for automation settings."].join("\n") : "No retained Show Mode run selected.";
  const notes=[conversation ? "Filtered to selected chat + run-global events." : "All retained conversations in selected run."];
  if(run?.dropped)notes.push(`${run.dropped} older events truncated.`);
  if(run?.cleared)notes.push(`${run.cleared} events cleared from diagnostic view.`);
  if(snapshot.evicted)notes.push(`${snapshot.evicted} older runs evicted.`);
  if(snapshot.missed)notes.push(`${snapshot.missed} events omitted to keep observation nonblocking.`);
  document.getElementById("diagnostic-note").textContent=notes.join(" ");
  const box=document.getElementById("diagnostic-events"),key=JSON.stringify([snapshot.selected,snapshot.events]);
  if(d.rendered===key)return;
  d.rendered=key;
  const oldTop=box.scrollTop,fragment=document.createDocumentFragment(),chats=new Map();
  for(const event of snapshot.events || []) {
    if(event.conversation && !chats.has(event.conversation))chats.set(event.conversation,chats.size+1);
    const row=document.createElement("div");row.className="diagnostic-event";
    const heading=document.createElement("strong");
    heading.textContent=[event.time,event.code,event.conversation ? `Retained chat ${chats.get(event.conversation)}` : "Run",event.reply ? `Reply ${event.reply}` : "",event.part ? `Part ${event.part}` : "",event.wait_kind || "",event.reason || "",event.detected_at ? "Detected at: "+event.detected_at : ""].filter(Boolean).join(" · ");
    const metrics=document.createElement("div");metrics.textContent=Object.entries(diagnosticMetricLabels).filter(([key])=>Number.isFinite(event.metrics?.[key])).map(([key,label])=>`${label}: ${key.endsWith("_ms") ? (event.metrics[key]/1000).toFixed(3)+"s" : event.metrics[key]}`).join(" · ");
    row.append(heading,metrics);fragment.append(row);
  }
  if(!fragment.childNodes.length) {const empty=document.createElement("p");empty.textContent="No retained events for this selection.";fragment.append(empty);}
  box.replaceChildren(fragment);box.scrollTop=document.getElementById("diagnostic-scroll").checked ? box.scrollHeight : oldTop;
}
// Explicit export schema: never serialize arbitrary backend fields, account,
// profile, raw conversation IDs, transcript rows or future sensitive settings.
function diagnosticExport(snapshot) {
  const run=snapshot.runs.find(r=>r.id===snapshot.selected);if(!run)return null;
  const s=run.settings,f=s?.first_message,settings=s ? {} : null;
  for(const key of ["reply_delay_min_seconds","reply_delay_max_seconds","auto_skip","no_reply_min_seconds","no_reply_max_seconds","next_chat_min_seconds","next_chat_max_seconds"])if(s)settings[key]=s[key];
  if(s)settings.first_message={mode:f?.mode,bot_first_percent:f?.bot_first_percent,wait_timeout:f?.wait_timeout,wait_min_seconds:f?.wait_min_seconds,wait_max_seconds:f?.wait_max_seconds};
  const chats=new Map();
  return {schema:1,timing_basis:"runner incoming detection; DOM own-row confirmation is not delivery/read receipt",run:{id:run.id,browser:run.browser,scope:run.scope,mode:run.mode,message_mode:run.message_mode,revision:run.revision,started:run.started,ended:run.ended,settings,dropped:run.dropped,cleared:run.cleared},evicted:snapshot.evicted,missed:snapshot.missed,events:(snapshot.events || []).map(e=>{
    if(e.conversation && !chats.has(e.conversation))chats.set(e.conversation,`chat_${chats.size+1}`);
    const metrics={};for(const key of Object.keys(diagnosticMetricLabels))if(Number.isFinite(e.metrics?.[key]))metrics[key]=e.metrics[key];
    return {sequence:e.sequence,time:e.time,detected_at:e.detected_at,code:e.code,reason:e.reason,conversation:e.conversation ? chats.get(e.conversation) : "",reply:e.reply,part:e.part,wait_kind:e.wait_kind,metrics};
  })};
}
async function copyHomeDiagnostics() {
  const d=state.diagnostics,run=d.snapshot?.selected;if(!run || d.key!==diagnosticKey() || d.busy)return;
  const key=diagnosticKey();d.busy=true;++d.token;d.pendingKey=null;diagnosticButtons();
  try {
    const snapshot=await invoke("GetHomeDiagnostics",run,"");
    if(key!==diagnosticKey())return;
    const payload=diagnosticExport(snapshot);if(!payload)throw new Error("Run no longer retained");
    const text=JSON.stringify(payload,null,2),fallback=document.getElementById("diagnostic-export");
    try {await navigator.clipboard.writeText(text);fallback.hidden=true;document.getElementById("diagnostic-note").textContent="Selected run diagnostics copied (all retained conversations; profile/account omitted).";}
    catch (_) {fallback.value=text;fallback.hidden=false;fallback.focus();fallback.select();document.getElementById("diagnostic-note").textContent="Clipboard unavailable. Copy the selected diagnostic text manually.";}
  } catch (_) {document.getElementById("diagnostic-note").textContent="Could not copy the selected run.";}
  finally {d.busy=false;diagnosticButtons();}
}
async function clearHomeDiagnostics() {
  const d=state.diagnostics,run=d.snapshot?.selected;if(!run || d.key!==diagnosticKey() || d.busy)return;
  d.busy=true;++d.token;d.pendingKey=null;diagnosticButtons();
  hideDiagnosticExport();
  let cleared=false;
  try {await invoke("ClearHomeDiagnostics",run);cleared=true;}
  catch (_) {document.getElementById("diagnostic-note").textContent="Could not clear diagnostics; chat history and logs.txt were not touched.";}
  finally {d.busy=false;if(cleared)await refreshHomeDiagnostics();diagnosticButtons();}
}
function bindHomeDiagnostics() {
  document.getElementById("diagnostic-run").addEventListener("change",event=>{state.diagnostics.selected=event.target.value;hideDiagnosticExport();refreshHomeDiagnostics();});
  document.getElementById("diagnostic-all").addEventListener("change",event=>{state.diagnostics.all=event.target.checked;hideDiagnosticExport();refreshHomeDiagnostics();});
  document.getElementById("diagnostic-copy").addEventListener("click",copyHomeDiagnostics);
  document.getElementById("diagnostic-clear").addEventListener("click",clearHomeDiagnostics);
}

function hideDiagnosticExport() {
 const box=document.getElementById("diagnostic-export");if(box){box.hidden=true;box.value="";}
}


function sharedSettingsReady() {
 return state.common.loaded && !!state.common.snapshot?.unified && !state.show.dirty && !state.show.pending;
}
function renderSharedLock() {
 const brief=document.getElementById("shared-settings-summary"),g=state.common.snapshot?.general;
 if(brief)brief.textContent=state.show.dirty ? "Unsaved settings — Save in Settings before starting." : !state.common.snapshot?.unified ? "Set up shared settings once in Settings before starting." : `Shared settings · Reply ${g.reply_delay_min_seconds}–${g.reply_delay_max_seconds}s · Next chat ${g.match_close_wait_min_seconds}–${g.match_close_wait_max_seconds}s · Silent timeout ${g.match_reply_timeout_min_seconds}–${g.match_reply_timeout_max_seconds}s`;
 const form=document.getElementById("general-form");if(!form)return;
 const locked=state.common.running || !!state.show.snapshot?.active || state.show.pending;
 for(const element of form.querySelectorAll("input,select,button"))element.disabled=locked || !state.common.loaded;
 if(document.getElementById("workspace-mode").value!=="standard")for(const element of form.querySelector(".advanced-settings").querySelectorAll("input,select"))element.disabled=true;
 if(!locked && state.common.loaded)updateStandardPolicy();
 const start=document.getElementById("start-bot"),stop=document.getElementById("stop-bot");
 if(start)start.disabled=!sharedSettingsReady() || state.common.running;
 if(stop)stop.disabled=!state.common.running;
 const status=document.getElementById("standard-save-status");
 if(locked && !state.show.pending){status.textContent="Stop running chats before saving shared settings.";status.dataset.runtimeLocked="true";}
 else if(!locked && status.dataset.runtimeLocked){delete status.dataset.runtimeLocked;status.textContent=state.show.dirty ? "Unsaved changes — Save before Start." : state.common.snapshot?.unified ? "Saved · shared by both modes." : "Choose an existing profile, review, then Save.";}
}
function fillSharedValues(g) {
 const f=g.first_message?.mode ? g.first_message : {mode:"bot",bot_first_percent:70,wait_timeout:false,wait_min_seconds:30,wait_max_seconds:60};
 document.getElementById("standard-policy-mode").value=f.mode;
 document.getElementById("standard-policy-chance").value=f.bot_first_percent;
 document.getElementById("standard-policy-timeout").checked=!!f.wait_timeout;
 document.getElementById("standard-policy-min").value=f.wait_min_seconds;
 document.getElementById("standard-policy-max").value=f.wait_max_seconds;updateStandardPolicy();
 for(const [id,key] of [["worker-reply-delay-min","reply_delay_min_seconds"],["worker-reply-delay-max","reply_delay_max_seconds"],["match-reply-timeout-min","match_reply_timeout_min_seconds"],["match-reply-timeout-max","match_reply_timeout_max_seconds"],["match-close-wait-min","match_close_wait_min_seconds"],["match-close-wait-max","match_close_wait_max_seconds"],["poll-min","poll_min_seconds"],["poll-max","poll_max_seconds"],["action-min","action_min_seconds"],["action-max","action_max_seconds"]])document.getElementById(id).value=g[key] ?? 0;
}
function chooseSharedSource(source) {
 const snapshot=state.common.snapshot;if(!snapshot || snapshot.unified)return;
 state.common.source=source;if(!source)return;
 const g={...snapshot.general};
 if(source==="show") {
  const s=snapshot.legacy_show;
  Object.assign(g,{reply_delay_min_seconds:s.reply_delay_min_seconds,reply_delay_max_seconds:s.reply_delay_max_seconds,match_reply_timeout_min_seconds:s.no_reply_min_seconds>0 ? s.no_reply_min_seconds : g.match_reply_timeout_min_seconds,match_reply_timeout_max_seconds:s.no_reply_min_seconds>0 ? s.no_reply_max_seconds : g.match_reply_timeout_max_seconds,match_close_wait_min_seconds:s.next_chat_min_seconds,match_close_wait_max_seconds:s.next_chat_max_seconds});
 }
 fillSharedValues(g);
 document.getElementById("settings-migration-summary").textContent=`Review ${source==="show" ? "Show Mode" : "Standard Bot"} values below. Save applies them to both modes. Only the three timing ranges are copied. Standard polling/action and its stored first-message policy are preserved.`;
 state.show.dirty=true;renderShowControl();
}
async function refreshSharedSettings() {
 const token=++state.common.token,edits=state.show.edits;
 if(!backend() || typeof backend().GetChatSettings!=="function") {document.getElementById("standard-save-status").textContent="Update the backend to use shared settings.";renderSharedLock();return;}
 try {
  const snapshot=await invoke("GetChatSettings");
  if(token!==state.common.token || edits!==state.show.edits || state.show.dirty)return;
  state.common.snapshot=snapshot;state.common.loaded=true;
  document.getElementById("settings-migration").hidden=!!snapshot.unified;
  if(snapshot.unified)fillSharedValues(snapshot.general);
  else {fillSharedValues(snapshot.general);document.getElementById("settings-source").value="";}
  refreshExecutionPreview();
  document.getElementById("standard-save-status").textContent=snapshot.unified ? "Saved · shared by both modes." : snapshot.source==="legacy_conflict" ? "Existing timing records conflict. Choose a source, review, then Save before starting." : "First setup: choose an existing profile, review, then Save.";
 } catch (_) {document.getElementById("standard-save-status").textContent="Could not load shared settings. Refresh to try again.";}
 renderShowControl();
}
async function saveSharedSettings(event) {
 event?.preventDefault();const status=document.getElementById("standard-save-status");
 if(state.show.pending || state.common.running || state.show.snapshot?.active){status.textContent="Stop all running chats before saving shared settings.";return;}
 if(!state.common.loaded || (!state.common.snapshot?.unified && !state.common.source)){status.textContent="Choose an existing profile before the first Save.";return;}
 const edits=state.show.edits;
 try {
  const mode=document.getElementById("workspace-mode").value;
  const pairs=[["poll-min","poll-max"],["action-min","action-max"],["worker-reply-delay-min","worker-reply-delay-max"],["match-reply-timeout-min","match-reply-timeout-max"],["match-close-wait-min","match-close-wait-max"]];validateTimingPairs(mode==="standard" ? pairs : pairs.slice(2));
  for(const id of mode==="standard" ? ["poll-min","action-min","match-reply-timeout-min"] : ["match-reply-timeout-min"])if(Number(document.getElementById(id).value)<1)throw new Error("Polling, action and no-reply timeout must be at least 1 second.");
  const general={...state.common.snapshot.general,first_message:mode==="standard" ? readStandardPolicy() : state.common.snapshot.general.first_message};
  for(const [ids,keys] of [[pairs[0],["poll_min_seconds","poll_max_seconds"]],[pairs[1],["action_min_seconds","action_max_seconds"]],[pairs[2],["reply_delay_min_seconds","reply_delay_max_seconds"]],[pairs[3],["match_reply_timeout_min_seconds","match_reply_timeout_max_seconds"]],[pairs[4],["match_close_wait_min_seconds","match_close_wait_max_seconds"]]])ids.forEach((id,i)=>{if(mode==="standard" || !["poll-min","poll-max","action-min","action-max"].includes(id))general[keys[i]]=Number(document.getElementById(id).value);});
  const payload={general,edit_mode:mode};
  state.show.pending=true;renderShowControl();
  await invoke("SaveChatSettings",payload);
  if(edits===state.show.edits)state.show.dirty=false;
  await refreshSharedSettings();await refreshShowControl();
  status.textContent=state.show.dirty ? "Saved, but newer edits remain unsaved." : "Saved · both modes will use these settings on their next Start.";
 }catch(error){status.textContent=error.message || String(error);}
 finally{state.show.pending=false;renderShowControl();}
}
function bindWorkspace() {
 const mode=document.getElementById("workspace-mode");let previousMode=mode.value;
 const update=()=>{
  if(previousMode==="standard" && mode.value==="show" && state.show.dirty && standardDraftChanged()){
   mode.value=previousMode;document.getElementById("standard-save-status").textContent="Save Standard-only changes before switching to Show Mode.";return;
  }
  previousMode=mode.value;const show=mode.value==="show";document.getElementById("settings-show").hidden=!show;document.getElementById("standard-actions").hidden=show;document.getElementById("live-chat-workspace").hidden=!show;document.getElementById("standard-mode-note").hidden=show;const advanced=document.querySelector("#general-form .advanced-settings");advanced.hidden=show;if(show)advanced.open=false;renderSharedLock();refreshExecutionPreview();};
 mode.addEventListener("change",update);update();
 for(const button of document.querySelectorAll("[data-open-tab]"))button.addEventListener("click",()=>activateTab(button.dataset.openTab));
 document.getElementById("diagnostic-details").addEventListener("toggle",event=>document.querySelector(".home-live-grid").classList.toggle("details-open",event.target.open));
}


function updateStandardPolicy() {
 const mode=document.getElementById("standard-policy-mode").value,wait=document.getElementById("standard-policy-timeout").checked;
 const locked=state.common.running || !!state.show.snapshot?.active || state.show.pending || !state.common.loaded || document.getElementById("workspace-mode").value!=="standard";
 document.getElementById("standard-policy-chance-row").hidden=mode!=="random";
 document.getElementById("standard-policy-chance").disabled=locked || mode!=="random";
 document.getElementById("standard-policy-timeout").disabled=locked || mode==="bot";
 for(const id of ["standard-policy-min","standard-policy-max"])document.getElementById(id).disabled=locked || mode==="bot" || !wait;
}
function standardPolicyDraft() {
 const value=id=>document.getElementById(id).value;
 return {mode:value("standard-policy-mode"),bot_first_percent:Number(value("standard-policy-chance")),wait_timeout:document.getElementById("standard-policy-timeout").checked,wait_min_seconds:Number(value("standard-policy-min")),wait_max_seconds:Number(value("standard-policy-max"))};
}
function standardDraftChanged(){
 const g=state.common.snapshot?.general;if(!g)return false;
 const f=g.first_message?.mode ? g.first_message : {mode:"bot",bot_first_percent:70,wait_timeout:false,wait_min_seconds:30,wait_max_seconds:60};
 const draft=standardPolicyDraft();
 return Object.keys(draft).some(key=>draft[key]!==f[key]) || [["poll-min","poll_min_seconds"],["poll-max","poll_max_seconds"],["action-min","action_min_seconds"],["action-max","action_max_seconds"]].some(([id,key])=>Number(document.getElementById(id).value)!==g[key]);
}
function readStandardPolicy() {
 const f=standardPolicyDraft();
 if(!["bot","user","random"].includes(f.mode))throw new Error("Choose a Standard first-message policy.");
 if(!Number.isInteger(f.bot_first_percent) || f.bot_first_percent<0 || f.bot_first_percent>100)throw new Error("Bot first chance must be 0 to 100.");
 validateTimingPairs([["standard-policy-min","standard-policy-max"]]);
 if(f.wait_timeout && f.wait_min_seconds<1)throw new Error("First wait needs a positive minimum.");
 return f;
}
async function refreshExecutionPreview() {
 const token=++state.executionRequest,mode=document.getElementById("workspace-mode").value,box=document.getElementById("execution-settings");
 if(!backend() || typeof backend().GetExecutionSettings!=="function"){box.textContent="Detailed execution settings require the updated backend.";return;}
 try {
  const r=await invoke("GetExecutionSettings",mode);
  if(token!==state.executionRequest || mode!==document.getElementById("workspace-mode").value)return;
  const g=r.standard;
  box.textContent=[`Mode: ${r.mode} · Driver: ${r.driver} · Source: ${r.source} · Revision: ${r.revision}`,r.purpose,r.show ? showSettingsSummary(r.show) : g ? `Reply ${g.reply_delay_min_seconds}–${g.reply_delay_max_seconds}s · Idle ${g.match_reply_timeout_min_seconds}–${g.match_reply_timeout_max_seconds}s · Next chat ${g.match_close_wait_min_seconds}–${g.match_close_wait_max_seconds}s · Poll ${g.poll_min_seconds}–${g.poll_max_seconds}s · Action ${g.action_min_seconds}–${g.action_max_seconds}s · First: ${g.first_message?.mode || "bot"}` : "No chat timings apply."].join("\n");
 }catch(_){if(token===state.executionRequest)box.textContent="Could not resolve execution settings. Review settings before Start.";}
}
