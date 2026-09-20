'use strict';

// 介面語系。預設依瀏覽器語言決定，使用者切換後存在 localStorage。
const I18N_LANGS = ['zh-Hant', 'en'];
const I18N_STORAGE_KEY = 'miniapp2api.lang';

const I18N_TEXT = {
  'zh-Hant': {
    // 首次設定
    'setup.title': '設定登入密碼',
    'setup.sub': '第一次啟動 miniapp2api，請先設定一組管理密碼。<br>之後開啟網頁都需要這組密碼才能進入號池。',
    'setup.password': '密碼（至少 6 個字元）',
    'setup.confirm': '再次輸入密碼',
    'setup.submit': '建立密碼並進入',

    // 登入
    'login.sub': '請輸入管理密碼以進入號池。',
    'login.password': '密碼',
    'login.submit': '登入',

    // 上方工具列
    'nav.settings': '設定',
    'nav.refresh': '重新整理',
    'nav.addAccount': '＋ 新增帳號',
    'nav.logout': '登出',

    // API 資訊
    'api.title': 'API 資訊',
    'api.key': 'API 金鑰',
    'api.copy': '複製',
    'api.hint': '完整金鑰只在產生時顯示一次，需要再次查看請到「設定」重新產生。',

    // 號池
    'pool.title': '號池',
    'pool.empty': '號池是空的，點選右上角「＋ 新增帳號」加入一組 JWT 即可。',

    // 頁尾
    'footer.made': '用 ❤️ 製作',
    'list.separator': '、',

    // 帳號表單
    'account.titleAdd': '新增帳號',
    'account.titleEdit': '編輯帳號',
    'account.modeJwt': '貼上 JWT',
    'account.modeLogin': '用 Email + 密碼',
    'account.name': '名稱（選填）',
    'account.namePlaceholder': '例如：主帳號',
    'account.jwt': 'JWT',
    'account.email': 'Email',
    'account.emailPlaceholder': 'miniapps.ai 的登入信箱',
    'account.passwordOptional': '密碼（選填）',
    'account.passwordOptionalHint': '填寫後 JWT 快到期時會自動重新登入續期',
    'account.passwordRequired': '密碼',
    'account.passwordLoginHint': 'miniapps.ai 的登入密碼',
    'account.passwordEditHint': '留空表示不變更',
    'account.cancel': '取消',
    'account.save': '儲存',
    'account.unnamed': '未命名帳號',

    // 帳號卡片
    'badge.enabled': '啟用',
    'badge.disabled': '停用',
    'badge.cooldown': '冷卻中',
    'badge.quota': '額度不足',
    'badge.quotaTitle': '額度不足的模型：{models}',
    'badge.expired': 'JWT 已過期',
    'badge.expiring': 'JWT 剩 {days} 天',
    'badge.autoRenew': '自動續期',
    'badge.autoRenewTitle': '已設定密碼，JWT 快到期時會自動重新登入',
    'kv.jwt': 'JWT',
    'kv.expires': 'JWT 到期',
    'kv.expiresValue': '{time}（剩 {days} 天）',
    'kv.requests': '使用次數',
    'kv.requestsValue': '{total} 次（成功 {success}／失敗 {failed}）',
    'kv.lastUsed': '最後使用',

    // 帳號卡片按鈕
    'action.test': '測試',
    'action.testing': '測試中…',
    'action.renew': '續期',
    'action.renewing': '續期中…',
    'action.edit': '編輯',
    'action.disable': '停用',
    'action.enable': '啟用',
    'action.delete': '刪除',

    // 統計
    'chip.total': '帳號總數',
    'chip.enabled': '啟用中',
    'chip.disabled': '停用',
    'chip.cooldown': '冷卻中',
    'chip.quota': '額度不足',
    'chip.expired': 'JWT 過期',
    'chip.requests': '請求數',
    'chip.success': '成功',
    'chip.failed': '失敗',

    // 設定
    'settings.title': '設定',
    'settings.regenKey': '重新產生 API 金鑰',
    'settings.regenHint': '重新產生後舊金鑰會立即失效，新的金鑰只會顯示一次。',
    'settings.addFromCatalog': '＋ 從模型目錄新增',
    'settings.changePassword': '修改登入密碼',
    'settings.currentPassword': '目前密碼',
    'settings.newPassword': '新密碼（至少 6 個字元）',
    'settings.updatePassword': '更新密碼',
    'settings.close': '關閉',

    // 新的 API 金鑰
    'key.title': '新的 API 金鑰',
    'key.hint': '這組金鑰只會顯示這一次，關閉後就無法再查看；請立刻複製保存，忘了只能重新產生一組。',
    'key.saved': '我已保存',

    // 模型
    'models.empty': '尚未設定模型',
    'models.remove': '移除',

    // 模型目錄
    'catalog.title': '模型目錄',
    'catalog.searchPlaceholder': '搜尋名稱、nativeId 或平台',
    'catalog.showRetired': '顯示已下架',
    'catalog.refetch': '重新抓取',
    'catalog.close': '關閉',
    'catalog.loading': '載入中…',
    'catalog.count': '顯示 {shown} / {total} 個模型',
    'catalog.empty': '沒有符合的模型',
    'catalog.price': '{points} 點',
    'catalog.add': '加入',
    'catalog.added': '已加入',
    'catalog.tagTools': '工具',
    'catalog.tagVision': '視覺',
    'catalog.tagReasoning': '推理',
    'catalog.tagRetired': '已下架',

    // 通知
    'toast.connectFailed': '無法連線到伺服器：{message}',
    'toast.testOk': '帳號可用，讀到 {count} 筆對話',
    'toast.testFailed': '測試失敗：{message}',
    'toast.renewed': '已重新登入，JWT 到期 {time}',
    'toast.renewFailed': '續期失敗：{message}',
    'toast.accountDeleted': '已刪除帳號',
    'toast.accountUpdated': '已更新帳號',
    'toast.accountAdded': '已新增帳號',
    'toast.accountAddedViaLogin': '已登入並新增帳號',
    'toast.needNewPassword': '請輸入新密碼',
    'toast.passwordUpdated': '密碼已更新',
    'toast.modelRemoved': '已移除模型 {id}',
    'toast.modelAdded': '已加入模型 {id}',
    'toast.refreshed': '已重新整理',
    'toast.copied': '已複製',
    'toast.copyFailed': '複製失敗，請手動選取',
    'toast.passwordMismatch': '兩次輸入的密碼不一致',
    'toast.setupDone': '密碼設定完成',

    // 確認視窗
    'confirm.deleteAccount': '確定要刪除「{name}」嗎？此動作會移除 {file}',
    'confirm.regenKey': '重新產生後，舊的 API 金鑰會立即失效，確定嗎？',
    'confirm.removeModel': '確定要移除模型「{id}」嗎？',

    // 伺服器回傳的錯誤碼
    'err.password_not_set': '尚未設定登入密碼',
    'err.password_already_set': '已經設定過密碼，請直接登入',
    'err.password_too_short': '密碼太短，至少需要 6 個字元',
    'err.wrong_password': '密碼錯誤',
    'err.login_required': '請先登入',
    'err.model_exists': '模型已經存在',
    'err.model_not_found': '找不到模型',
    'err.account_not_found': '找不到帳號',
    'err.account_missing_jwt': '此帳號缺少 JWT',
    'err.no_available_account': '號池中沒有可用的帳號',
    'err.no_catalog_account': '號池中沒有可用的帳號，無法讀取模型目錄',
    'err.catalog_failed': '讀取模型目錄失敗：{message}',
    'err.missing_credentials': '請填寫 JWT，或改填 Email 與密碼',
    'err.api_not_found': '找不到 API 路徑',
    'err.method_not_allowed': '不支援的請求方法',
    'err.login_failed': '登入失敗：{message}',
    'err.renew_failed': '續期失敗：{message}',
    'err.invalid_body': '請求內容格式錯誤：{message}',
    'err.request_failed': '請求失敗：{message}',
  },

  en: {
    // First run
    'setup.title': 'Set admin password',
    'setup.sub': 'This is the first run of miniapp2api. Set an admin password;<br>you will need it every time you open the Web UI.',
    'setup.password': 'Password (at least 6 characters)',
    'setup.confirm': 'Confirm password',
    'setup.submit': 'Create password and continue',

    // Sign in
    'login.sub': 'Enter the admin password to open the account pool.',
    'login.password': 'Password',
    'login.submit': 'Sign in',

    // Toolbar
    'nav.settings': 'Settings',
    'nav.refresh': 'Refresh',
    'nav.addAccount': '+ Add account',
    'nav.logout': 'Sign out',

    // API details
    'api.title': 'API details',
    'api.key': 'API key',
    'api.copy': 'Copy',
    'api.hint': 'The full key is shown only once. Regenerate it in Settings to see it again.',

    // Pool
    'pool.title': 'Account pool',
    'pool.empty': 'The pool is empty. Use "+ Add account" in the top right to add a JWT.',

    // Footer
    'footer.made': 'made with ❤️',
    'list.separator': ', ',

    // Account form
    'account.titleAdd': 'Add account',
    'account.titleEdit': 'Edit account',
    'account.modeJwt': 'Paste JWT',
    'account.modeLogin': 'Email + password',
    'account.name': 'Name (optional)',
    'account.namePlaceholder': 'for example: main account',
    'account.jwt': 'JWT',
    'account.email': 'Email',
    'account.emailPlaceholder': 'your miniapps.ai sign-in email',
    'account.passwordOptional': 'Password (optional)',
    'account.passwordOptionalHint': 'when set, an expiring JWT is renewed automatically',
    'account.passwordRequired': 'Password',
    'account.passwordLoginHint': 'your miniapps.ai password',
    'account.passwordEditHint': 'leave blank to keep the current one',
    'account.cancel': 'Cancel',
    'account.save': 'Save',
    'account.unnamed': 'Unnamed account',

    // Account card
    'badge.enabled': 'Enabled',
    'badge.disabled': 'Disabled',
    'badge.cooldown': 'Cooling down',
    'badge.quota': 'No credits',
    'badge.quotaTitle': 'Models out of credits: {models}',
    'badge.expired': 'JWT expired',
    'badge.expiring': 'JWT expires in {days}d',
    'badge.autoRenew': 'Auto-renew',
    'badge.autoRenewTitle': 'A password is stored, so an expiring JWT is renewed automatically',
    'kv.jwt': 'JWT',
    'kv.expires': 'JWT expires',
    'kv.expiresValue': '{time} ({days}d left)',
    'kv.requests': 'Requests',
    'kv.requestsValue': '{total} ({success} ok / {failed} failed)',
    'kv.lastUsed': 'Last used',

    // Account card actions
    'action.test': 'Test',
    'action.testing': 'Testing...',
    'action.renew': 'Renew',
    'action.renewing': 'Renewing...',
    'action.edit': 'Edit',
    'action.disable': 'Disable',
    'action.enable': 'Enable',
    'action.delete': 'Delete',

    // Stats
    'chip.total': 'Accounts',
    'chip.enabled': 'Enabled',
    'chip.disabled': 'Disabled',
    'chip.cooldown': 'Cooling',
    'chip.quota': 'No credits',
    'chip.expired': 'JWT expired',
    'chip.requests': 'Requests',
    'chip.success': 'Success',
    'chip.failed': 'Failed',

    // Settings
    'settings.title': 'Settings',
    'settings.regenKey': 'Regenerate API key',
    'settings.regenHint': 'Regenerating invalidates the old key immediately; the new key is shown only once.',
    'settings.addFromCatalog': '+ Add from model catalog',
    'settings.changePassword': 'Change admin password',
    'settings.currentPassword': 'Current password',
    'settings.newPassword': 'New password (at least 6 characters)',
    'settings.updatePassword': 'Update password',
    'settings.close': 'Close',

    // New API key
    'key.title': 'New API key',
    'key.hint': 'This key is shown only once and cannot be viewed again after you close this dialog. Copy it now; if you lose it, regenerate a new one.',
    'key.saved': 'I saved it',

    // Models
    'models.empty': 'No models configured yet',
    'models.remove': 'Remove',

    // Model catalog
    'catalog.title': 'Model catalog',
    'catalog.searchPlaceholder': 'search by name, nativeId or platform',
    'catalog.showRetired': 'Show retired',
    'catalog.refetch': 'Refetch',
    'catalog.close': 'Close',
    'catalog.loading': 'Loading...',
    'catalog.count': 'Showing {shown} / {total} models',
    'catalog.empty': 'No matching models',
    'catalog.price': '{points} credits',
    'catalog.add': 'Add',
    'catalog.added': 'Added',
    'catalog.tagTools': 'Tools',
    'catalog.tagVision': 'Vision',
    'catalog.tagReasoning': 'Reasoning',
    'catalog.tagRetired': 'Retired',

    // Toasts
    'toast.connectFailed': 'Cannot reach the server: {message}',
    'toast.testOk': 'Account works, {count} conversations found',
    'toast.testFailed': 'Test failed: {message}',
    'toast.renewed': 'Signed in again, JWT expires {time}',
    'toast.renewFailed': 'Renew failed: {message}',
    'toast.accountDeleted': 'Account deleted',
    'toast.accountUpdated': 'Account updated',
    'toast.accountAdded': 'Account added',
    'toast.accountAddedViaLogin': 'Signed in and added the account',
    'toast.needNewPassword': 'Enter a new password',
    'toast.passwordUpdated': 'Password updated',
    'toast.modelRemoved': 'Model {id} removed',
    'toast.modelAdded': 'Model {id} added',
    'toast.refreshed': 'Refreshed',
    'toast.copied': 'Copied',
    'toast.copyFailed': 'Copy failed, please select the text manually',
    'toast.passwordMismatch': 'The two passwords do not match',
    'toast.setupDone': 'Admin password set',

    // Confirm dialogs
    'confirm.deleteAccount': 'Delete "{name}"? This removes {file}',
    'confirm.regenKey': 'The old API key stops working immediately. Continue?',
    'confirm.removeModel': 'Remove model "{id}"?',

    // Error codes returned by the server
    'err.password_not_set': 'Admin password is not set',
    'err.password_already_set': 'An admin password is already set; please sign in',
    'err.password_too_short': 'Password is too short, use at least 6 characters',
    'err.wrong_password': 'Wrong password',
    'err.login_required': 'Not signed in',
    'err.model_exists': 'Model already exists',
    'err.model_not_found': 'Model not found',
    'err.account_not_found': 'Account not found',
    'err.account_missing_jwt': 'This account has no JWT',
    'err.no_available_account': 'No usable account in the pool',
    'err.no_catalog_account': 'No usable account in the pool to read the model catalog',
    'err.catalog_failed': 'Cannot fetch the model catalog: {message}',
    'err.missing_credentials': 'Provide a JWT, or an email and password',
    'err.api_not_found': 'Unknown API endpoint',
    'err.method_not_allowed': 'Method not allowed',
    'err.login_failed': 'Sign-in failed: {message}',
    'err.renew_failed': 'Renew failed: {message}',
    'err.invalid_body': 'Invalid request body: {message}',
    'err.request_failed': 'Request failed: {message}',
  },
};

let i18nLang = 'zh-Hant';

// detectLanguage 依 localStorage、瀏覽器語言依序決定預設語系。
function detectLanguage() {
  try {
    const saved = localStorage.getItem(I18N_STORAGE_KEY);
    if (saved && I18N_LANGS.indexOf(saved) >= 0) return saved;
  } catch (err) { /* localStorage 可能被封鎖，忽略 */ }
  const nav = String(navigator.language || '').toLowerCase();
  return nav.indexOf('zh') === 0 ? 'zh-Hant' : 'en';
}

function currentLanguage() {
  return i18nLang;
}

// setLanguage 切換語系並記住選擇。
function setLanguage(lang) {
  i18nLang = I18N_LANGS.indexOf(lang) >= 0 ? lang : 'zh-Hant';
  try { localStorage.setItem(I18N_STORAGE_KEY, i18nLang); } catch (err) { /* 忽略 */ }
}

function formatText(text, params) {
  if (!params) return text;
  let out = text;
  for (const name of Object.keys(params)) {
    out = out.split('{' + name + '}').join(String(params[name]));
  }
  return out;
}

// t 取出目前語系的文字；缺少時退回繁體中文，再退回 key 本身。
function t(key, params) {
  const table = I18N_TEXT[i18nLang] || {};
  const text = table[key] !== undefined ? table[key] : I18N_TEXT['zh-Hant'][key];
  if (text === undefined) return key;
  return formatText(text, params);
}

// tOr 取出目前語系的文字，缺少時改用 fallback（例如伺服器回傳的英文訊息）。
function tOr(key, params, fallback) {
  const table = I18N_TEXT[i18nLang] || {};
  if (table[key] === undefined) return fallback;
  return formatText(table[key], params);
}

// applyI18n 把 data-i18n* 屬性套用到畫面上。
function applyI18n(root) {
  const scope = root || document;
  scope.querySelectorAll('[data-i18n]').forEach((node) => {
    node.textContent = t(node.dataset.i18n);
  });
  scope.querySelectorAll('[data-i18n-html]').forEach((node) => {
    node.innerHTML = t(node.dataset.i18nHtml);
  });
  scope.querySelectorAll('[data-i18n-placeholder]').forEach((node) => {
    node.placeholder = t(node.dataset.i18nPlaceholder);
  });
  scope.querySelectorAll('[data-i18n-title]').forEach((node) => {
    node.title = t(node.dataset.i18nTitle);
  });
  document.documentElement.lang = i18nLang === 'en' ? 'en' : 'zh-Hant';
  scope.querySelectorAll('[data-lang-btn]').forEach((node) => {
    node.classList.toggle('is-active', node.dataset.langBtn === i18nLang);
  });
}
