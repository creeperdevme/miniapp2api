package miniapps

import (
	"context"
	"errors"
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
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(aiModelsPayload))
	}))
	defer server.Close()

	// 模型目錄只需要帳號的 JWT 與 CSRF，不需要 modelId。
	client := New(Credentials{JWT: "test-jwt", CSRFCookie: "csrf-cookie", CSRFToken: "csrf-token"})
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
