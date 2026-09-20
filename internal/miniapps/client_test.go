package miniapps

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 以下 payload 取自真實的 api.miniapps.ai /ai-models 回應（只保留用到的欄位）。
const aiModelsPayload = `{
  "page": 1,
  "take": 10000,
  "total": null,
  "hasMore": false,
  "items": [
    {"id": "uuid-minimax", "title": "MiniMax M2", "nativeId": "minimax-m2", "platformId": "openrouter",
     "type": "text", "creditPrice": 1, "enabled": true, "isVisible": true, "isDown": false,
     "hasTools": true, "hasVision": false, "isReasoning": false, "variantLabel": null},
    {"id": "uuid-astra", "title": "GPT 6 Astra", "nativeId": "gpt-6-astra", "platformId": "openai",
     "type": "text", "creditPrice": 58, "enabled": true, "isVisible": true, "isDown": false,
     "hasTools": true, "hasVision": true, "isReasoning": true, "variantLabel": null},
    {"id": "uuid-down", "title": "Down Model", "nativeId": "down-model", "platformId": "openai",
     "type": "text", "creditPrice": 5, "enabled": false, "isVisible": false, "isDown": true}
  ]
}`

func TestAIModels(t *testing.T) {
	var gotOptions, gotToolID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ai-models" {
			t.Errorf("請求路徑錯誤：%s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("請求方法錯誤：%s", r.Method)
		}
		gotOptions = r.URL.Query().Get("options")
		gotToolID = r.URL.Query().Get("toolId")
		if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, "jwt=test-jwt") {
			t.Errorf("應該帶上 jwt Cookie，得到 %q", cookie)
		} else if strings.Contains(cookie, csrfCookieName) {
			t.Errorf("唯讀的 GET 不應該帶上 CSRF Cookie，得到 %q", cookie)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(aiModelsPayload))
	}))
	defer server.Close()

	// 模型目錄只需要帳號的 JWT，不需要 modelId 或 CSRF。
	client := New(Credentials{JWT: "test-jwt"})
	client.baseURL = server.URL

	models, err := client.AIModels(context.Background(), "tool-1")
	if err != nil {
		t.Fatalf("AIModels 失敗：%v", err)
	}
	if gotToolID != "tool-1" {
		t.Fatalf("toolId 應該放在查詢字串，得到 %q", gotToolID)
	}
	if !strings.Contains(gotOptions, "10000") {
		t.Fatalf("options 應該帶上 itemsPerPage，得到 %q", gotOptions)
	}
	if len(models) != 3 {
		t.Fatalf("應該回傳 3 個模型，得到 %d", len(models))
	}

	// 結果應該依名稱排序（忽略大小寫）。
	order := []string{models[0].Title, models[1].Title, models[2].Title}
	want := []string{"Down Model", "GPT 6 Astra", "MiniMax M2"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("排序結果錯誤：%v", order)
		}
	}

	astra := models[1]
	if astra.ID != "uuid-astra" || astra.NativeID != "gpt-6-astra" || astra.CreditPrice != 58 || !astra.HasTools {
		t.Fatalf("模型欄位解析錯誤：%+v", astra)
	}
	if !astra.Enabled || !astra.IsVisible || astra.IsDown {
		t.Fatalf("可用狀態解析錯誤：%+v", astra)
	}
}

func TestAIModelsUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer server.Close()

	client := New(Credentials{JWT: "expired"})
	client.baseURL = server.URL

	_, err := client.AIModels(context.Background(), "tool-1")
	if !IsAuthError(err) {
		t.Fatalf("401 應該被視為帳號憑證錯誤，得到 %v", err)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("錯誤應該帶上 HTTP 狀態：%v", err)
	}
}

func TestSendFetchesCSRFAutomatically(t *testing.T) {
	var csrfHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/csrf":
			csrfHits++
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: "fresh-cookie", Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"csrfToken":"fresh-token"}`))
		case "/chat":
			if got := r.Header.Get("x-csrf-token"); got != "fresh-token" {
				t.Errorf("x-csrf-token 應該使用自動取得的配對，得到 %q", got)
			}
			if cookie := r.Header.Get("Cookie"); !strings.Contains(cookie, csrfCookieName+"=fresh-cookie") {
				t.Errorf("Cookie 應該帶上自動取得的 CSRF 值，得到 %q", cookie)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"conversationId":"conv-1"}`))
		default:
			t.Errorf("非預期的請求路徑：%s", r.URL.Path)
		}
	}))
	defer server.Close()

	// 帳號只填 JWT，CSRF 應該由用戶端自己取得。
	client := New(Credentials{JWT: "test-jwt", ToolID: "tool-1", ModelID: "model-1"})
	client.baseURL = server.URL

	conversationID, err := client.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send 失敗：%v", err)
	}
	if conversationID != "conv-1" {
		t.Fatalf("conversationId 錯誤：%q", conversationID)
	}
	if csrfHits != 1 {
		t.Fatalf("應該只取得一次 CSRF，得到 %d 次", csrfHits)
	}
}

func TestSendRefreshesExpiredCSRF(t *testing.T) {
	var csrfHits, chatHits int
	var chatTokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/csrf":
			csrfHits++
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: fmt.Sprintf("cookie-%d", csrfHits), Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"csrfToken":"token-%d"}`, csrfHits)))
		case "/chat":
			chatHits++
			chatTokens = append(chatTokens, r.Header.Get("x-csrf-token"))
			if chatHits == 1 {
				w.WriteHeader(statusCSRFExpired)
				_, _ = w.Write([]byte(`{"statusCode":419,"message":"invalid csrf token"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"conversationId":"conv-%d"}`, chatHits)))
		default:
			t.Errorf("非預期的請求路徑：%s", r.URL.Path)
		}
	}))
	defer server.Close()

	// 帳號帶著一組過期的 CSRF，第一次送出會拿到 419，用戶端應該自動重取。
	client := New(Credentials{JWT: "test-jwt", CSRFCookie: "stale", CSRFToken: "stale", ToolID: "tool-1", ModelID: "model-1"})
	client.baseURL = server.URL

	conversationID, err := client.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send 失敗：%v", err)
	}
	if conversationID != "conv-2" {
		t.Fatalf("conversationId 錯誤：%q", conversationID)
	}
	if csrfHits != 1 || chatHits != 2 {
		t.Fatalf("應該重取一次 CSRF 並重試一次，得到 csrf=%d chat=%d", csrfHits, chatHits)
	}

	// 重取後的配對會進到共用快取，下一個請求不該再抓一次。
	next := New(Credentials{JWT: "test-jwt", ToolID: "tool-1", ModelID: "model-1", CSRFCache: client.csrf})
	next.baseURL = server.URL
	if _, err := next.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("Send 失敗：%v", err)
	}
	if csrfHits != 1 {
		t.Fatalf("共用快取應該沿用既有的 CSRF，得到 %d 次取得", csrfHits)
	}

	// 依序應該是：帳號填的過期值 → 重取後的 token-1 → 共用快取沿用的 token-1。
	want := []string{"stale", "token-1", "token-1"}
	if len(chatTokens) != len(want) {
		t.Fatalf("送出次數錯誤：%v", chatTokens)
	}
	for i := range want {
		if chatTokens[i] != want[i] {
			t.Fatalf("第 %d 次送出的 x-csrf-token 應為 %q，得到 %q", i+1, want[i], chatTokens[i])
		}
	}
}
