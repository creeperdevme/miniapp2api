package config

import (
	"errors"
	"os"
	"path/filepath"
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

// 預設不應該有任何模型，全部交由使用者從模型目錄加入。
func TestNoBuiltInModels(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	if models := cfg.ModelList(); len(models) != 0 {
		t.Fatalf("預設不應該有模型，得到 %+v", models)
	}
	// 沒有模型時 FindModel 也不能 panic。
	if model := cfg.FindModel("gpt-4o"); model.ID != "" || model.ModelID != "" {
		t.Fatalf("沒有模型時應該回傳空值，得到 %+v", model)
	}
}

func TestAddAndRemoveModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}

	if err := cfg.AddModel(Model{ID: "minimax-m2"}); err == nil {
		t.Fatal("缺少 modelId 應該被拒絕")
	}
	if err := cfg.AddModel(Model{ID: "MiniMax M2", ModelID: "model-1"}); err == nil {
		t.Fatal("含空白的模型名稱應該被拒絕")
	}
	if err := cfg.AddModel(Model{ID: "minimax-m2", ModelID: "model-1"}); err != nil {
		t.Fatalf("AddModel 失敗：%v", err)
	}
	if len(cfg.ModelList()) != 1 {
		t.Fatalf("應該有 1 個模型，得到 %d", len(cfg.ModelList()))
	}

	added := cfg.FindModel("MiniMax-M2")
	if added.ModelID != "model-1" || added.Name != "minimax-m2" || added.Revision != 1 || added.Language != "zh" {
		t.Fatalf("新增的模型沒有補上預設值：%+v", added)
	}
	if added.ToolID != DefaultToolID {
		t.Fatalf("toolId 應該固定為 %s，得到 %q", DefaultToolID, added.ToolID)
	}
	if err := cfg.AddModel(Model{ID: "MiniMax-M2", ModelID: "model-2"}); !errors.Is(err, ErrModelExists) {
		t.Fatalf("重複的名稱應該回報 ErrModelExists，得到 %v", err)
	}

	// toolId 不寫進 config.json。
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("讀取設定檔失敗：%v", err)
	}
	if containsText(string(data), "tool_id") {
		t.Fatalf("config.json 不應該有 tool_id：%s", data)
	}

	// 可以把模型全部移除，重新載入後仍然是空的。
	if err := cfg.RemoveModel("minimax-m2"); err != nil {
		t.Fatalf("RemoveModel 失敗：%v", err)
	}
	if err := cfg.RemoveModel("no-such-model"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("移除不存在的模型應該回報 ErrModelNotFound，得到 %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("重新載入失敗：%v", err)
	}
	if models := reloaded.ModelList(); len(models) != 0 {
		t.Fatalf("移除後不應該有模型，得到 %+v", models)
	}
}

// 舊設定檔裡的 tool_id 會被忽略，一律改用寫死的 DefaultToolID。
func TestLegacyToolIDIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	original := `{"models":[{"id":"minimax-m2","name":"MiniMax M2","tool_id":"custom-tool","model_id":"model-1","revision":2,"language":"en"}]}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("寫入設定檔失敗：%v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	model := cfg.FindModel("minimax-m2")
	if model.ToolID != DefaultToolID {
		t.Fatalf("toolId 應該固定為 %s，得到 %q", DefaultToolID, model.ToolID)
	}
	if model.ModelID != "model-1" || model.Name != "MiniMax M2" || model.Revision != 2 || model.Language != "en" {
		t.Fatalf("原有設定被覆蓋了：%+v", model)
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
