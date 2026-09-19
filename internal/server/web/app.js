'use strict';

const state = {
  session: null,
  accounts: [],
  editing: null,
  timer: null,
  keyVisible: false,
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

function maskKey(key) {
  if (!key) return '-';
  if (state.keyVisible || key.length <= 14) return key;
  return key.slice(0, 10) + '••••••••' + key.slice(-4);
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
  $('api-key').textContent = maskKey(session.api_key);
  $('data-dir').textContent = session.data_dir || '-';
  $('req-key').checked = !!session.require_api_key;
  $('health-dot').style.background = (session.pool && session.pool.enabled > 0) ? 'var(--ok)' : 'var(--err)';

  const models = session.models || [];
  $('model-list').innerHTML = models.length
    ? models.map((m) => `<div><b>${esc(m.id)}</b> — ${esc(m.name || '')}</div>`).join('')
    : '<div>尚未設定模型</div>';
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
  return badges.join(' ');
}

function renderAccounts() {
  const container = $('accounts');
  if (!state.accounts.length) {
    container.className = '';
    container.innerHTML = '<div class="empty">號池是空的，點選右上角「＋ 新增帳號」開始加入 JWT 與 CSRF 資訊。</div>';
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
        <div class="acct-id">${esc(account.email || '')} ${esc(account.id)}</div>
        <div class="kv"><span>JWT</span><span>${esc(account.jwt || '-')}</span></div>
        <div class="kv"><span>JWT 到期</span><span>${esc(expiry)}</span></div>
        <div class="kv"><span>使用次數</span><span>${esc(stats.requests || 0)} 次（成功 ${esc(stats.success || 0)}／失敗 ${esc(stats.failed || 0)}）</span></div>
        <div class="kv"><span>最後使用</span><span>${esc(fmtTime(account.last_used_at))}</span></div>
        ${errLine}
        <div class="acct-actions">
          <button class="btn btn-sm" data-action="check" data-id="${esc(account.id)}">測試</button>
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

function openAccountModal(account) {
  state.editing = account || null;
  const editing = !!account;
  $('account-title').textContent = editing ? '編輯帳號' : '新增帳號';
  $('account-hint').textContent = editing
    ? 'JWT／CSRF 欄位留空表示不變更，填寫則會覆蓋原本的值。'
    : '貼上瀏覽器登入 miniapps.ai 後的 JWT 與 CSRF 資訊，會存成 auths/{uuid}.json。';
  $('acct-name').value = editing ? account.name || '' : '';
  $('acct-jwt').value = '';
  $('acct-csrf-cookie').value = '';
  $('acct-csrf-token').value = '';
  $('acct-jwt').required = !editing;
  $('acct-csrf-cookie').required = !editing;
  $('acct-csrf-token').required = !editing;
  $('modal-account').classList.remove('hidden');
  $('acct-name').focus();
}

$('account-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const payload = {
    name: $('acct-name').value.trim(),
    jwt: $('acct-jwt').value.trim(),
    csrf_cookie: $('acct-csrf-cookie').value.trim(),
    csrf_token: $('acct-csrf-token').value.trim(),
  };

  const button = $('account-submit');
  button.disabled = true;
  try {
    if (state.editing) {
      await api(`/api/accounts/${state.editing.id}`, { method: 'PUT', body: payload });
      toast('已更新帳號', 'ok');
    } else {
      await api('/api/accounts', { method: 'POST', body: payload });
      toast('已新增帳號', 'ok');
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

$('req-key').addEventListener('change', async (event) => {
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { require_api_key: event.target.checked } });
    renderSession();
    toast('已更新設定', 'ok');
  } catch (err) {
    toast(err.message, 'err');
    renderSession();
  }
});

$('btn-regen-key').addEventListener('click', async () => {
  if (!confirm('重新產生後，舊的 API 金鑰會立即失效，確定嗎？')) return;
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { regenerate_api_key: true } });
    state.keyVisible = true;
    renderSession();
    toast('已產生新的 API 金鑰', 'ok');
  } catch (err) { toast(err.message, 'err'); }
});

$('btn-reveal-key').addEventListener('click', () => {
  state.keyVisible = !state.keyVisible;
  renderSession();
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
    if (button.dataset.copy === 'api-key' && !state.keyVisible) {
      state.keyVisible = true;
      renderSession();
    }
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
