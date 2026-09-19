package store

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testJWT(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"user-1","email":"tester@example.com","exp":1893456000}`))
	return header + "." + payload + ".signature"
}

func TestCreateAndPick(t *testing.T) {
	root := t.TempDir()
	pool, err := Open(root)
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}

	account, err := pool.Create(Account{
		JWT:        testJWT(t),
		CSRFCookie: "cookie-value",
		CSRFToken:  "token-value",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}
	if account.Email != "tester@example.com" {
		t.Fatalf("應該從 JWT 解出信箱，得到 %q", account.Email)
	}

	path := filepath.Join(root, DirName, account.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("帳號檔案不存在 %s：%v", path, err)
	}
	if len(account.ID) != 36 {
		t.Fatalf("帳號 ID 應該是 UUID，得到 %q", account.ID)
	}

	views := pool.List()
	if len(views) != 1 {
		t.Fatalf("List 應該回傳 1 筆，得到 %d", len(views))
	}

	picked, err := pool.Pick(nil)
	if err != nil {
		t.Fatalf("Pick 失敗：%v", err)
	}
	if picked.ID != account.ID {
		t.Fatalf("Pick 挑到錯誤的帳號：%s", picked.ID)
	}

	// 發生錯誤後帳號會進入冷卻，不能再被挑中。
	pool.Record(account.ID, errors.New("boom"), CooldownDuration)
	if _, err := pool.Pick(nil); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("冷卻中的帳號不應該被挑中，得到 %v", err)
	}

	// 成功後冷卻解除。
	pool.Record(account.ID, nil, CooldownDuration)
	if _, err := pool.Pick(nil); err != nil {
		t.Fatalf("冷卻應該要解除：%v", err)
	}

	stored, ok := pool.Get(account.ID)
	if !ok {
		t.Fatal("找不到剛建立的帳號")
	}
	if stored.Stats.Requests != 2 || stored.Stats.Success != 1 || stored.Stats.Failed != 1 {
		t.Fatalf("統計數字錯誤：%+v", stored.Stats)
	}
}

// 額度不足之類的錯誤不應該讓帳號冷卻，否則會連帶影響其他模型。
func TestRecordWithoutCooldownKeepsAccountAvailable(t *testing.T) {
	pool, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}
	account, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}

	pool.Record(account.ID, errors.New("額度不足"), 0)

	if _, err := pool.Pick(nil); err != nil {
		t.Fatalf("沒有冷卻時應該還能被挑中：%v", err)
	}
	stored, _ := pool.Get(account.ID)
	if stored.Stats.Failed != 1 {
		t.Fatalf("失敗次數應該要累計：%+v", stored.Stats)
	}
	if stored.LastError != "額度不足" {
		t.Fatalf("應該要記下錯誤訊息，得到 %q", stored.LastError)
	}
}

func TestModifyDisableAndDelete(t *testing.T) {
	root := t.TempDir()
	pool, err := Open(root)
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}

	account, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}

	if _, err := pool.Modify(account.ID, func(acc *Account) error {
		acc.Enabled = false
		acc.Name = "主要帳號"
		return nil
	}); err != nil {
		t.Fatalf("Modify 失敗：%v", err)
	}
	if _, err := pool.Pick(nil); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("停用的帳號不應該被挑中，得到 %v", err)
	}

	if _, err := pool.Modify("不存在", func(*Account) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("應該回報找不到帳號，得到 %v", err)
	}

	if err := pool.Delete(account.ID); err != nil {
		t.Fatalf("Delete 失敗：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, DirName, account.ID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("檔案應該被刪除：%v", err)
	}
	if pool.Count() != 0 {
		t.Fatalf("號池應該是空的")
	}
}

func TestCreateValidatesFields(t *testing.T) {
	pool, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}
	if _, err := pool.Create(Account{CSRFCookie: "c", CSRFToken: "t"}); err == nil {
		t.Fatal("缺少 JWT 應該要失敗")
	}
	if _, err := pool.Create(Account{JWT: "jwt", CSRFToken: "t"}); err == nil {
		t.Fatal("缺少 CSRF_Cookie 應該要失敗")
	}
	if _, err := pool.Create(Account{JWT: "jwt", CSRFCookie: "c"}); err == nil {
		t.Fatal("缺少 CSRF_Token 應該要失敗")
	}
}

func TestRedactedHidesSecrets(t *testing.T) {
	account := Account{JWT: "abcdefghijklmnopqrstuvwxyz", CSRFCookie: "0123456789abcdef", CSRFToken: "short"}
	view := account.View()
	if view.JWT == account.JWT || view.CSRFCookie == account.CSRFCookie {
		t.Fatal("敏感欄位應該被遮蔽")
	}
	if !strings.HasPrefix(view.JWT, "abcdef") || !strings.HasSuffix(view.JWT, "wxyz") {
		t.Fatalf("遮蔽後應保留頭尾片段：%q", view.JWT)
	}
	if strings.Contains(view.JWT, "ghijklmnop") {
		t.Fatalf("遮蔽後不應包含中間內容：%q", view.JWT)
	}
}

func TestParseJWTRejectsGarbage(t *testing.T) {
	if _, ok := ParseJWT("not-a-jwt"); ok {
		t.Fatal("不應該解析成功")
	}
}

// 舊版帳號檔留下的欄位要能被忽略，讀取時不會出錯，寫回時會被清掉。
func TestLoadLegacyAccountFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建立目錄失敗：%v", err)
	}

	const id = "11111111-2222-3333-4444-555555555555"
	legacy := `{
  "id": "` + id + `",
  "name": "舊帳號",
  "jwt": "` + testJWT(t) + `",
  "csrf_cookie": "cookie",
  "csrf_token": "token",
  "tool_id": "old-tool",
  "model_id": "old-model",
  "revision": 3,
  "language": "en",
  "note": "舊備註",
  "enabled": true
}`
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("寫入舊格式檔案失敗：%v", err)
	}

	pool, err := Open(root)
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}
	account, ok := pool.Get(id)
	if !ok {
		t.Fatal("應該要讀到舊帳號")
	}
	if account.Name != "舊帳號" || account.JWT == "" || !account.Enabled {
		t.Fatalf("舊帳號內容不正確：%+v", account)
	}

	// 觸發一次寫回，舊欄位應該要從檔案中消失。
	if _, err := pool.Modify(id, func(acc *Account) error {
		acc.Name = "改名後"
		return nil
	}); err != nil {
		t.Fatalf("Modify 失敗：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		t.Fatalf("讀取檔案失敗：%v", err)
	}
	for _, key := range []string{"tool_id", "model_id", "revision", "language", "note"} {
		if strings.Contains(string(data), key) {
			t.Fatalf("舊欄位 %s 應該要消失，檔案內容：%s", key, data)
		}
	}
}
