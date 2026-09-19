package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	if cfg.HasPassword() {
		t.Fatal("全新設定不應該有密碼")
	}
	if err := cfg.SetPassword("123"); err == nil {
		t.Fatal("太短的密碼應該被拒絕")
	}
	if err := cfg.SetPassword("s3cret123"); err != nil {
		t.Fatalf("SetPassword 失敗：%v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("設定檔應該要被寫入：%v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("重新載入失敗：%v", err)
	}
	if !reloaded.HasPassword() {
		t.Fatal("重新載入後應該有密碼")
	}
	if err := reloaded.CheckPassword("s3cret123"); err != nil {
		t.Fatalf("密碼應該要正確：%v", err)
	}
	if err := reloaded.CheckPassword("wrong-password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("錯誤密碼應該回報 ErrWrongPassword，得到 %v", err)
	}
	// 檔案裡不應該出現明文密碼。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("讀取設定檔失敗：%v", err)
	}
	if string(data) == "" || containsText(string(data), "s3cret123") {
		t.Fatal("設定檔不應該包含明文密碼")
	}
}

func TestAPIKeyAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}

	if !cfg.RequireKey() {
		t.Fatal("預設應該要求 API 金鑰")
	}
	if cfg.ListenAddr() != "127.0.0.1:8787" {
		t.Fatalf("預設位址錯誤：%s", cfg.ListenAddr())
	}

	generated, err := cfg.EnsureAPIKey()
	if err != nil || !generated {
		t.Fatalf("應該要產生 API 金鑰：%v", err)
	}
	key := cfg.Key()
	if len(key) < 20 {
		t.Fatalf("API 金鑰太短：%q", key)
	}
	if !cfg.CheckKey(key) {
		t.Fatal("CheckKey 應該要通過")
	}
	if cfg.CheckKey("wrong") {
		t.Fatal("錯誤的金鑰不應該通過")
	}

	again, err := cfg.EnsureAPIKey()
	if err != nil || again {
		t.Fatal("已有金鑰時不應該重新產生")
	}

	if err := cfg.SetListenAddr("0.0.0.0:9000"); err != nil {
		t.Fatalf("SetListenAddr 失敗：%v", err)
	}
	if cfg.ListenAddr() != "0.0.0.0:9000" {
		t.Fatalf("位址沒有更新：%s", cfg.ListenAddr())
	}
	if cfg.BaseURL() != "http://127.0.0.1:9000/v1" {
		t.Fatalf("BaseURL 應該把 0.0.0.0 換成 127.0.0.1，得到 %s", cfg.BaseURL())
	}
	if err := cfg.SetListenAddr("bad-address"); err == nil {
		t.Fatal("錯誤的位址應該要失敗")
	}
}

func TestFindModel(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	models := cfg.ModelList()
	if len(models) != 4 {
		t.Fatalf("內建應該有 4 個模型，得到 %d", len(models))
	}
	for _, id := range []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		model := cfg.FindModel(id)
		if !strings.EqualFold(model.ID, id) {
			t.Fatalf("找不到模型 %s，得到 %+v", id, model)
		}
		if model.ToolID == "" || model.ModelID == "" {
			t.Fatalf("模型 %s 缺少 toolId 或 modelId：%+v", id, model)
		}
	}
	if model := cfg.FindModel("gpt-5.6-luna"); model.ModelID != "b95a7fe5-fd23-4b72-8c05-aa5ff51df1f1" ||
		model.ToolID != "65afe0d6-4215-4408-8a7d-8f32f9e592a7" {
		t.Fatalf("gpt-5.6-luna 的 ID 不正確：%+v", model)
	}
	// 未知的模型名稱要退回預設值，讓各種 OpenAI 客戶端都能直接使用。
	if model := cfg.FindModel("gpt-4o"); model.ID != "gpt-6-astra" {
		t.Fatalf("未知模型應該退回預設，得到 %+v", model)
	}
}

// 既有設定檔缺少內建模型時，應該自動補上且不覆蓋原有設定。
func TestDefaultsMergedIntoExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := `{"models":[{"id":"gpt-6-astra","name":"我的 Astra","tool_id":"custom-tool","model_id":"custom-model","revision":2,"language":"en"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("寫入設定檔失敗：%v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	if len(cfg.ModelList()) != 4 {
		t.Fatalf("應該補成 4 個模型，得到 %d", len(cfg.ModelList()))
	}
	kept := cfg.FindModel("gpt-6-astra")
	if kept.ToolID != "custom-tool" || kept.Name != "我的 Astra" || kept.Revision != 2 {
		t.Fatalf("原有設定被覆蓋了：%+v", kept)
	}
}

func containsText(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
