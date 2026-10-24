'use strict';

const $ = (id) => document.getElementById(id);
const input = window.jsonEditor;
const state = { mode: 'format', wrap: true, output: '', history: [], version: 0, timer: null, controller: null, historyVersion: 0, activeID: null, error: null, composing: false, repair: null };
const bytes = (n) => n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} KB`;
let toastTimer;

function toast(message) {
  $('toast').textContent = message;
  $('toast').hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { $('toast').hidden = true; }, 2800);
}

async function api(path, options = {}) {
  const response = await fetch(path, { credentials: 'same-origin', ...options });
  if (response.status === 204) return null;
  const data = await response.json();
  if (!response.ok) throw Object.assign(new Error(data.error || '请求失败，请重试'), data);
  return data;
}

function status(text, kind = '') {
  const el = $('output-status');
  el.className = kind ? `status-${kind}` : '';
  el.textContent = text;
}

function invalidate() {
  state.version++;
  clearTimeout(state.timer);
  state.controller?.abort();
  state.output = '';
  $('fold-all').disabled = true;
  $('unfold-all').disabled = true;
  $('input-meta').textContent = `${input.value.length.toLocaleString()} 字符 · UTF-8`;
  $('save-status').classList.remove('save-warning');
  return state.version;
}

function resetOutput() {
  $('error-panel').hidden = true;
  input.removeAttribute('aria-invalid');
  status('');
}

function schedule({ save = true, immediate = false, repair = false } = {}) {
  const version = invalidate();
  if (state.composing) return;
  if (!input.value.trim()) {
    resetOutput();
    $('save-status').textContent = '◈ 有效 JSON 自动保存历史 · 仅当前浏览器可见';
    return;
  }
  $('error-panel').hidden = true;
  input.removeAttribute('aria-invalid');
  $('save-status').textContent = save ? '◈ 等待整理完成后保存…' : '◈ 整理中…';
  status(repair ? '正在修复…' : '正在整理…');
  state.timer = setTimeout(() => format(version, save, repair), immediate ? 0 : 450);
}

async function format(version, save, repair) {
  if (new TextEncoder().encode(input.value).length > 1024 * 1024) {
    showError({ message: '输入不能超过 1 MB。请缩小 JSON 后重试。' });
    return;
  }
  const controller = new AbortController(); state.controller = controller;
  try {
    await sessionReady;
    if (version !== state.version) return;
    const original = input.value;
    const result = await api('/api/format', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ input: original, mode: state.mode, indent: $('indent').value, save, repair }), signal: controller.signal });
    if (version !== state.version) return;
    state.output = result.output;
    replaceWithFormatted(result.output);
    if (result.repair) {
      state.repair = { ...result.repair, original, output: result.output };
    } else if (state.repair && state.repair.output === original) {
      const oldOffsets = significantOffsets(original), newOffsets = significantOffsets(result.output);
      const positions = new Map(oldOffsets.map((offset, index) => [offset, newOffsets[index]]));
      const ranges = [];
      for (const range of state.repair.ranges) {
        for (let offset = range.start; offset < range.end; offset++) {
          const end = positions.get(offset + 1);
          if (end === undefined) continue;
          const previous = ranges[ranges.length - 1];
          if (previous?.end === end - 1) previous.end = end;
          else ranges.push({ start: end - 1, end });
        }
      }
      state.repair.ranges = ranges;
      state.repair.output = result.output;
    }
    renderRepair();
    $('fold-all').disabled = false; $('unfold-all').disabled = false;
    status('有效 JSON', 'ok');
    $('save-status').textContent = result.warning ? `◈ ${result.warning}` : result.saved ? '◈ 已保存到历史记录 · 仅当前浏览器可见' : '◈ ' + (state.activeID ? '已恢复历史记录' : '整理完成');
    $('save-status').classList.toggle('save-warning', !!result.warning);
    if (result.saved) await loadHistory();
  } catch (error) {
    if (error.name === 'AbortError' || version !== state.version) return;
    showError(error);
  }
}

function showError(error) {
  state.error = error;
  $('error-panel').hidden = false;
  input.setAttribute('aria-invalid', 'true');
  $('error-location').textContent = error.line ? `第 ${error.line} 行，第 ${error.column} 列附近` : '暂时无法整理';
  $('error-message').textContent = error.message || error.error;
  $('save-status').textContent = error.line ? '当前内容未保存 · 可点击“修复 JSON”尝试修复' : '当前内容未保存';
  $('error-panel').title = `${$('error-location').textContent}：${$('error-message').textContent}`;
  $('locate-error').hidden = !error.line;
  status('');
}

function clearRepair() {
  state.repair = null;
  renderRepair();
}

function renderRepair() {
  const info = state.repair;
  input.setRepairRanges(info?.ranges || []);
  $('repair-notice').hidden = !info;
  if (!info) return;
  $('repair-summary').textContent = `已修复：补入 ${info.added} 个字符${info.added ? '（绿色下划线）' : ''}${info.removed ? `，删除 ${info.removed} 个多余字符` : ''}。请核对结果。`;
}

// Ignore formatting whitespace while preserving spaces and escapes inside strings.
function significantOffsets(text) {
  const offsets = [0];
  let quoted = false, escaped = false;
  for (let i = 0; i < text.length; i++) {
    const char = text[i];
    if (quoted || !/\s/.test(char)) offsets.push(i + 1);
    if (escaped) { escaped = false; continue; }
    if (quoted && char === '\\') { escaped = true; continue; }
    if (char === '"') quoted = !quoted;
  }
  return offsets;
}

function replaceWithFormatted(value) {
  if (input.value !== value) {
    const before = significantOffsets(input.value), after = significantOffsets(value);
    const map = (position) => {
      let low = 0, high = before.length;
      while (low < high) {
        const mid = (low + high) >>> 1;
        if (before[mid] <= position) low = mid + 1; else high = mid;
      }
      return after[Math.min(low - 1, after.length - 1)];
    };
    const start = map(input.selectionStart), end = map(input.selectionEnd);
    const direction = input.selectionDirection, top = input.scrollTop, left = input.scrollLeft;
    input.value = value;
    input.setSelectionRange(start, end, direction);
    input.scrollTop = top; input.scrollLeft = left;
  }
  $('input-meta').textContent = `${input.value.length.toLocaleString()} 字符 · UTF-8`;
  $('error-panel').hidden = true;
  input.removeAttribute('aria-invalid');
}

async function loadHistory() {
  const version = ++state.historyVersion;
  try {
    const result = await api('/api/history');
    if (version !== state.historyVersion) return;
    state.history = result.items;
    renderHistory();
  } catch (error) {
    if (version !== state.historyVersion) return;
    $('history-list').replaceChildren();
    const msg = document.createElement('div'); msg.className = 'history-empty'; msg.textContent = error.message;
    $('history-list').append(msg);
  }
}

function renderHistory() {
  $('history-count').textContent = state.history.length;
  const query = $('history-search').value.trim().toLowerCase();
  const items = state.history.filter((h) => h.title.toLowerCase().includes(query));
  const container = $('history-list'); container.replaceChildren();
  if (!items.length) {
    const msg = document.createElement('div'); msg.className = 'history-empty';
    msg.textContent = query ? '没有找到匹配的记录' : '还没有历史记录。\n粘贴一段 JSON，开始第一次整理。';
    container.append(msg); return;
  }
  for (const item of items) {
    const row = document.createElement('div'); row.className = `history-row${state.activeID === item.id ? ' active' : ''}`;
    const open = document.createElement('button'); open.className = 'history-open'; open.title = item.title;
    const icon = document.createElement('span'); icon.className = 'history-braces'; icon.textContent = '{ }'; icon.setAttribute('aria-hidden', 'true');
    const text = document.createElement('span'); text.className = 'history-text';
    const title = document.createElement('span'); title.className = 'history-title'; title.textContent = item.title;
    const meta = document.createElement('span'); meta.className = 'history-meta';
    meta.textContent = `${new Date(item.updatedAt).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })} · ${bytes(item.bytes)} · ${item.mode === 'compact' ? '压缩' : '格式化'}`;
    text.append(title, meta); open.append(icon, text); open.addEventListener('click', () => restore(item.id));
    const remove = document.createElement('button'); remove.className = 'icon-button history-delete'; remove.textContent = '×'; remove.title = '删除此记录'; remove.setAttribute('aria-label', `删除记录：${item.title}`);
    remove.addEventListener('click', async () => {
      if (!confirm('删除这条历史记录？此操作不会清空当前编辑区。')) return;
      remove.disabled = true;
      try { await api(`/api/history/${item.id}`, { method: 'DELETE' }); if (state.activeID === item.id) state.activeID = null; await loadHistory(); toast('历史记录已删除'); } catch (error) { toast(error.message); remove.disabled = false; }
    });
    row.append(open, remove); container.append(row);
  }
}

async function restore(id) {
  const version = invalidate();
  try {
    const item = await api(`/api/history/${id}`);
    if (version !== state.version) return;
    clearRepair(); input.value = item.input; state.activeID = item.id; state.mode = item.mode; $('indent').value = item.indent;
    updateMode(); renderHistory(); schedule({ save: false, immediate: true });
    toast('已恢复历史记录');
  } catch (error) { if (version === state.version) toast(error.message); }
}

function updateMode() {
  for (const mode of ['format', 'compact']) { const button = $(`mode-${mode}`); button.classList.toggle('selected', mode === state.mode); button.setAttribute('aria-pressed', String(mode === state.mode)); }
  $('indent').disabled = state.mode === 'compact';
}

function clearInput() { clearRepair(); input.value = ''; state.activeID = null; schedule(); renderHistory(); input.focus(); }
input.addEventListener('compositionstart', () => { clearRepair(); state.composing = true; invalidate(); });
input.addEventListener('compositionend', () => { state.composing = false; schedule(); });
input.addEventListener('input', (event) => {
  clearRepair();
  state.activeID = null;
  schedule({ immediate: event.inputType === 'insertFromPaste' });
});
$('toggle-history').addEventListener('click', () => {
  const open = $('history-sidebar').hidden;
  $('history-sidebar').hidden = !open;
  $('app-shell').classList.toggle('history-visible', open);
  $('toggle-history').setAttribute('aria-expanded', String(open));
  $('toggle-history').firstElementChild.textContent = open ? '‹' : '›';
  $('toggle-history').setAttribute('aria-label', open ? '收起历史记录' : '展开历史记录');
  $('toggle-history').title = open ? '收起历史记录' : '展开历史记录';
});
$('new-document').addEventListener('click', clearInput);
$('indent').addEventListener('change', () => schedule({ immediate: true }));
for (const mode of ['format', 'compact']) $(`mode-${mode}`).addEventListener('click', () => { state.mode = mode; updateMode(); schedule({ immediate: true }); });
$('wrap').addEventListener('click', () => {
  state.wrap = !state.wrap;
  $('wrap').classList.toggle('active', state.wrap); $('wrap').setAttribute('aria-checked', String(state.wrap));
  input.wrap = state.wrap ? 'soft' : 'off';
});
$('fold-all').addEventListener('click', () => input.foldAll());
$('unfold-all').addEventListener('click', () => input.unfoldAll());
$('repair-json').addEventListener('click', () => {
  schedule({ immediate: true, repair: true });
});
$('undo-repair').addEventListener('click', () => {
  if (!state.repair) return;
  const original = state.repair.original;
  clearRepair(); input.value = original;
  schedule({ save: false, immediate: true, repair: false });
  input.focus();
});
$('history-search').addEventListener('input', renderHistory);
$('refresh-history').addEventListener('click', loadHistory);
$('locate-error').addEventListener('click', () => {
  input.focus();
  if (!state.error?.line) return;
  const lines = input.value.split('\n');
  const offset = lines.slice(0, state.error.line - 1).reduce((n, line) => n + line.length + 1, 0) + [...(lines[state.error.line - 1] || '')].slice(0, state.error.column - 1).join('').length;
  input.setSelectionRange(offset, offset + 1);
});
document.addEventListener('json-format-request', () => schedule({ immediate: true }));
document.addEventListener('keydown', (event) => {
  if (event.defaultPrevented) return;
  if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') { event.preventDefault(); schedule({ immediate: true }); }
  if (event.altKey && event.key.toLowerCase() === 'n') { event.preventDefault(); clearInput(); }
});
async function health() {
  try { await api('/api/health'); $('connection').classList.remove('offline'); $('connection').replaceChildren(); const dot = document.createElement('i'); $('connection').append(dot, 'MySQL 已连接'); }
  catch { $('connection').classList.add('offline'); $('connection').replaceChildren(); const dot = document.createElement('i'); $('connection').append(dot, '服务连接异常'); }
}
// Establish the HttpOnly session before the first auto-save to avoid first-load cookie races.
const sessionReady = loadHistory();
health();
setInterval(health, 30000);
