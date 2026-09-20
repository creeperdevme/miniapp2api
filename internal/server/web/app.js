'use strict';

const state = {
  session: null,
  accounts: [],
  editing: null,
  timer: null,
  catalog: [],
  catalogLoaded: false,
};

const $ = (id) => document.getElementById(id);

function esc(value) {
  return String(value == null ? '' : value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

async function api(path, options = {}) {
  const init = { method: options.method || 'GET', credentials: 'same-origin' };
  if (options.body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(options.body);
  }
  const res = await fetch(path, init);
  const text = await res.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch (err) { data = { error: text }; }
  }
  if (!res.ok) {
    const detail = data && data.error;
    const message = typeof detail === 'string' ? detail : (detail && detail.message) || `HTTP ${res.status}`;
    const error = new Error(message);
    error.status = res.status;
    throw error;
  }
  return data;
}

function toast(message, kind = '') {
  const node = document.createElement('div');
  node.className = kind;
  node.textContent = message;
  $('toast').appendChild(node);
  setTimeout(() => node.remove(), 4200);
}

function show(view) {
  for (const name of ['setup', 'login', 'dashboard']) {
    $('view-' + name).classList.toggle('hidden', name !== view);
  }
}

function fmtTime(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  const pad = (n) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function daysLeft(value) {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return Math.floor((date.getTime() - Date.now()) / 86400000);
}

// ---------------------------------------------------------------- 初始化

async function init() {
  try {
    const session = await api('/api/session');
    state.session = session;
    if (session.needs_setup) { show('setup'); return; }
    if (!session.logged_in) { show('login'); return; }
    await enterDashboard();
  } catch (err) {
    toast('無法連線到伺服器：' + err.message, 'err');
    show('login');
  }
}

async function enterDashboard() {
  show('dashboard');
  renderSession();
  await refreshAccounts();
  startPolling();
}

function startPolling() {
  stopPolling();
  state.timer = setInterval(async () => {
    try {
      await refreshAccounts();
    } catch (err) {
      if (err.status === 401) {
        stopPolling();
        show('login');
      }
    }
  }, 4000);
}

function stopPolling() {
  if (state.timer) clearInterval(state.timer);
  state.timer = null;
}

function renderSession() {
  const session = state.session || {};
  $('api-base').textContent = session.base_url || '-';
  $('api-key').textContent = session.api_key_masked || '-';
  if (session.api_key) showFreshKey(session.api_key);

  renderModels();
}

// showFreshKey 顯示剛產生的 API 金鑰，而且只顯示這一次。
function showFreshKey(key) {
  $('modal-settings').classList.add('hidden');
  $('fresh-key').textContent = key;
  $('modal-key').classList.remove('hidden');
  delete state.session.api_key;
}

function renderChips(summary) {
  const data = summary || {};
  const chips = [
    ['帳號總數', data.total || 0],
    ['啟用中', data.enabled || 0],
    ['停用', data.disabled || 0],
    ['冷卻中', data.cooldown || 0],
    ['額度不足', data.quota || 0],
    ['JWT 過期', data.expired || 0],
    ['請求數', data.requests || 0],
    ['成功', data.success || 0],
    ['失敗', data.failed || 0],
  ];
  $('stat-chips').innerHTML = chips.map(([label, value]) => `<span class="chip">${esc(label)} <b>${esc(value)}</b></span>`).join('');
}

// ---------------------------------------------------------------- 號池列表

async function refreshAccounts() {
  const data = await api('/api/accounts');
  state.accounts = data.accounts || [];
  renderAccounts();
  renderChips(data.summary);
}

function accountBadges(account) {
  const badges = [];
  if (account.enabled) badges.push('<span class="badge ok">啟用</span>');
  else badges.push('<span class="badge">停用</span>');

  if (account.cooling_down) {
    badges.push('<span class="badge warn">冷卻中</span>');
  }
  if (account.quota_exceeded) {
    const models = account.quota_models || [];
    const title = models.length ? ` title="額度不足的模型：${esc(models.join('、'))}"` : '';
    badges.push(`<span class="badge err"${title}>額度不足</span>`);
  }
  const left = daysLeft(account.expires_at);
  if (left !== null) {
    if (left < 0) badges.push('<span class="badge err">JWT 已過期</span>');
    else if (left <= 3) badges.push(`<span class="badge warn">JWT 剩 ${left} 天</span>`);
  }
  if (account.password) {
    badges.push('<span class="badge ok" title="已設定密碼，JWT 快到期時會自動重新登入">自動續期</span>');
  }
  return badges.join(' ');
}

function renderAccounts() {
  const container = $('accounts');
  if (!state.accounts.length) {
    container.className = '';
    container.innerHTML = '<div class="empty">號池是空的，點選右上角「＋ 新增帳號」加入一組 JWT 即可。</div>';
    return;
  }
  container.className = 'grid';
  container.innerHTML = state.accounts.map((account) => {
    const stats = account.stats || {};
    const expiry = account.expires_at
      ? `${fmtTime(account.expires_at)}${daysLeft(account.expires_at) !== null ? `（剩 ${daysLeft(account.expires_at)} 天）` : ''}`
      : '-';
    const errLine = account.last_error ? `<div class="err-line">${esc(account.last_error)}</div>` : '';

    return `
      <div class="acct ${account.enabled ? '' : 'off'}">
        <div class="acct-top">
          <div class="acct-name">${esc(account.name || account.email || '未命名帳號')}</div>
          <div class="spacer"></div>
          ${accountBadges(account)}
        </div>
        <div class="acct-id">${esc(account.email || account.id)} · auths/${esc(account.file_name || '')}</div>
        <div class="kv"><span>JWT</span><span>${esc(account.jwt || '-')}</span></div>
        <div class="kv"><span>JWT 到期</span><span>${esc(expiry)}</span></div>
        <div class="kv"><span>使用次數</span><span>${esc(stats.requests || 0)} 次（成功 ${esc(stats.success || 0)}／失敗 ${esc(stats.failed || 0)}）</span></div>
        <div class="kv"><span>最後使用</span><span>${esc(fmtTime(account.last_used_at))}</span></div>
        ${errLine}
        <div class="acct-actions">
          <button class="btn btn-sm" data-action="check" data-id="${esc(account.id)}">測試</button>
          ${account.password ? `<button class="btn btn-sm" data-action="renew" data-id="${esc(account.id)}">續期</button>` : ''}
          <button class="btn btn-sm" data-action="edit" data-id="${esc(account.id)}">編輯</button>
          <button class="btn btn-sm" data-action="toggle" data-id="${esc(account.id)}">${account.enabled ? '停用' : '啟用'}</button>
          <button class="btn btn-sm btn-danger" data-action="delete" data-id="${esc(account.id)}">刪除</button>
        </div>
      </div>`;
  }).join('');
}

$('accounts').addEventListener('click', async (event) => {
  const button = event.target.closest('button[data-action]');
  if (!button) return;
  const id = button.dataset.id;
  const account = state.accounts.find((item) => item.id === id);
  if (!account) return;

  if (button.dataset.action === 'check') {
    button.disabled = true;
    button.textContent = '測試中…';
    try {
      const result = await api(`/api/accounts/${id}/check`, { method: 'POST' });
      if (result.ok) toast(`帳號可用，讀到 ${result.conversations} 筆對話`, 'ok');
      else toast('測試失敗：' + result.error, 'err');
    } catch (err) {
      toast('測試失敗：' + err.message, 'err');
    } finally {
      button.disabled = false;
      button.textContent = '測試';
      refreshAccounts().catch(() => {});
    }
    return;
  }

  if (button.dataset.action === 'edit') { openAccountModal(account); return; }

  if (button.dataset.action === 'renew') {
    button.disabled = true;
    button.textContent = '續期中…';
    try {
      const updated = await api(`/api/accounts/${id}/renew`, { method: 'POST' });
      toast(`已重新登入，JWT 到期 ${fmtTime(updated.expires_at)}`, 'ok');
    } catch (err) {
      toast('續期失敗：' + err.message, 'err');
    } finally {
      button.disabled = false;
      button.textContent = '續期';
      refreshAccounts().catch(() => {});
    }
    return;
  }

  if (button.dataset.action === 'toggle') {
    try {
      await api(`/api/accounts/${id}`, { method: 'PUT', body: { enabled: !account.enabled } });
      await refreshAccounts();
    } catch (err) { toast(err.message, 'err'); }
    return;
  }

  if (button.dataset.action === 'delete') {
    if (!confirm(`確定要刪除「${account.name || account.id}」嗎？此動作會移除 auths/${account.id}.json`)) return;
    try {
      await api(`/api/accounts/${id}`, { method: 'DELETE' });
      toast('已刪除帳號', 'ok');
      await refreshAccounts();
    } catch (err) { toast(err.message, 'err'); }
  }
});

// ---------------------------------------------------------------- 帳號表單

const accountModes = {
  jwt: {
    passwordLabel: '密碼（選填）',
    passwordHint: '填寫後 JWT 快到期時會自動重新登入續期',
  },
  login: {
    passwordLabel: '密碼',
    passwordHint: 'miniapps.ai 的登入密碼',
  },
};

function setAccountMode(mode) {
  if (!accountModes[mode]) mode = 'jwt';
  state.accountMode = mode;

  const editing = !!state.editing;
  const login = mode === 'login';
  $('acct-mode').querySelectorAll('button[data-mode]').forEach((button) => {
    button.classList.toggle('is-active', button.dataset.mode === mode);
  });

  // 隱藏的欄位一定要關掉 required，否則瀏覽器會擋住送出。
  $('acct-jwt-group').classList.toggle('hidden', login);
  $('acct-email-group').classList.toggle('hidden', !login);
  $('acct-jwt').required = !editing && !login;
  $('acct-email').required = !editing && login;
  $('acct-password').required = !editing && login;

  $('acct-password-label').textContent = editing ? '密碼（選填）' : accountModes[mode].passwordLabel;
  $('acct-password').placeholder = editing ? '留空表示不變更' : accountModes[mode].passwordHint;
}

function openAccountModal(account) {
  state.editing = account || null;
  const editing = !!account;
  $('account-title').textContent = editing ? '編輯帳號' : '新增帳號';
  $('acct-name').value = editing ? account.name || '' : '';
  $('acct-jwt').value = '';
  $('acct-email').value = '';
  $('acct-password').value = '';
  $('acct-mode').classList.toggle('hidden', editing);
  setAccountMode('jwt');
  $('modal-account').classList.remove('hidden');
  $('acct-name').focus();
}

$('acct-mode').addEventListener('click', (event) => {
  const button = event.target.closest('button[data-mode]');
  if (button) setAccountMode(button.dataset.mode);
});

$('account-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const editing = !!state.editing;
  const login = !editing && state.accountMode === 'login';
  const payload = {
    name: $('acct-name').value.trim(),
    password: $('acct-password').value,
  };
  if (login) payload.email = $('acct-email').value.trim();
  else payload.jwt = $('acct-jwt').value.trim();

  const button = $('account-submit');
  button.disabled = true;
  try {
    if (state.editing) {
      await api(`/api/accounts/${state.editing.id}`, { method: 'PUT', body: payload });
      toast('已更新帳號', 'ok');
    } else {
      await api('/api/accounts', { method: 'POST', body: payload });
      toast(login ? '已登入並新增帳號' : '已新增帳號', 'ok');
    }
    $('modal-account').classList.add('hidden');
    await refreshAccounts();
  } catch (err) {
    toast(err.message, 'err');
  } finally {
    button.disabled = false;
  }
});

// ---------------------------------------------------------------- 設定

$('btn-regen-key').addEventListener('click', async () => {
  if (!confirm('重新產生後，舊的 API 金鑰會立即失效，確定嗎？')) return;
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { regenerate_api_key: true } });
    renderSession();
  } catch (err) { toast(err.message, 'err'); }
});

$('btn-change-password').addEventListener('click', async () => {
  const current = $('cur-password').value;
  const next = $('new-password').value;
  if (!next) { toast('請輸入新密碼', 'err'); return; }
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { current_password: current, new_password: next } });
    $('cur-password').value = '';
    $('new-password').value = '';
    toast('密碼已更新', 'ok');
  } catch (err) { toast(err.message, 'err'); }
});

// ---------------------------------------------------------------- 模型

// modelAliasBase 把上游名稱整理成能安全放進 URL 的模型名稱。
function modelAliasBase(value) {
  return String(value || '')
    .trim()
    .replace(/[^A-Za-z0-9._-]+/g, '-')
    .replace(/^[.-]+|[.-]+$/g, '')
    .toLowerCase();
}

function currentModels() {
  return (state.session && state.session.models) || [];
}

// usedModelIds 回傳已經被用掉的對外模型名稱。
function usedModelIds() {
  const used = new Set();
  for (const model of currentModels()) used.add(model.id.toLowerCase());
  return used;
}

// addedUpstreamIds 回傳已經加入設定的上游 modelId。
function addedUpstreamIds() {
  const added = new Set();
  for (const model of currentModels()) added.add(model.model_id);
  return added;
}

// suggestModelId 以 nativeId 當作對外名稱，撞名時再加上變體標籤或序號。
function suggestModelId(item, used) {
  const base = modelAliasBase(item.native_id) || modelAliasBase(item.title) || 'model';
  let candidate = base;
  if (used.has(candidate)) {
    const label = modelAliasBase(item.variant_label);
    if (label) candidate = `${base}-${label}`;
  }
  const stem = candidate;
  let index = 2;
  while (used.has(candidate)) candidate = `${stem}-${index++}`;
  return candidate;
}

function renderModels() {
  const models = currentModels();
  const container = $('model-list');
  if (!models.length) {
    container.innerHTML = '<div class="empty" style="padding:26px">尚未設定模型</div>';
    return;
  }

  container.innerHTML = models.map((model) => `
    <div class="model-row">
      <code>${esc(model.id)}</code>
      <span class="model-name">${esc(model.name || '')}</span>
      <div class="spacer"></div>
      <button class="btn btn-sm btn-danger" data-action="remove-model" data-id="${esc(model.id)}">移除</button>
    </div>`).join('');
}

function openCatalogModal() {
  $('modal-catalog').classList.remove('hidden');
  if (!state.catalogLoaded) loadCatalog(false);
}

async function loadCatalog(refresh) {
  state.catalog = [];
  $('cat-meta').textContent = '載入中…';
  $('cat-list').innerHTML = '';
  try {
    const data = await api('/api/models/catalog' + (refresh ? '?refresh=1' : ''));
    state.catalog = data.models || [];
    state.catalogLoaded = true;
    renderCatalog();
  } catch (err) {
    $('cat-meta').textContent = '';
    $('cat-list').innerHTML = `<div class="empty">${esc(err.message)}</div>`;
  }
}

function renderCatalog() {
  const catalog = state.catalog || [];
  const keyword = $('cat-search').value.trim().toLowerCase();
  const showAll = $('cat-all').checked;
  const used = usedModelIds();
  const added = addedUpstreamIds();

  const items = catalog.filter((item) => {
    if (!showAll && !item.available) return false;
    if (!keyword) return true;
    return `${item.title} ${item.native_id} ${item.platform}`.toLowerCase().includes(keyword);
  });

  $('cat-meta').textContent = `顯示 ${items.length} / ${catalog.length} 個模型`;
  if (!items.length) {
    $('cat-list').innerHTML = '<div class="empty">沒有符合的模型</div>';
    return;
  }

  $('cat-list').innerHTML = items.map((item) => {
    const already = added.has(item.id);
    const tags = [];
    if (item.has_tools) tags.push('工具');
    if (item.has_vision) tags.push('視覺');
    if (item.is_reasoning) tags.push('推理');
    if (!item.available) tags.push('已下架');

    const parts = [];
    if (!already) parts.push(suggestModelId(item, used));
    parts.push(item.native_id, item.platform, `${item.credit_price} 點`);
    if (tags.length) parts.push(tags.join('、'));

    return `
      <div class="cat-item">
        <div>
          <div class="cat-title">${esc(item.title)}</div>
          <div class="cat-sub">${esc(parts.join(' · '))}</div>
        </div>
        <div class="spacer"></div>
        <button class="btn btn-sm ${already ? '' : 'btn-primary'}" data-add="${esc(item.id)}"${already ? ' disabled' : ''}>${already ? '已加入' : '加入'}</button>
      </div>`;
  }).join('');
}

$('btn-model-catalog').addEventListener('click', openCatalogModal);
$('btn-cat-refresh').addEventListener('click', () => loadCatalog(true));
$('cat-search').addEventListener('input', renderCatalog);
$('cat-all').addEventListener('change', renderCatalog);

$('cat-list').addEventListener('click', async (event) => {
  const button = event.target.closest('button[data-add]');
  if (!button) return;
  const item = (state.catalog || []).find((model) => model.id === button.dataset.add);
  if (!item) return;

  const alias = suggestModelId(item, usedModelIds());
  button.disabled = true;
  try {
    state.session = await api('/api/models', {
      method: 'POST',
      body: { id: alias, name: item.title, model_id: item.id },
    });
    renderSession();
    renderCatalog();
    toast(`已加入模型 ${alias}`, 'ok');
  } catch (err) {
    button.disabled = false;
    toast(err.message, 'err');
  }
});

$('model-list').addEventListener('click', async (event) => {
  const button = event.target.closest('button[data-action="remove-model"]');
  if (!button) return;
  const id = button.dataset.id;
  if (!confirm(`確定要移除模型「${id}」嗎？`)) return;
  try {
    state.session = await api(`/api/models/${encodeURIComponent(id)}`, { method: 'DELETE' });
    renderSession();
    renderCatalog();
    toast(`已移除模型 ${id}`, 'ok');
  } catch (err) {
    toast(err.message, 'err');
  }
});

// ---------------------------------------------------------------- 其他互動

$('btn-add').addEventListener('click', () => openAccountModal(null));
$('btn-refresh').addEventListener('click', () => refreshAccounts().then(() => toast('已重新整理')).catch((err) => toast(err.message, 'err')));
$('btn-settings').addEventListener('click', () => $('modal-settings').classList.remove('hidden'));

document.querySelectorAll('[data-close]').forEach((button) => {
  button.addEventListener('click', () => $(button.dataset.close).classList.add('hidden'));
});

document.querySelectorAll('.overlay').forEach((overlay) => {
  overlay.addEventListener('click', (event) => {
    if (event.target === overlay) overlay.classList.add('hidden');
  });
});

document.querySelectorAll('[data-copy]').forEach((button) => {
  button.addEventListener('click', async () => {
    const target = $(button.dataset.copy);
    const text = target.textContent;
    try {
      await navigator.clipboard.writeText(text);
      toast('已複製', 'ok');
    } catch (err) {
      toast('複製失敗，請手動選取', 'err');
    }
  });
});

$('btn-logout').addEventListener('click', async () => {
  stopPolling();
  try { await api('/api/logout', { method: 'POST' }); } catch (err) { /* 忽略 */ }
  state.session = null;
  show('login');
});

$('setup-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const password = $('setup-password').value;
  if (password !== $('setup-confirm').value) { toast('兩次輸入的密碼不一致', 'err'); return; }
  try {
    state.session = await api('/api/setup', { method: 'POST', body: { password } });
    toast('密碼設定完成', 'ok');
    await enterDashboard();
  } catch (err) { toast(err.message, 'err'); }
});

$('login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  try {
    state.session = await api('/api/login', { method: 'POST', body: { password: $('login-password').value } });
    $('login-password').value = '';
    await enterDashboard();
  } catch (err) { toast(err.message, 'err'); }
});

init();
