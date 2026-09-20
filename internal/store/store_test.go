package store

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testJWT(t *testing.T) string {
	t.Helper()
	return testJWTWithEmail(t, "tester@example.com")
}

func testJWTWithEmail(t *testing.T, email string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"id":"user-1","email":"` + email + `","exp":1893456000}`))
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

	// 檔名以 JWT 裡的 email 為主。
	if account.FileName() != "tester@example.com" {
		t.Fatalf("檔名應該用 email，得到 %q", account.FileName())
	}
	path := filepath.Join(root, DirName, "tester@example.com.json")
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

	picked, err := pool.Pick(nil, "test-model")
	if err != nil {
		t.Fatalf("Pick 失敗：%v", err)
	}
	if picked.ID != account.ID {
		t.Fatalf("Pick 挑到錯誤的帳號：%s", picked.ID)
	}

	// 發生錯誤後帳號會進入冷卻，不能再被挑中。
	pool.Record(account.ID, errors.New("boom"), CooldownDuration)
	if _, err := pool.Pick(nil, "test-model"); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("冷卻中的帳號不應該被挑中，得到 %v", err)
	}

	// 成功後冷卻解除。
	pool.Record(account.ID, nil, CooldownDuration)
	if _, err := pool.Pick(nil, "test-model"); err != nil {
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

	if _, err := pool.Pick(nil, "test-model"); err != nil {
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
	if _, err := pool.Pick(nil, "test-model"); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("停用的帳號不應該被挑中，得到 %v", err)
	}

	if _, err := pool.Modify("不存在", func(*Account) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("應該回報找不到帳號，得到 %v", err)
	}

	if err := pool.Delete(account.ID); err != nil {
		t.Fatalf("Delete 失敗：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, DirName, "tester@example.com.json")); !errors.Is(err, os.ErrNotExist) {
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
	// CSRF 是選填的，留空時中轉會自己向 /auth/csrf 取得。
	if _, err := pool.Create(Account{JWT: "jwt"}); err != nil {
		t.Fatalf("只填 JWT 應該要成功，得到 %v", err)
	}
}

func TestAccountRenewalHelpers(t *testing.T) {
	soon := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	far := time.Now().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	window := 3 * 24 * time.Hour

	if !(Account{ExpiresAt: soon}).ExpiringWithin(window) {
		t.Fatal("剩一天應該算即將到期")
	}
	if (Account{ExpiresAt: far}).ExpiringWithin(window) {
		t.Fatal("剩 30 天不該算即將到期")
	}
	if (Account{}).ExpiringWithin(window) {
		t.Fatal("沒有到期時間時不該算即將到期")
	}
	if (Account{ExpiresAt: soon}).Expired() {
		t.Fatal("還沒到期不該算過期")
	}

	if (Account{Email: "a@b.c"}).AutoRenewable() {
		t.Fatal("沒有密碼時不該能自動續期")
	}
	if (Account{Password: "pw"}).AutoRenewable() {
		t.Fatal("沒有信箱時不該能自動續期")
	}
	if !(Account{Email: "a@b.c", Password: "pw"}).AutoRenewable() {
		t.Fatal("有信箱與密碼時應該能自動續期")
	}
}

func TestRedactedHidesPassword(t *testing.T) {
	account := Account{JWT: "abcdefghijklmnop", Password: "super-secret-password"}
	if view := account.View(); view.Password == account.Password || view.Password == "" {
		t.Fatalf("密碼應該被遮蔽，得到 %q", view.Password)
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

// 帳號檔案以 email 命名；同名時加序號，換了 email 會跟著更名。
func TestAccountFileNamedByEmail(t *testing.T) {
	root := t.TempDir()
	pool, err := Open(root)
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}
	dir := filepath.Join(root, DirName)

	first, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}
	if first.FileName() != "tester@example.com" {
		t.Fatalf("檔名應該用 email，得到 %q", first.FileName())
	}

	// 同一個 email 再加一次時檔名加上序號，不會覆蓋掉前一個帳號。
	second, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c2", CSRFToken: "t2", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}
	if second.FileName() != "tester@example.com-2" {
		t.Fatalf("撞名時應該加序號，得到 %q", second.FileName())
	}
	if _, err := os.Stat(filepath.Join(dir, second.FileName()+".json")); err != nil {
		t.Fatalf("第二個帳號的檔案不存在：%v", err)
	}

	// 換成別的 email 時檔案會更名，舊檔案要消失。
	if _, err := pool.Modify(second.ID, func(acc *Account) error {
		acc.JWT = testJWTWithEmail(t, "renamed@example.com")
		return nil
	}); err != nil {
		t.Fatalf("Modify 失敗：%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tester@example.com-2.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("舊檔名應該被移除：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "renamed@example.com.json"))
	if err != nil {
		t.Fatalf("新檔名的檔案不存在：%v", err)
	}
	if !strings.Contains(string(data), "renamed@example.com") {
		t.Fatalf("檔案內容應該是新的 email：%s", data)
	}

	// 重開之後兩個帳號都還在，檔名維持以 email 命名。
	reopened, err := Open(root)
	if err != nil {
		t.Fatalf("重新 Open 失敗：%v", err)
	}
	if reopened.Count() != 2 {
		t.Fatalf("重開後應該有 2 個帳號，得到 %d", reopened.Count())
	}
	stored, ok := reopened.Get(second.ID)
	if !ok {
		t.Fatal("重開後找不到改名過的帳號")
	}
	if stored.FileName() != "renamed@example.com" {
		t.Fatalf("重開後檔名應該維持 email，得到 %q", stored.FileName())
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
	// 舊的 uuid 檔名會自動改成以 email 為主。
	if _, err := os.Stat(filepath.Join(dir, id+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("舊檔名應該被改名：%v", err)
	}

	// 觸發一次寫回，舊欄位應該要從檔案中消失。
	if _, err := pool.Modify(id, func(acc *Account) error {
		acc.Name = "改名後"
		return nil
	}); err != nil {
		t.Fatalf("Modify 失敗：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "tester@example.com.json"))
	if err != nil {
		t.Fatalf("讀取檔案失敗：%v", err)
	}
	for _, key := range []string{"tool_id", "model_id", "revision", "language", "note"} {
		if strings.Contains(string(data), key) {
			t.Fatalf("舊欄位 %s 應該要消失，檔案內容：%s", key, data)
		}
	}
}

// 額度不足的帳號應該在「該模型」上排到候選順位的最後，
// 沒有其他選擇時仍然可用，而且不會影響其他模型。
func TestQuotaExceededAccountPickedLast(t *testing.T) {
	pool, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}

	first, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}
	second, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}

	// second 剛被用過，first 的 LastUsed 還是零值，因此 first 一定是最久未使用的帳號。
	pool.MarkUsed(second.ID)

	// first 在 model-a 上額度不足。
	pool.MarkQuotaExceeded(first.ID, "model-a")

	stored, _ := pool.Get(first.ID)
	if !stored.QuotaExceededFor("model-a") {
		t.Fatal("model-a 應該被標記為額度不足")
	}
	if stored.QuotaExceededFor("model-b") {
		t.Fatal("model-b 不應該受到 model-a 的額度不足影響")
	}

	// 挑選 model-b 時 first 沒有被降優先序，仍然應該因為最久未使用而被選中。
	picked, err := pool.Pick(nil, "model-b")
	if err != nil {
		t.Fatalf("Pick 失敗：%v", err)
	}
	if picked.ID != first.ID {
		t.Fatalf("其他模型不應該受到影響，應該挑到 first，得到 %s", picked.ID)
	}

	// 挑選 model-a 時，即使 first 最久未使用，也應該讓 second 優先。
	picked, err = pool.Pick(nil, "model-a")
	if err != nil {
		t.Fatalf("Pick 失敗：%v", err)
	}
	if picked.ID != second.ID {
		t.Fatalf("額度不足的帳號應該被排到最後，得到 %s", picked.ID)
	}

	// 沒有其他選擇時，額度不足的帳號仍然可以被挑中（不是被停用）。
	picked, err = pool.Pick(map[string]bool{second.ID: true}, "model-a")
	if err != nil {
		t.Fatalf("額度不足的帳號不應該被停用：%v", err)
	}
	if picked.ID != first.ID {
		t.Fatalf("應該挑到 first，得到 %s", picked.ID)
	}
}

// 清除額度不足標記時只會影響指定的模型。
func TestClearQuotaExceededIsPerModel(t *testing.T) {
	pool, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失敗：%v", err)
	}
	account, err := pool.Create(Account{JWT: testJWT(t), CSRFCookie: "c", CSRFToken: "t", Enabled: true})
	if err != nil {
		t.Fatalf("Create 失敗：%v", err)
	}

	pool.MarkQuotaExceeded(account.ID, "model-a")
	pool.MarkQuotaExceeded(account.ID, "model-b")

	stored, _ := pool.Get(account.ID)
	if !stored.QuotaExceededFor("model-a") || !stored.QuotaExceededFor("model-b") {
		t.Fatal("標記之後兩個模型都應該處於額度不足狀態")
	}
	if got := len(stored.QuotaExceededModels()); got != 2 {
		t.Fatalf("應該有 2 個模型被標記，得到 %d", got)
	}

	pool.ClearQuotaExceeded(account.ID, "model-a")
	stored, _ = pool.Get(account.ID)
	if stored.QuotaExceededFor("model-a") {
		t.Fatal("model-a 的標記應該要清除")
	}
	if !stored.QuotaExceededFor("model-b") {
		t.Fatal("清除 model-a 不應該影響 model-b")
	}
}
