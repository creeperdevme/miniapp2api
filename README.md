# miniapp2api

把 **miniapps.ai** 的帳號池包成 **OpenAI 相容的 `/v1` API**，並附上一個網頁介面讓你管理號池。

用 Go 寫成、沒有外部相依套件，單一執行檔即可運行。

## 功能

- 第一次啟動時，網頁會要求設定一組管理密碼（存成 PBKDF2-HMAC-SHA256 雜湊，不會存明文）。
- 登入後直接進入 **號池** 頁面，右上角有「**＋ 新增帳號**」。
- 新增帳號只需填 **JWT**、**CSRF_Cookie**、**CSRF_Token**，會依 JWT 裡的 email 存成 `auths/{email}.json`。
- 提供 OpenAI 相容端點：`GET /v1/models`、`POST /v1/chat/completions`（支援 `stream`）。
- 模型清單預設是空的，從上游的模型目錄（`GET /ai-models`）挑選並加入即可。
- 自動挑選最少使用的帳號，出錯的帳號會進入冷卻並自動換下一個帳號重試。

## 快速開始

```powershell
go build -o miniapp2api.exe .
.\miniapp2api.exe
```

啟動後會自動開啟瀏覽器（`-no-browser` 可關閉），並在 console 顯示：

```
 資料目錄   D:\Miniapp.ai Proxy
 網頁介面   http://127.0.0.1:8787/
 OpenAI API http://127.0.0.1:8787/v1
 API 金鑰   sk-m2a-4163••••••••bd7b（完整金鑰請在網頁介面重新產生）
```

### 命令列參數

| 參數 | 說明 |
| --- | --- |
| `-addr 127.0.0.1:8787` | 指定監聽位址（預設讀取 `config.json`） |
| `-data <目錄>` | 指定資料目錄（預設為執行檔所在目錄） |
| `-no-browser` | 啟動後不要自動開啟瀏覽器 |

## 網頁介面

1. **首次開啟**：設定管理密碼（至少 6 個字元）。
2. **登入後**：看到號池列表、API Base URL、API 金鑰（只顯示遮罩）與使用統計。
3. **新增帳號**：填入下列三個欄位，按「儲存」。
   - `JWT`：瀏覽器 DevTools → Application → Cookies → `api.miniapps.ai` → `jwt`
   - `CSRF_Cookie`：同處的 `__Host-miniapps.x-csrf-token`
   - `CSRF_Token`：DevTools → Network → 任一 `POST /chat` 請求 → 標頭 `x-csrf-token`
4. 每張帳號卡片可以 **測試**（只呼叫 `quickAccess`，不消耗 AI 額度）、**編輯**、**停用／啟用**、**刪除**。
5. 右上角 **設定** 可以管理模型：清單預設是空的，按「＋ 從模型目錄新增」，
   從上游的 `GET /ai-models` 目錄把模型「加入」或「移除」。目錄會快取 10 分鐘，需要重抓時按「重新抓取」。

密碼與 API 金鑰都可以在右上角 **設定** 中變更；API 金鑰只在產生時顯示一次，之後要查看只能按
「重新產生 API 金鑰」換一組新的（舊的會立即失效）。

## 使用 OpenAI API

任何支援 OpenAI 的客戶端都能直接使用，只要把 Base URL 指到 `http://127.0.0.1:8787/v1`。

`model` 請填 **設定** 中已加入的模型名稱（`GET /v1/models` 可列出目前可用的名稱）。

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer sk-m2a-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "chat-latest",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8787/v1", api_key="sk-m2a-xxxx")

response = client.chat.completions.create(
    model="chat-latest",
    messages=[{"role": "user", "content": "你好"}],
)
print(response.choices[0].message.content)
```

串流也支援（`"stream": true`）。

### 模型對應

**預設沒有任何模型**，第一次使用請先到右上角 **設定** →「＋ 從模型目錄新增」把要用的模型加入。

- 目錄直接來自上游的 `GET /ai-models`，加入時會自動帶入該模型的 `model_id`（上游的 UUID）。
- 對外模型名稱預設取上游的 `nativeId`（例如 `chat-latest`），撞名時自動加上變體標籤或序號。
- 所有模型共用同一個寫死的 `toolId`（程式內的 `config.DefaultToolID`），不需要也不能設定。
- 傳入未知的模型名稱（例如 `gpt-4o`）時，會自動使用清單中的第一個模型，方便現成客戶端直接接入；
  清單是空的時候 `POST /v1/chat/completions` 會回 `503 no_model_configured`。
- 也可以直接編輯 `config.json` 的 `models` 陣列。

## 檔案說明

### `config.json`

```json
{
  "password_salt": "…",
  "password_hash": "…",
  "api_key": "sk-m2a-…",
  "require_api_key": true,
  "host": "127.0.0.1",
  "port": 8787,
  "request_timeout_seconds": 180,
  "models": [
    {
      "id": "chat-latest",
      "name": "ChatGPT Latest",
      "model_id": "3d0b890a-7658-4165-b860-e030b620e3e8",
      "revision": 1,
      "language": "zh"
    },
    {
      "id": "claude-sonnet-4-5",
      "name": "Claude 4.5 Sonnet",
      "model_id": "90fda230-de04-4133-8b34-b989d564925e",
      "revision": 1,
      "language": "zh"
    }
  ]
}
```

`models` 預設是空陣列；`tool_id` 不放在設定檔裡，一律使用程式內寫死的值。

### `auths/{email}.json`

每個帳號一個檔案，內容只有 `jwt`、`csrf_cookie`、`csrf_token`、啟用狀態與使用統計
（`modelId` 等一律來自 `config.json` 的模型設定，不放在帳號裡）。
檔名取自 JWT 裡的 email，例如 `auths/tester@example.com.json`；同一個 email 有多筆帳號時
會依序加上 `-2`、`-3`。舊版以 uuid 命名的檔案會在啟動時自動改成 email 檔名。
檔案權限為 `0600`，可自行備份或複製到其他機器。

## 中轉流程

1. 收到 `POST /v1/chat/completions`，把訊息轉成單一提示文字。
2. 從號池挑選最久未使用的啟用帳號，以它的 JWT / CSRF 呼叫上游。
3. `POST /chat` 取得 `conversationId`，接著輪詢 `GET /conversations/quickAccess` 等待 AI 輸出完成。
4. 以 `GET /conversations/{id}/messages` 取得完整回覆，轉成 OpenAI 格式回傳。
5. 帳號發生錯誤時記入統計：憑證或網路類錯誤會讓該帳號冷卻 60 秒，本次請求自動改用下一個帳號重試（最多 3 個）。
   額度不足（402／412）只記錄不冷卻，因為那通常代表「這個模型沒額度」，冷卻會連帶讓同帳號的其他模型也不能用。

## 常見狀態

| 情況 | 回應 |
| --- | --- |
| 上游額度不足（HTTP 412 / 402） | `402 insufficient_quota` / `insufficient_credits` |
| 上游限流 | `429 rate_limit_exceeded` |
| 等待回覆超過 180 秒 | `504 upstream_timeout` |
| 號池沒有可用帳號（停用或冷卻中） | `503 no_available_account` |
| API 金鑰錯誤或缺漏 | `401 invalid_api_key` / `missing_api_key` |
| 尚未加入任何模型 | `503 no_model_configured` |

帳號失效（JWT 過期、CSRF 錯誤）會在網頁上以紅色錯誤訊息顯示，該帳號也會暫時進入冷卻。

## 開發

```powershell
go vet ./...
go test ./...
```

專案結構：

```
main.go                     啟動、參數、開瀏覽器
internal/config/            config.json（密碼雜湊、API 金鑰、模型設定）
internal/store/             auths/{email}.json 號池
internal/miniapps/          api.miniapps.ai 用戶端與回應解析
internal/openai/            OpenAI 相容格式與訊息轉換
internal/server/            HTTP 路由、網頁介面與 /v1 端點
internal/server/web/        網頁介面（index.html / app.js）
```
