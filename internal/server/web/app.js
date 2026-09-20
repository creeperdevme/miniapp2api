'use strict';

const state = {
  session: null,
  accounts: [],
  summary: null,
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
    let message;
    if (typeof detail === 'string') message = detail;
    else if (detail && detail.message) message = detail.message;
    else message = `HTTP ${res.status}`;
    // 伺服器會帶上穩定的 code，前端據此顯示對應語系的訊息。
    const code = (data && data.code) || (detail && detail.code) || '';
    const error = new Error(code ? tOr('err.' + code, { message }, message) : message);
    error.status = res.status;
    error.code = code;
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
    toast(t('toast.connectFailed', { message: err.message }), 'err');
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
  state.summary = data;
  const chips = [
    [t('chip.total'), data.total || 0],
    [t('chip.enabled'), data.enabled || 0],
    [t('chip.disabled'), data.disabled || 0],
    [t('chip.cooldown'), data.cooldown || 0],
    [t('chip.quota'), data.quota || 0],
    [t('chip.expired'), data.expired || 0],
    [t('chip.requests'), data.requests || 0],
    [t('chip.success'), data.success || 0],
    [t('chip.failed'), data.failed || 0],
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
  if (account.enabled) badges.push(`<span class="badge ok">${esc(t('badge.enabled'))}</span>`);
  else badges.push(`<span class="badge">${esc(t('badge.disabled'))}</span>`);

  if (account.cooling_down) {
    badges.push(`<span class="badge warn">${esc(t('badge.cooldown'))}</span>`);
  }
  if (account.quota_exceeded) {
    const models = account.quota_models || [];
    const title = models.length
      ? ` title="${esc(t('badge.quotaTitle', { models: models.join(t('list.separator')) }))}"`
      : '';
    badges.push(`<span class="badge err"${title}>${esc(t('badge.quota'))}</span>`);
  }
  const left = daysLeft(account.expires_at);
  if (left !== null) {
    if (left < 0) badges.push(`<span class="badge err">${esc(t('badge.expired'))}</span>`);
    else if (left <= 3) badges.push(`<span class="badge warn">${esc(t('badge.expiring', { days: left }))}</span>`);
  }
  if (account.password) {
    badges.push(`<span class="badge ok" title="${esc(t('badge.autoRenewTitle'))}">${esc(t('badge.autoRenew'))}</span>`);
  }
  return badges.join(' ');
}

function renderAccounts() {
  const container = $('accounts');
  if (!state.accounts.length) {
    container.className = '';
    container.innerHTML = `<div class="empty">${esc(t('pool.empty'))}</div>`;
    return;
  }
  container.className = 'grid';
  container.innerHTML = state.accounts.map((account) => {
    const stats = account.stats || {};
    const left = daysLeft(account.expires_at);
    const expiry = account.expires_at
      ? (left !== null ? t('kv.expiresValue', { time: fmtTime(account.expires_at), days: left }) : fmtTime(account.expires_at))
      : '-';
    const errLine = account.last_error ? `<div class="err-line">${esc(account.last_error)}</div>` : '';

    return `
      <div class="acct ${account.enabled ? '' : 'off'}">
        <div class="acct-top">
          <div class="acct-name">${esc(account.name || account.email || t('account.unnamed'))}</div>
          <div class="spacer"></div>
          ${accountBadges(account)}
        </div>
        <div class="acct-id">${esc(account.email || account.id)} · auths/${esc(account.file_name || '')}</div>
        <div class="kv"><span>${esc(t('kv.jwt'))}</span><span>${esc(account.jwt || '-')}</span></div>
        <div class="kv"><span>${esc(t('kv.expires'))}</span><span>${esc(expiry)}</span></div>
        <div class="kv"><span>${esc(t('kv.requests'))}</span><span>${esc(t('kv.requestsValue', { total: stats.requests || 0, success: stats.success || 0, failed: stats.failed || 0 }))}</span></div>
        <div class="kv"><span>${esc(t('kv.lastUsed'))}</span><span>${esc(fmtTime(account.last_used_at))}</span></div>
        ${errLine}
        <div class="acct-actions">
          <button class="btn btn-sm" data-action="check" data-id="${esc(account.id)}">${esc(t('action.test'))}</button>
          ${account.password ? `<button class="btn btn-sm" data-action="renew" data-id="${esc(account.id)}">${esc(t('action.renew'))}</button>` : ''}
          <button class="btn btn-sm" data-action="edit" data-id="${esc(account.id)}">${esc(t('action.edit'))}</button>
          <button class="btn btn-sm" data-action="toggle" data-id="${esc(account.id)}">${esc(account.enabled ? t('action.disable') : t('action.enable'))}</button>
          <button class="btn btn-sm btn-danger" data-action="delete" data-id="${esc(account.id)}">${esc(t('action.delete'))}</button>
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
    button.textContent = t('action.testing');
    try {
      const result = await api(`/api/accounts/${id}/check`, { method: 'POST' });
      if (result.ok) toast(t('toast.testOk', { count: result.conversations }), 'ok');
      else toast(t('toast.testFailed', { message: result.error }), 'err');
    } catch (err) {
      toast(t('toast.testFailed', { message: err.message }), 'err');
    } finally {
      button.disabled = false;
      button.textContent = t('action.test');
      refreshAccounts().catch(() => {});
    }
    return;
  }

  if (button.dataset.action === 'edit') { openAccountModal(account); return; }

  if (button.dataset.action === 'renew') {
    button.disabled = true;
    button.textContent = t('action.renewing');
    try {
      const updated = await api(`/api/accounts/${id}/renew`, { method: 'POST' });
      toast(t('toast.renewed', { time: fmtTime(updated.expires_at) }), 'ok');
    } catch (err) {
      toast(t('toast.renewFailed', { message: err.message }), 'err');
    } finally {
      button.disabled = false;
      button.textContent = t('action.renew');
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
    const name = account.name || account.id;
    if (!confirm(t('confirm.deleteAccount', { name, file: `auths/${account.id}.json` }))) return;
    try {
      await api(`/api/accounts/${id}`, { method: 'DELETE' });
      toast(t('toast.accountDeleted'), 'ok');
      await refreshAccounts();
    } catch (err) { toast(err.message, 'err'); }
  }
});

// ---------------------------------------------------------------- 帳號表單

// accountModes 描述兩種新增帳號模式要顯示的密碼欄位文案（i18n key）。
const accountModes = {
  jwt: {
    passwordLabel: 'account.passwordOptional',
    passwordHint: 'account.passwordOptionalHint',
  },
  login: {
    passwordLabel: 'account.passwordRequired',
    passwordHint: 'account.passwordLoginHint',
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

  const labelKey = editing ? 'account.passwordOptional' : accountModes[mode].passwordLabel;
  const hintKey = editing ? 'account.passwordEditHint' : accountModes[mode].passwordHint;
  $('acct-password-label').dataset.i18n = labelKey;
  $('acct-password-label').textContent = t(labelKey);
  $('acct-password').dataset.i18nPlaceholder = hintKey;
  $('acct-password').placeholder = t(hintKey);
}

function openAccountModal(account) {
  state.editing = account || null;
  const editing = !!account;
  const titleKey = editing ? 'account.titleEdit' : 'account.titleAdd';
  $('account-title').dataset.i18n = titleKey;
  $('account-title').textContent = t(titleKey);
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
      toast(t('toast.accountUpdated'), 'ok');
    } else {
      await api('/api/accounts', { method: 'POST', body: payload });
      toast(t(login ? 'toast.accountAddedViaLogin' : 'toast.accountAdded'), 'ok');
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
  if (!confirm(t('confirm.regenKey'))) return;
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { regenerate_api_key: true } });
    renderSession();
  } catch (err) { toast(err.message, 'err'); }
});

$('btn-change-password').addEventListener('click', async () => {
  const current = $('cur-password').value;
  const next = $('new-password').value;
  if (!next) { toast(t('toast.needNewPassword'), 'err'); return; }
  try {
    state.session = await api('/api/settings', { method: 'PUT', body: { current_password: current, new_password: next } });
    $('cur-password').value = '';
    $('new-password').value = '';
    toast(t('toast.passwordUpdated'), 'ok');
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
    container.innerHTML = `<div class="empty" style="padding:26px">${esc(t('models.empty'))}</div>`;
    return;
  }

  container.innerHTML = models.map((model) => `
    <div class="model-row">
      <code>${esc(model.id)}</code>
      <span class="model-name">${esc(model.name || '')}</span>
      <div class="spacer"></div>
      <button class="btn btn-sm btn-danger" data-action="remove-model" data-id="${esc(model.id)}">${esc(t('models.remove'))}</button>
    </div>`).join('');
}

function openCatalogModal() {
  $('modal-catalog').classList.remove('hidden');
  if (!state.catalogLoaded) loadCatalog(false);
}

async function loadCatalog(refresh) {
  state.catalog = [];
  $('cat-meta').textContent = t('catalog.loading');
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

  $('cat-meta').textContent = t('catalog.count', { shown: items.length, total: catalog.length });
  if (!items.length) {
    $('cat-list').innerHTML = `<div class="empty">${esc(t('catalog.empty'))}</div>`;
    return;
  }

  $('cat-list').innerHTML = items.map((item) => {
    const already = added.has(item.id);
    const tags = [];
    if (item.has_tools) tags.push(t('catalog.tagTools'));
    if (item.has_vision) tags.push(t('catalog.tagVision'));
    if (item.is_reasoning) tags.push(t('catalog.tagReasoning'));
    if (!item.available) tags.push(t('catalog.tagRetired'));

    const parts = [];
    if (!already) parts.push(suggestModelId(item, used));
    parts.push(item.native_id, item.platform, t('catalog.price', { points: item.credit_price }));
    if (tags.length) parts.push(tags.join(t('list.separator')));

    return `
      <div class="cat-item">
        <div>
          <div class="cat-title">${esc(item.title)}</div>
          <div class="cat-sub">${esc(parts.join(' · '))}</div>
        </div>
        <div class="spacer"></div>
        <button class="btn btn-sm ${already ? '' : 'btn-primary'}" data-add="${esc(item.id)}"${already ? ' disabled' : ''}>${esc(already ? t('catalog.added') : t('catalog.add'))}</button>
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
    toast(t('toast.modelAdded', { id: alias }), 'ok');
  } catch (err) {
    button.disabled = false;
    toast(err.message, 'err');
  }
});

$('model-list').addEventListener('click', async (event) => {
  const button = event.target.closest('button[data-action="remove-model"]');
  if (!button) return;
  const id = button.dataset.id;
  if (!confirm(t('confirm.removeModel', { id }))) return;
  try {
    state.session = await api(`/api/models/${encodeURIComponent(id)}`, { method: 'DELETE' });
    renderSession();
    renderCatalog();
    toast(t('toast.modelRemoved', { id }), 'ok');
  } catch (err) {
    toast(err.message, 'err');
  }
});

// ---------------------------------------------------------------- 其他互動

$('btn-add').addEventListener('click', () => openAccountModal(null));
$('btn-refresh').addEventListener('click', () => refreshAccounts().then(() => toast(t('toast.refreshed'))).catch((err) => toast(err.message, 'err')));
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
      toast(t('toast.copied'), 'ok');
    } catch (err) {
      toast(t('toast.copyFailed'), 'err');
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
  if (password !== $('setup-confirm').value) { toast(t('toast.passwordMismatch'), 'err'); return; }
  try {
    state.session = await api('/api/setup', { method: 'POST', body: { password } });
    toast(t('toast.setupDone'), 'ok');
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

// ---------------------------------------------------------------- 語系

// refreshDynamicText 重新套用由程式產生、不經過 data-i18n 的文案。
function refreshDynamicText() {
  if (state.summary) renderChips(state.summary);
  if (state.session) renderModels();
  renderAccounts();
  if (state.catalogLoaded) renderCatalog();
  if (!$('modal-account').classList.contains('hidden')) {
    const titleKey = state.editing ? 'account.titleEdit' : 'account.titleAdd';
    $('account-title').dataset.i18n = titleKey;
    $('account-title').textContent = t(titleKey);
    setAccountMode(state.accountMode);
  }
}

document.querySelectorAll('[data-lang-btn]').forEach((button) => {
  button.addEventListener('click', () => {
    setLanguage(button.dataset.langBtn);
    applyI18n();
    refreshDynamicText();
  });
});

// 啟動時先套用語系，再開始抓資料。
setLanguage(detectLanguage());
applyI18n();

init();
