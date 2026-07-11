package main

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tenzing agent</title>
<style>
  :root {
    --bg: #1a1a2e;
    --bg2: #16213e;
    --fg: #e0e0e0;
    --fg-dim: #888;
    --accent: #0f3460;
    --blue: #53a8ff;
    --yellow: #e2b93d;
    --red: #e74c3c;
    --green: #2ecc71;
    --mono: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: var(--mono);
    background: var(--bg);
    color: var(--fg);
    height: 100vh;
    display: flex;
    flex-direction: column;
  }
  #chat {
    flex: 1;
    overflow-y: auto;
    padding: 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .msg { max-width: 100%; white-space: pre-wrap; word-break: break-word; line-height: 1.5; }
  .msg.user { color: var(--blue); }
  .msg.user::before { content: '❯ '; }
  .msg.assistant { color: var(--fg); }
  .msg.thinking { color: var(--fg-dim); font-style: italic; }
  .msg.thinking::before { content: '💭 '; }
  .msg.tool { color: var(--yellow); font-size: 0.85em; opacity: 0.8; }
  .msg.tool.sub { padding-left: 1rem; opacity: 0.65; }
  .msg.subagent { color: var(--green); font-size: 0.85em; }
  .msg.tool-progress { color: var(--fg-dim); font-size: 0.8em; padding-left: 1rem; }
  .msg.error { color: var(--red); }
  .msg.error::before { content: '✗ '; }
  .msg.system { color: var(--fg-dim); font-size: 0.85em; }
  .msg.streaming { color: var(--fg); }
  .msg.streaming::after { content: '▊'; animation: blink 1s step-end infinite; }
  @keyframes blink { 50% { opacity: 0; } }

  #input-area {
    border-top: 1px solid var(--accent);
    padding: 0.75rem 1rem;
    display: flex;
    gap: 0.5rem;
    background: var(--bg2);
  }
  #status { font-size: 0.8em; color: var(--fg-dim); padding: 0.25rem 1rem; background: var(--bg2); }
  #query {
    flex: 1;
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 4px;
    color: var(--fg);
    font-family: var(--mono);
    font-size: 0.9rem;
    padding: 0.5rem;
    resize: none;
    outline: none;
  }
  #query:focus { border-color: var(--blue); }
  button {
    background: var(--accent);
    color: var(--fg);
    border: none;
    border-radius: 4px;
    padding: 0.5rem 1rem;
    font-family: var(--mono);
    cursor: pointer;
    font-size: 0.85rem;
  }
  button:hover { background: var(--blue); }
  button:disabled { opacity: 0.4; cursor: default; }
  #cancel-btn { background: var(--red); display: none; }
  #cancel-btn:hover { opacity: 0.8; }
  pre { background: #0d1117; padding: 0.5rem; border-radius: 4px; overflow-x: auto; margin: 0.25rem 0; }
  code { font-family: var(--mono); }
</style>
</head>
<body>

<div id="chat"></div>
<div id="status"></div>
<div id="input-area">
  <textarea id="query" rows="2" placeholder="ask something..." autofocus></textarea>
  <button id="send-btn" onclick="send()">send</button>
  <button id="cancel-btn" onclick="cancel()">cancel</button>
</div>

<script>
const chat = document.getElementById('chat');
const queryEl = document.getElementById('query');
const sendBtn = document.getElementById('send-btn');
const cancelBtn = document.getElementById('cancel-btn');
const statusEl = document.getElementById('status');

let running = false;
let streamEl = null;
let thinkEl = null;
let inputTokens = 0;
let outputTokens = 0;

function addMsg(cls, text) {
  const el = document.createElement('div');
  el.className = 'msg ' + cls;
  el.textContent = text;
  chat.appendChild(el);
  chat.scrollTop = chat.scrollHeight;
  return el;
}

function updateStatus(text) {
  statusEl.textContent = text;
}

function setRunning(v) {
  running = v;
  sendBtn.disabled = v;
  cancelBtn.style.display = v ? 'inline-block' : 'none';
  queryEl.disabled = v;
  if (!v) queryEl.focus();
}

function finalizeStream() {
  if (streamEl) { streamEl.classList.remove('streaming'); streamEl = null; }
}
function finalizeThinking() {
  if (thinkEl) { thinkEl = null; }
}

// SSE
const es = new EventSource('/events');

es.addEventListener('text_delta', e => {
  finalizeThinking();
  if (!streamEl) {
    streamEl = addMsg('streaming', '');
  }
  streamEl.textContent += e.data;
  chat.scrollTop = chat.scrollHeight;
});

es.addEventListener('thinking_delta', e => {
  if (!thinkEl) {
    thinkEl = addMsg('thinking', '');
  }
  thinkEl.textContent += e.data;
  chat.scrollTop = chat.scrollHeight;
});

function agentTag(d) {
  return d.agent ? '[' + d.agent + '] ' : '';
}

es.addEventListener('tool_start', e => {
  finalizeStream();
  const d = JSON.parse(e.data);
  const inp = d.input.length > 500 ? d.input.slice(0, 500) + '…' : d.input;
  addMsg('tool' + (d.agent ? ' sub' : ''), '⚙ ' + agentTag(d) + d.name + ' ' + inp);
});

es.addEventListener('tool_result', e => {
  const d = JSON.parse(e.data);
  const lines = (d.output || '').split('\n').slice(0, 10);
  const prefix = d.error === 'true' ? '✗ ' : '✓ ';
  const inp = d.input.length > 500 ? d.input.slice(0, 500) + '…' : d.input;
  addMsg('tool' + (d.agent ? ' sub' : ''), prefix + agentTag(d) + d.name + ' ' + inp + '\n' + lines.join('\n'));
});

es.addEventListener('subagent', e => {
  finalizeStream();
  const d = JSON.parse(e.data);
  if (d.state === 'started') {
    const p = d.prompt.length > 200 ? d.prompt.slice(0, 200) + '…' : d.prompt;
    addMsg('subagent', '⧉ ' + d.agent + ' spawned: ' + p);
  } else {
    addMsg('subagent', '⧉ ' + d.agent + ' done (' + d.duration + ')');
  }
});

es.addEventListener('tool_progress', e => {
  const d = JSON.parse(e.data);
  if (d.detail.trim()) {
    addMsg('tool-progress', '▸ ' + d.phase + '\n' + d.detail);
  }
  updateStatus('⚙ ' + d.tool + ' → ' + d.phase);
});

es.addEventListener('llm_meta', e => {
  const d = JSON.parse(e.data);
  inputTokens += d.input_tokens;
  outputTokens += d.output_tokens;
});

es.addEventListener('answer', e => {
  const d = JSON.parse(e.data);
  finalizeThinking();
  if (streamEl) {
    streamEl.classList.remove('streaming');
    streamEl = null;
  } else if (d.text) {
    addMsg('assistant', d.text);
  }
});

es.addEventListener('error', e => {
  try {
    const d = JSON.parse(e.data);
    addMsg('error', d.error);
  } catch(_) {
    addMsg('error', e.data);
  }
});

es.addEventListener('status', e => {
  const d = JSON.parse(e.data);
  if (d.state === 'running') {
    updateStatus('thinking…');
  } else {
    const tk = fmtTokens(inputTokens) + '↑ ' + fmtTokens(outputTokens) + '↓';
    updateStatus(tk);
    setRunning(false);
  }
});

function fmtTokens(n) {
  if (n >= 1e6) return (n/1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n/1e3).toFixed(1) + 'k';
  return n.toString();
}

async function send() {
  const q = queryEl.value.trim();
  if (!q || running) return;

  addMsg('user', q);
  queryEl.value = '';
  setRunning(true);
  streamEl = null;
  thinkEl = null;

  try {
    const res = await fetch('/query', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({query: q}),
    });
    if (!res.ok) {
      const txt = await res.text();
      addMsg('error', txt);
      setRunning(false);
    }
  } catch(e) {
    addMsg('error', e.message);
    setRunning(false);
  }
}

async function cancel() {
  try { await fetch('/cancel', {method: 'POST'}); } catch(_) {}
}

queryEl.addEventListener('keydown', e => {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    send();
  }
});
</script>

</body>
</html>`
