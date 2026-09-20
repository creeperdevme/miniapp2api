# miniapp2api

[繁體中文](README.md) | **English**

Wraps a pool of [miniapps.ai](https://miniapps.ai) accounts into an **OpenAI-compatible `/v1` API**, with a web UI for managing the pool.
Pure Go, no third-party dependencies, and a single self-contained binary (Windows / Linux).

> This is a personal relay tool. Make sure your usage complies with the upstream service's terms; you are responsible for your own accounts and credits.

## Table of contents

- [Features](#features)
- [Installation](#installation)
  - [Prebuilt binaries](#prebuilt-binaries)
  - [Windows](#windows)
  - [Linux](#linux)
  - [Build from source](#build-from-source)
- [First run](#first-run)
- [Adding accounts](#adding-accounts)
- [Calling the OpenAI API](#calling-the-openai-api)
- [Data files](#data-files)
- [How a request is relayed](#how-a-request-is-relayed)
- [Troubleshooting](#troubleshooting)
- [Development and releases](#development-and-releases)

## Features

- OpenAI-compatible endpoints: `GET /v1/models`, `POST /v1/chat/completions` (supports `stream`).
- Web UI: pool listing and stats, model management, API key management, password change.
- Two ways to add an account: paste a **JWT**, or provide an **email + password** and let the relay sign in for you.
- The Web UI switches between **繁體中文 / English** (top right), while console output is always English.
- **CSRF is fully automatic**: requests that need it fetch a fresh pair from `/auth/csrf`, so you never copy cookies or headers by hand.
- **Automatic JWT renewal**: accounts that store a password are silently re-logged-in when the JWT is within 3 days of expiring, and you can also renew on demand.
- Picks the least recently used enabled account; failing accounts cool down and the request automatically retries with the next one.
- The admin password is stored as a PBKDF2-HMAC-SHA256 hash (never plaintext); the API key is shown only once in the Web UI and is never printed to the console.

## Installation

### Prebuilt binaries

Grab the archive for your platform from [Releases](https://github.com/creeperdevme/miniapp2api/releases):

| Platform | File |
| --- | --- |
| Windows x64 | `miniapp2api_<version>_windows_amd64.zip` |
| Windows ARM64 | `miniapp2api_<version>_windows_arm64.zip` |
| Linux x64 | `miniapp2api_<version>_linux_amd64.tar.gz` |
| Linux ARM64 | `miniapp2api_<version>_linux_arm64.tar.gz` |

You can also trigger a build without tagging by going to **Actions → release → Run workflow** and downloading the archives from that run's **Artifacts**.

### Windows

1. Extract the zip to a permanent folder, for example `D:\miniapp2api`.

   > The default data directory is **the folder containing the executable**, so `config.json` and `auths/` are created there. Keep it somewhere stable.

2. Double-click `miniapp2api.exe`, or run it from PowerShell:

   ```powershell
   cd D:\miniapp2api
   .\miniapp2api.exe
   ```

3. It opens your browser (pass `-no-browser` to skip that). The console prints the data directory, URLs and pool size, but **never the API key**.
4. If Windows SmartScreen appears, choose "More info" -> "Run anyway".

Common flags:

```powershell
.\miniapp2api.exe -h                                     # show all flags
.\miniapp2api.exe -data D:\miniapp2api-data -no-browser  # custom data dir, no browser
.\miniapp2api.exe -addr 127.0.0.1:9000                   # change the port (default 8787)
```

**Exposing it to your LAN** (phone, another PC, ...):

```powershell
# 1) listen on every interface
.\miniapp2api.exe -addr 0.0.0.0:8787

# 2) open the firewall (elevated PowerShell)
New-NetFirewallRule -DisplayName "miniapp2api" -Direction Inbound -Protocol TCP -LocalPort 8787 -Action Allow

# 3) find your LAN IP
ipconfig
```

Other devices can then open `http://<your-lan-ip>:8787/`, and the `/v1` base URL becomes the same host.

> Exposing the port puts your API on the network, so keep the API key secret (`require_api_key` defaults to `true`, meaning `/v1` always requires a key).

**Start automatically at logon** (Task Scheduler):

```powershell
schtasks /create /tn miniapp2api /tr "D:\miniapp2api\miniapp2api.exe -data D:\miniapp2api -no-browser" /sc onlogon
schtasks /run /tn miniapp2api         # start now
schtasks /delete /tn miniapp2api /f   # remove
```

### Linux

```bash
# 1) download and extract (use the version you actually downloaded)
tar -xzf miniapp2api_<version>_linux_amd64.tar.gz
chmod +x miniapp2api

# 2) install into PATH (optional)
sudo install -m 755 miniapp2api /usr/local/bin/miniapp2api

# 3) create the data directory and start
sudo mkdir -p /opt/miniapp2api
miniapp2api -data /opt/miniapp2api -no-browser
```

> Headless servers cannot open a browser; that failure is harmless. Pass `-no-browser`.
> If you only use it locally, keep the default `127.0.0.1:8787`; switch to `0.0.0.0:8787` only when you need remote access.

**Run it as a systemd service** - `/etc/systemd/system/miniapp2api.service`:

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
# dedicated system user without a login shell
sudo useradd --system --home /opt/miniapp2api --shell /usr/sbin/nologin miniapp2api
sudo chown -R miniapp2api:miniapp2api /opt/miniapp2api

sudo systemctl daemon-reload
sudo systemctl enable --now miniapp2api
sudo systemctl status miniapp2api
journalctl -u miniapp2api -f
```

> Permission note: files under `auths/` contain JWTs and (if you provided one) a plaintext password.
> The program creates them with mode `0600`, but when running under systemd make sure the whole data
> directory is owned by the service user (the `chown` above).

**Open the firewall**:

```bash
sudo ufw allow 8787/tcp                                                          # Debian / Ubuntu
sudo firewall-cmd --add-port=8787/tcp --permanent && sudo firewall-cmd --reload   # RHEL / Fedora
```

**Quick background run without systemd**:

```bash
nohup miniapp2api -data /opt/miniapp2api -no-browser > /var/log/miniapp2api.log 2>&1 &
```

### Build from source

Requires Go 1.24 or newer:

```bash
git clone https://github.com/creeperdevme/miniapp2api.git
cd miniapp2api
go build -o miniapp2api .          # use -o miniapp2api.exe on Windows
./miniapp2api
```

Cross-compiling needs no extra toolchain:

```bash
GOOS=windows GOARCH=amd64 go build -o miniapp2api.exe .
GOOS=linux   GOARCH=arm64 go build -o miniapp2api-linux-arm64 .
```

### Command-line flags

| Flag | Description |
| --- | --- |
| `-addr 127.0.0.1:8787` | Listen address; when omitted it is read from `config.json` (default `127.0.0.1:8787`) |
| `-data <dir>` | Data directory holding `config.json` and `auths/`; defaults to the executable's folder |
| `-no-browser` | Do not open a browser after startup |
| `-h` | Show usage |

## First run

1. Open `http://127.0.0.1:8787/` (it opens automatically).
2. It asks you to **set an admin password** (at least 6 characters) and logs you in.
3. You land on the **account pool** page, which shows the `Base URL` and the `API key`.

   > The API key is displayed only once, in the Web UI (right after setup, or when you click "Regenerate API key").
   > To see it again you must click "Regenerate API key" in **Settings**, which invalidates the old one.
   > If you missed it, simply regenerate.

4. The model list starts empty. Add at least one model via **Settings -> Add from model catalog** before calling `/v1`.

Use the **中文 / English** switch in the top right to change the UI language at any time; the choice is remembered in your browser. Console output stays English.

## Adding accounts

The "**+ Add account**" dialog offers two modes:

| Mode | Required fields | Notes |
| --- | --- | --- |
| **Paste JWT** | `JWT` (required), `Password` (optional) | Reuse an existing credential |
| **Email + password** | `Email`, `Password` (both required) | The relay signs in via `/auth/login` and stores the resulting JWT |

Both modes store the account as `auths/{email}.json` (the file name comes from the JWT's email; duplicates get `-2`, `-3`, ...).
"Name" is an optional label that only affects the UI.

**Where to get the JWT**: sign in at miniapps.ai, open DevTools, go to **Application -> Cookies -> `https://api.miniapps.ai`** and copy the `jwt` value.
You no longer need the CSRF cookie or header; the relay fetches those itself.

**What the password is for**: accounts with a stored password renew themselves. Upstream JWTs are valid for a
fixed 15 days with no refresh mechanism, so the relay signs in again when the token is within 3 days of expiring,
or whenever you press "Renew".

> The password is stored as **plaintext** in `auths/{email}.json` (the UI and API only ever show a masked value).
> Leave it blank if you would rather paste a fresh JWT every two weeks.

Each account card offers:

| Button | Action |
| --- | --- |
| Test | Calls `quickAccess` only (reads the conversation list). **Consumes no AI credits** and verifies the credential |
| Renew | Signs in again with the stored email/password to get a new JWT (only shown when a password is stored) |
| Edit | Change the name, replace the JWT, replace the password (leaving a field empty keeps the current value) |
| Disable / Enable | Disabled accounts are never picked |
| Delete | Removes the account and deletes `auths/{email}.json` |

## Calling the OpenAI API

Point any OpenAI client at `http://127.0.0.1:8787/v1` and send your API key.
`model` must be a model you added in **Settings** (`GET /v1/models` lists what is available).

```bash
curl http://127.0.0.1:8787/v1/chat/completions \
  -H "Authorization: Bearer sk-m2a-xxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-6-astra",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

```python
from openai import OpenAI

client = OpenAI(base_url="http://127.0.0.1:8787/v1", api_key="sk-m2a-xxxx")

stream = client.chat.completions.create(
    model="gpt-6-astra",
    messages=[{"role": "user", "content": "Hello"}],
    stream=True,
)
for chunk in stream:
    print(chunk.choices[0].delta.content or "", end="")
```

Or via environment variables:

```
OPENAI_BASE_URL=http://127.0.0.1:8787/v1
OPENAI_API_KEY=sk-m2a-xxxx
```

> **Tool / function calling is not supported.** The upstream is a chat-style MiniApp with no tool protocol;
> `tools` and `tool_choice` are ignored, and responses always contain `content` with `finish_reason: "stop"`.

### Model mapping

- The model list **starts empty**; add models in **Settings -> Add from model catalog**.
- The catalog comes from the upstream `GET /ai-models` endpoint (several hundred entries). Adding a model stores its
  `model_id`; the public name defaults to the upstream `nativeId` and is automatically suffixed on collisions.
- Every model shares one hard-coded `toolId` (`config.DefaultToolID`); it is neither configurable nor stored in `config.json`.
- Requesting an unknown model name falls back to the first model in the list, so existing clients work out of the box.

## Data files

### `config.json`

Created and migrated automatically inside the data directory:

```json
{
  "password_salt": "...",
  "password_hash": "...",
  "api_key": "sk-m2a-...",
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

| Field | Description |
| --- | --- |
| `password_salt` / `password_hash` | PBKDF2-HMAC-SHA256 hash of the admin password (210,000 iterations); no plaintext |
| `api_key` | The key clients send to `/v1` |
| `require_api_key` | When `true`, `/v1` requires `Authorization: Bearer <key>` (default `true`) |
| `host` / `port` | Listen address, default `127.0.0.1:8787`; `-addr` overrides it |
| `request_timeout_seconds` | How long to wait for an AI reply, default 180 |
| `models` | The public model list, empty by default |

`models[].language` is the language code sent upstream (`zh`, `en`, ...); `revision` normally stays `1`.

### `auths/{email}.json`

One file per account, created with mode `0600`:

```json
{
  "id": "...",
  "name": "",
  "jwt": "eyJhbGciOi...",
  "password": "...",
  "csrf_cookie": "",
  "csrf_token": "",
  "enabled": true,
  "created_at": "...",
  "updated_at": "...",
  "last_used_at": "...",
  "email": "tester@example.com",
  "expires_at": "...",
  "stats": { "requests": 0, "success": 0, "failed": 0 }
}
```

- `password` is optional and only used for automatic renewal; the field is omitted when unset.
- `csrf_cookie` / `csrf_token` are normally empty, meaning "fetch automatically"; setting them overrides that.
- `expires_at` is parsed from `jwt` and drives the "JWT expires in N days" badge.
- Files can be backed up or copied to another machine (just make sure `id` values stay unique).

## How a request is relayed

1. `POST /v1/chat/completions` arrives; `messages` are flattened into a single prompt.
2. The least recently used enabled account is picked; if its JWT expires within 3 days and a password is stored, it is renewed first.
3. When CSRF is needed, a pair is fetched from `/auth/csrf` (cached per account; a `419` triggers one refetch and retry).
4. `POST /chat` returns a `conversationId`, then the relay polls the upstream until the answer is complete.
5. The full answer is converted into an OpenAI response (`stream` sends incremental SSE chunks).
6. Failures are recorded per account: credential and network errors cool the account down for 60 seconds and the request
   retries with the next account (up to 3 attempts). Quota errors (402/412) are recorded but do **not** cool the account
   down, because they usually mean "this account cannot afford this model" and cooling would also disable its cheaper models.

## Troubleshooting

| Symptom | Meaning / fix |
| --- | --- |
| Upstream out of credits | `402 insufficient_quota` / `insufficient_credits`; use another account or top it up |
| Upstream rate limit | `429 rate_limit_exceeded`; retry later |
| Took too long | `504 upstream_timeout`; raise `request_timeout_seconds` |
| No usable account | `503 no_available_account` (everything disabled or cooling down) |
| Bad API key | `401 invalid_api_key` / `missing_api_key` |
| No model configured | `503 no_model_configured`; add a model in Settings |
| `419 invalid csrf token` | The relay refetches CSRF and retries, so you should not normally see this |
| JWT expired | With a stored password it renews itself; otherwise paste a fresh JWT |
| Port already in use | The startup message tells you; pick another with `-addr 127.0.0.1:<port>` |
| Browser did not open | Open the URL printed in the console (normal on headless Linux) |

## Development and releases

```bash
gofmt -l .            # formatting check
go vet ./...
go test ./... -count=1
node --check internal/server/web/app.js   # frontend syntax check
```

Project layout:

```
main.go                        startup, flags, opening the browser
internal/config/               config.json (password hash, API key, models)
internal/store/                the auths/{email}.json pool
internal/miniapps/             api.miniapps.ai client (CSRF, login, sending, parsing)
internal/openai/               OpenAI-compatible types and message conversion
internal/server/               HTTP routes, web UI, /v1 endpoints
internal/server/web/           web UI (index.html / app.js / i18n.js, embedded via go:embed)
.github/workflows/ci.yml       gofmt, vet, tests and dual-platform builds on push/PR
.github/workflows/release.yml  builds Windows/Linux binaries and publishes a Release on tags
```

### Cutting a release

```bash
git tag v1.0.1
git push origin v1.0.1
```

The `release` workflow builds `windows/amd64`, `windows/arm64`, `linux/amd64` and `linux/arm64` using the Go version
from `go.mod`, packages them as zip/tar.gz, publishes them to Releases, and injects the version via `-ldflags`
(it shows up in the startup banner).

To get the archives without publishing a Release, run **Actions -> release -> Run workflow** manually; the version
becomes `0.0.0-<commit>` and the files land in that run's Artifacts.

## License

[AGPL-3.0](LICENSE)
