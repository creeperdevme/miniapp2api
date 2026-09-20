# miniapp2api

**繁體中文** | [English](README.en.md)

把 [miniapps.ai](https://miniapps.ai) 的帳號池包成 **OpenAI 相容的 `/v1` API**，並附上一個網頁介面讓你管理號池。
純 Go 實作、沒有外部相依套件，單一執行檔就能跑（Windows / Linux）。

> 這是一支自用的中轉工具。請自行確認使用方式符合上游服務的條款，帳號狀態與額度問題請自行負責。

## 目錄

- [功能](#功能)
- [安裝](#安裝)
  - [下載預先編譯的執行檔](#下載預先編譯的執行檔)
  - [Windows](#windows)
  - [Linux](#linux)
  - [從原始碼建置](#從原始碼建置)
- [第一次啟動](#第一次啟動)
- [新增帳號](#新增帳號)
- [呼叫 OpenAI API](#呼叫-openai-api)
- [資料檔案](#資料檔案)
- [中轉流程](#中轉流程)
- [常見問題](#常見問題)
- [開發與發行](#開發與發行)
- [認可與致謝](#認可與致謝)

## 功能

- OpenAI 相容端點：`GET /v1/models`、`POST /v1/chat/completions`（支援 `stream`）。
- 網頁管理介面：號池清單與統計、模型管理、API 金鑰管理、登入密碼變更。
- 帳號有兩種新增方式：貼上 **JWT**，或填 **Email + 密碼** 讓中轉自己登入換一組 JWT。
- 網頁介面可切換 **繁體中文／English**（右上角），console 輸出固定為英文。
- **CSRF 全自動**：需要 CSRF 的請求會自動向 `/auth/csrf` 取得配對，不用手動抓 Cookie 與標頭。
- **JWT 自動續期**：帳號有存密碼時，JWT 剩不到 3 天會自動重新登入換新，也可以隨時手動按「續期」。
- 自動挑選最久未使用的啟用帳號；失敗的帳號會進入冷卻，本次請求自動換下一個帳號重試。
- 管理密碼以 PBKDF2-HMAC-SHA256 保存（不存明文）；API 金鑰只在網頁介面顯示一次，不會印在 console。

## 安裝

### 下載預先編譯的執行檔

到 [Releases](https://github.com/creeperdevme/miniapp2api/releases) 下載對應平台的檔案：

| 平台 | 檔案 |
| --- | --- |
| Windows x64 | `miniapp2api_<版本>_windows_amd64.zip` |
| Windows ARM64 | `miniapp2api_<版本>_windows_arm64.zip` |
| Linux x64 | `miniapp2api_<版本>_linux_amd64.tar.gz` |
| Linux ARM64 | `miniapp2api_<版本>_linux_arm64.tar.gz` |

也可以到 **Actions → release → Run workflow** 手動觸發建置，再到該次執行的 **Artifacts** 下載，不需要先打 tag。

### Windows

1. 解壓縮到一個固定的資料夾，例如 `D:\miniapp2api`。

   > 預設的資料目錄就是**執行檔所在資料夾**，`config.json` 與 `auths/` 都會建在那裡，所以建議放在固定位置。

2. 雙擊 `miniapp2api.exe`，或在 PowerShell 執行：

   ```powershell
   cd D:\miniapp2api
   .\miniapp2api.exe
   ```

3. 啟動後會自動開啟瀏覽器（加 `-no-browser` 可關閉）；console 會顯示資料目錄、網址與號池數量，**不會印出 API 金鑰**。
4. 如果跳出 Windows SmartScreen 警告，選「更多資訊」→「仍要執行」。

常用參數：

```powershell
.\miniapp2api.exe -h                                              # 顯示所有參數
.\miniapp2api.exe -data D:\miniapp2api-data -no-browser           # 自訂資料目錄、不開瀏覽器
.\miniapp2api.exe -addr 127.0.0.1:9000                            # 換連接埠（預設 8787）
```

**讓區網其他裝置連線**（例如手機、另一台電腦）：

```powershell
# 1) 改成監聽所有網卡
.\miniapp2api.exe -addr 0.0.0.0:8787

# 2) 開放防火牆（需系統管理員權限的 PowerShell）
New-NetFirewallRule -DisplayName "miniapp2api" -Direction Inbound -Protocol TCP -LocalPort 8787 -Action Allow

# 3) 查自己的區網 IP
ipconfig
```

之後其他裝置就能連 `http://<你的區網IP>:8787/`，`/v1` 的 Base URL 也換成同樣的位址。

> 對外開放等於把 API 暴露在網路上，請務必保管好 API 金鑰（`config.json` 的 `require_api_key` 預設為 `true`，呼叫 `/v1` 必須帶金鑰）。

**開機自動啟動**（工作排程器）：

```powershell
schtasks /create /tn miniapp2api /tr "D:\miniapp2api\miniapp2api.exe -data D:\miniapp2api -no-browser" /sc onlogon
schtasks /run /tn miniapp2api       # 立刻啟動
schtasks /delete /tn miniapp2api /f # 移除
```

### Linux

```bash
# 1) 下載並解壓（版本號請換成實際下載到的）
tar -xzf miniapp2api_<版本>_linux_amd64.tar.gz
chmod +x miniapp2api

# 2) 安裝到 PATH（可選）
sudo install -m 755 miniapp2api /usr/local/bin/miniapp2api

# 3) 建立資料目錄並啟動
sudo mkdir -p /opt/miniapp2api
miniapp2api -data /opt/miniapp2api -no-browser
```

> 伺服器通常沒有桌面環境，開瀏覽器會失敗但不影響服務，加 `-no-browser` 即可。
> 只有自己用的話建議維持預設的 `127.0.0.1:8787`；要讓外部連線再改成 `0.0.0.0:8787`。

**用 systemd 常駐**：

`/etc/systemd/system/miniapp2api.service`

```ini
[Unit]
Description=miniapp2api
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=miniapp2api
Group=miniapp2api
WorkingDirectory=/opt/miniapp2api
ExecStart=/usr/local/bin/miniapp2api -data /opt/miniapp2api -addr 0.0.0.0:8787 -no-browser
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# 建立專用使用者（不給登入 shell）
sudo useradd --system --home /opt/miniapp2api --shell /usr/sbin/nologin miniapp2api
sudo chown -R miniapp2api:miniapp2api /opt/miniapp2api

sudo systemctl daemon-reload
sudo systemctl enable --now miniapp2api
sudo systemctl status miniapp2api
journalctl -u miniapp2api -f
```

> 權限提醒：`auths/` 內的帳號檔含 JWT 與（如果填了）明文密碼，程式會以 `0600` 建立，
> 但用 systemd 常駐時記得整個資料夾的擁有者是執行身分（上面的 `chown`）。

**開放防火牆**：

```bash
sudo ufw allow 8787/tcp                                                   # Debian / Ubuntu
sudo firewall-cmd --add-port=8787/tcp --permanent && sudo firewall-cmd --reload   # RHEL / Fedora
```

**不用 systemd 的臨時跑法**：

```bash
nohup miniapp2api -data /opt/miniapp2api -no-browser > /var/log/miniapp2api.log 2>&1 &
```

### 從原始碼建置

需要 Go 1.24 以上：

```bash
git clone https://github.com/creeperdevme/miniapp2api.git
cd miniapp2api
go build -o miniapp2api .          # Windows 請用 -o miniapp2api.exe
./miniapp2api
```

跨平台編譯（不需安裝對應平台的工具鏈）：

```bash
GOOS=windows GOARCH=amd64 go build -o miniapp2api.exe .
GOOS=linux   GOARCH=arm64 go build -o miniapp2api-linux-arm64 .
```

### 命令列參數

| 參數 | 說明 |
| --- | --- |
| `-addr 127.0.0.1:8787` | 指定監聽位址；沒給就讀 `config.json`（預設 `127.0.0.1:8787`） |
| `-data <目錄>` | 指定資料目錄，存放 `config.json` 與 `auths/`；預設為執行檔所在目錄 |
| `-no-browser` | 啟動後不要自動開啟瀏覽器 |
| `-h` | 顯示參數說明 |

## 第一次啟動

1. 開啟 `http://127.0.0.1:8787/`（啟動時會自動開）。
2. 第一次會要求**設定管理密碼**（至少 6 個字元），設定後直接登入。
3. 登入後就是 **號池** 頁面，上方會顯示 `Base URL` 與 `API 金鑰`。

   > API 金鑰只在網頁介面顯示一次（首次設定完成、或按「重新產生 API 金鑰」時）。
   > 之後要查看只能到右上角 **設定** 按「重新產生 API 金鑰」，舊金鑰會立即失效。
   > 還沒複製就走掉了也別緊張：重新產生一組即可。

4. 此時模型清單是空的，先到 **設定 → ＋ 從模型目錄新增** 加入至少一個模型，`/v1` 才能用。

右上角的 **中文 / English** 可以隨時切換介面語言，選擇會存在瀏覽器裡；console 的訊息一律是英文。

## 新增帳號

右上角「**＋ 新增帳號**」有兩種模式：

| 模式 | 需要填寫 | 說明 |
| --- | --- | --- |
| **貼上 JWT** | `JWT`（必填）、`密碼`（選填） | 直接使用現成的登入憑證 |
| **用 Email + 密碼** | `Email`、`密碼`（皆必填） | 中轉會先向 `/auth/login` 登入換一組 JWT，驗證通過才存檔 |

兩種模式都會存成 `auths/{email}.json`（檔名取自 JWT 裡的 email；同一個 email 有多筆帳號時會加上 `-2`、`-3`）。
「名稱」是選填的備註，只影響網頁上顯示。

**JWT 怎麼拿**：在瀏覽器登入 miniapps.ai → DevTools → **Application → Cookies → `https://api.miniapps.ai`** → 複製 `jwt` 的值。
`CSRF` 不用管，中轉會自己取得。

**密碼是做什麼的**：填了密碼的帳號可以自動續期。上游的 JWT 固定 15 天到期，而且沒有換發機制，
所以程式會在 JWT 剩不到 3 天時（或你按「續期」時）自動重新登入換一組新的。

> 密碼會以**明文**存在 `auths/{email}.json`（網頁與 API 只會顯示遮蔽後的值）。
> 不想存密碼就不要填，JWT 過期時再到網頁上貼一組新的即可。

每張帳號卡片可以：

| 按鈕 | 動作 |
| --- | --- |
| 測試 | 只呼叫 `quickAccess`（讀對話清單），**不消耗 AI 額度**，用來確認憑證是否有效 |
| 續期 | 用儲存的 email／密碼重新登入，換一組新的 JWT（僅在填了密碼時出現） |
| 編輯 | 修改名稱、換 JWT、換密碼（留空表示不變更） |
| 停用／啟用 | 停用的帳號不會被挑中 |
| 刪除 | 移除該帳號並刪掉 `auths/{email}.json` |

## 呼叫 OpenAI API

把任何 OpenAI 客戶端的 Base URL 指到 `http://127.0.0.1:8787/v1`，並帶上 API 金鑰。
`model` 請填你在 **設定** 裡加入的模型名稱（`GET /v1/models` 可以列出目前有哪些）。

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer sk-m2a-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-6-astra",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8787/v1", api_key="sk-m2a-xxxx")

stream = client.chat.completions.create(
    model="gpt-6-astra",
    messages=[{"role": "user", "content": "你好"}],
    stream=True,
)
for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")
```

其他相容寫法：

```
OPENAI_BASE_URL=http://127.0.0.1:8787/v1
OPENAI_API_KEY=sk-m2a-xxxx
```

> **工具調用（function calling）不支援。** 上游是聊天型 MiniApp，沒有工具協議；
> 請求中的 `tools`／`tool_choice` 會被忽略，回應一律是 `content` 加 `finish_reason: "stop"`。

### 模型對應

- 模型清單**預設是空的**，請先到 **設定 → ＋ 從模型目錄新增**。
- 目錄來自上游 `GET /ai-models`（數百筆），加入時會自動帶上該模型的 `model_id`，
  對外名稱預設取上游的 `nativeId`（可自行改名，撞名時會自動加序號）。
- 所有模型共用同一個寫死的 `toolId`（程式內的 `config.DefaultToolID`），不需要也不能設定。
- 傳入清單中不存在的模型名稱時，會自動用清單中的第一個模型，方便現成客戶端直接接入。

## 資料檔案

### `config.json`

放在資料目錄底下，程式啟動時會自動建立／補齊：

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
      "id": "gpt-6-astra",
      "name": "GPT 6 Astra",
      "model_id": "f57145fe-a761-4ac4-9cc5-676ac291c433",
      "revision": 1,
      "language": "zh"
    }
  ]
}
```

| 欄位 | 說明 |
| --- | --- |
| `password_salt` / `password_hash` | 管理密碼的 PBKDF2-HMAC-SHA256 雜湊（210,000 次迭代），沒有明文 |
| `api_key` | `/v1` 使用的 API 金鑰 |
| `require_api_key` | `true` 時呼叫 `/v1` 必須帶 `Authorization: Bearer <key>`（預設 `true`） |
| `host` / `port` | 監聽位址，預設 `127.0.0.1:8787`；`-addr` 會覆蓋這裡的值 |
| `request_timeout_seconds` | 等待 AI 回覆的上限，預設 180 秒 |
| `models` | 對外的模型清單，預設為空陣列 |

`models[].language` 是送給上游的語言代碼（`zh`、`en`…），`revision` 一般保持 `1`。

### `auths/{email}.json`

一個帳號一個檔案，權限 `0600`：

```json
{
  "id": "…",
  "name": "",
  "jwt": "eyJhbGciOi…",
  "password": "…",
  "csrf_cookie": "",
  "csrf_token": "",
  "enabled": true,
  "created_at": "…",
  "updated_at": "…",
  "last_used_at": "…",
  "email": "tester@example.com",
  "expires_at": "…",
  "stats": { "requests": 0, "success": 0, "failed": 0 }
}
```

- `password` 是選填的登入密碼（自動續期用），沒有填就不會出現這個欄位。
- `csrf_cookie`／`csrf_token` 平常是空的，代表由中轉自動取得；手動填寫則會覆蓋。
- `expires_at` 由 `jwt` 解析而來，網頁上的「JWT 剩 N 天」就是看這個值。
- 檔案可以自行備份或複製到別台機器（複製後記得確認 `id` 沒有重複）。

## 中轉流程

1. 收到 `POST /v1/chat/completions`，把 `messages` 組成單一提示文字。
2. 從號池挑選「最久沒用」的啟用帳號；若該帳號的 JWT 剩不到 3 天且有存密碼，先自動續期。
3. 需要 CSRF 時自動向 `/auth/csrf` 取得配對（同一個帳號共用一組，遇到 `419` 會重取一次並重試）。
4. `POST /chat` 取得 `conversationId`，接著輪詢上游等待 AI 輸出完成。
5. 取得完整回覆後轉成 OpenAI 格式回傳（`stream` 會逐段送出 SSE）。
6. 帳號出錯時記入統計：憑證或網路類錯誤讓該帳號冷卻 60 秒，本次請求自動換下一個帳號重試（最多 3 個）；
   額度不足（402／412）只記錄不冷卻，因為那通常代表「這個帳號買不起這個模型」，
   冷卻會連帶讓同帳號的其他便宜模型也不能用。

## 常見問題

| 情況 | 回應 / 處理 |
| --- | --- |
| 上游額度不足 | `402 insufficient_quota`／`insufficient_credits`，換帳號或幫該帳號加值 |
| 上游限流 | `429 rate_limit_exceeded`，稍後再試 |
| 等太久 | `504 upstream_timeout`，可調高 `request_timeout_seconds` |
| 號池沒有可用帳號 | `503 no_available_account`（全部停用或正在冷卻） |
| API 金鑰錯誤 | `401 invalid_api_key`／`missing_api_key` |
| 還沒加入模型 | `503 no_model_configured`，到設定加入模型 |
| `419 invalid csrf token` | 中轉會自動重取 CSRF 並重試，正常情況下你不會看到 |
| JWT 過期 | 帳號有存密碼會自動續期；沒存就到網頁貼一組新的 |
| 連接埠被占用 | 啟動時會提示，用 `-addr 127.0.0.1:其他埠` 換一個 |
| 瀏覽器沒自動開啟 | 手動連 console 印出的網址即可（Linux 沒有桌面環境時很正常） |

## 開發與發行

```bash
gofmt -l .            # 格式檢查
go vet ./...
go test ./... -count=1
node --check internal/server/web/app.js   # 前端語法檢查
```

專案結構：

```
main.go                      啟動、命令列參數、開啟瀏覽器
internal/config/             config.json（密碼雜湊、API 金鑰、模型設定）
internal/store/              auths/{email}.json 號池
internal/miniapps/           api.miniapps.ai 用戶端（CSRF、登入、送出訊息、解析回應）
internal/openai/             OpenAI 相容格式與訊息轉換
internal/server/             HTTP 路由、網頁介面、/v1 端點
internal/server/web/         網頁介面（index.html / app.js / i18n.js，用 go:embed 包進執行檔）
.github/workflows/ci.yml     推送與 PR 時跑格式檢查、vet、測試與雙平台建置
.github/workflows/release.yml 打 tag 時建置 Windows／Linux 執行檔並發 Release
```

### 發行新版本

```bash
git tag v1.0.1
git push origin v1.0.1
```

`release` workflow 會依 `go.mod` 的 Go 版本建置
`windows/amd64`、`windows/arm64`、`linux/amd64`、`linux/arm64` 四種執行檔，
打包成 zip／tar.gz 後發佈到 Releases，並把版本號用 `-ldflags` 寫進執行檔（啟動橫幅會顯示）。

只想拿檔案、不想發 Release 時，到 **Actions → release → Run workflow** 手動觸發，
版本號會是 `0.0.0-<commit>`，檔案放在該次執行的 Artifacts。

## 認可與致謝

本專案認可並感謝 [LINUX DO](https://linux.do/) 社群，專案的推廣與討論也在該社群進行。

## 授權

[AGPL-3.0](LICENSE)
