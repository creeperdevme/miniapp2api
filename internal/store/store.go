// Package store 管理號池（帳號池），每個帳號以 auths/{email}.json 儲存。
package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// DirName 是帳號檔案的目錄名稱。
	DirName = "auths"

	// CooldownDuration 是帳號發生錯誤後的冷卻時間。
	CooldownDuration = 60 * time.Second

	// QuotaWindow 是帳號額度不足後，被排到候選順位最後的時間長度。
	//
	// 額度不足是「帳號 × 模型」層級：同一個帳號可能買得起便宜模型、
	// 卻買不起昂貴模型，所以標記要按模型分開記錄，不能整個帳號一起降優先序。
	// 讓帳號冷卻會連帶使其他模型一起不能用，因此不冷卻；
	// 改成只把它排到候選順位的最後，期限過後自動回到正常順位。
	QuotaWindow = 10 * time.Minute
)

// ErrNotFound 表示找不到指定的帳號。
var ErrNotFound = errors.New("找不到帳號")

// ErrNoAccount 表示目前沒有可用的帳號。
var ErrNoAccount = errors.New("號池中沒有可用的帳號（可能都已停用或正在冷卻中）")

// Stats 是帳號的使用統計。
type Stats struct {
	Requests int64 `json:"requests"`
	Success  int64 `json:"success"`
	Failed   int64 `json:"failed"`
}

// Account 是單一帳號的設定。
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	JWT  string `json:"jwt"`

	// Password 是選填的登入密碼；填寫後 JWT 快到期時會自動重新登入續期。
	// 留白時帳號的 JWT 過期就只能手動重新貼上。
	Password string `json:"password,omitempty"`

	// CSRFCookie 與 CSRFToken 為選填；留空時中轉會自動向 /auth/csrf 取得。
	CSRFCookie string `json:"csrf_cookie"`
	CSRFToken  string `json:"csrf_token"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	Email      string `json:"email,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Stats      Stats  `json:"stats"`

	// 以下為執行期狀態，不會寫入檔案。
	LastError       string               `json:"-"`
	LastErrorAt     string               `json:"-"`
	CooldownUntil   time.Time            `json:"-"`
	QuotaExceededAt map[string]time.Time `json:"-"`

	// file 是這個帳號在 auths/ 下的檔名（不含 .json），由 Store 維護。
	file string
}

// FileName 回傳帳號在 auths/ 下的檔名（不含 .json 副檔名）。
func (a Account) FileName() string {
	if a.file != "" {
		return a.file
	}
	return defaultFileName(a)
}

// defaultFileName 以 email 為主，沒有 email 時退回名稱，最後才用 ID。
func defaultFileName(acc Account) string {
	if name := slugify(acc.Email); name != "" {
		return name
	}
	if name := slugify(acc.Name); name != "" {
		return name
	}
	return acc.ID
}

// reservedNames 是 Windows 保留下來的裝置名稱，不能當檔名。
var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// slugify 把文字轉成安全的檔名片段，只保留小寫英數與 @ . + _ -。
func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dashed := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '@', r == '.', r == '+', r == '_', r == '-':
			b.WriteRune(r)
			dashed = r == '-'
		default:
			if !dashed {
				b.WriteByte('-')
				dashed = true
			}
		}
	}

	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return ""
	}
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	if reservedNames[stem] {
		return "acct-" + name
	}
	return name
}

// LastUsed 回傳最後使用時間，未曾使用時回傳零值。
func (a Account) LastUsed() time.Time {
	t, err := time.Parse(time.RFC3339, a.LastUsedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Expired 回報 JWT 是否已過期。
func (a Account) Expired() bool {
	exp, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return false
	}
	return time.Now().After(exp)
}

// ExpiringWithin 回報 JWT 是否會在指定的時間內到期（已過期也算）。
func (a Account) ExpiringWithin(d time.Duration) bool {
	exp, err := time.Parse(time.RFC3339, a.ExpiresAt)
	if err != nil {
		return false
	}
	return time.Now().Add(d).After(exp)
}

// AutoRenewable 回報這個帳號是否能用帳密自動續期。
func (a Account) AutoRenewable() bool {
	return strings.TrimSpace(a.Password) != "" && strings.TrimSpace(a.Email) != ""
}

// CoolingDown 回報帳號是否在錯誤冷卻中。
func (a Account) CoolingDown() bool {
	return !a.CooldownUntil.IsZero() && time.Now().Before(a.CooldownUntil)
}

// QuotaExceededFor 回報帳號是否因為「指定模型」額度不足而被降優先序。
//
// 額度不足是「帳號 × 模型」層級：402／412 取決於該模型的 creditPrice
// 與帳號餘額，同一個帳號可以買得起便宜模型卻買不起昂貴模型。
func (a Account) QuotaExceededFor(modelID string) bool {
	at, ok := a.QuotaExceededAt[modelID]
	return ok && !at.IsZero() && time.Since(at) < QuotaWindow
}

// QuotaExceededModels 回傳目前因額度不足而被降優先序的模型 ID（已排序）。
func (a Account) QuotaExceededModels() []string {
	ids := make([]string, 0, len(a.QuotaExceededAt))
	for modelID := range a.QuotaExceededAt {
		if a.QuotaExceededFor(modelID) {
			ids = append(ids, modelID)
		}
	}
	sort.Strings(ids)
	return ids
}

// DisplayName 回傳用來顯示的名稱。
func (a Account) DisplayName() string {
	if strings.TrimSpace(a.Name) != "" {
		return a.Name
	}
	if a.Email != "" {
		return a.Email
	}
	return a.ID
}

// Redacted 回傳移除了敏感欄位（JWT、CSRF）的副本，供網頁介面顯示。
func (a Account) Redacted() Account {
	a.JWT = mask(a.JWT)
	a.CSRFCookie = mask(a.CSRFCookie)
	a.CSRFToken = mask(a.CSRFToken)
	a.Password = mask(a.Password)
	return a
}

// View 是給網頁介面使用的帳號資料（已遮蔽敏感欄位並附上執行期狀態）。
type View struct {
	Account
	FileName      string   `json:"file_name"`
	CoolingDown   bool     `json:"cooling_down"`
	QuotaExceeded bool     `json:"quota_exceeded"`
	QuotaModels   []string `json:"quota_models,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
	LastErrorAt   string   `json:"last_error_at,omitempty"`
}

// View 產生網頁介面用的資料。
func (a Account) View() View {
	quotaModels := a.QuotaExceededModels()
	return View{
		Account:       a.Redacted(),
		FileName:      a.FileName() + ".json",
		CoolingDown:   a.CoolingDown(),
		QuotaExceeded: len(quotaModels) > 0,
		QuotaModels:   quotaModels,
		LastError:     a.LastError,
		LastErrorAt:   a.LastErrorAt,
	}
}

func mask(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 10 {
		return strings.Repeat("•", len(token))
	}
	return token[:6] + strings.Repeat("•", 8) + token[len(token)-4:]
}

// Store 是磁碟上的號池。
type Store struct {
	dir string

	mu       sync.RWMutex
	accounts map[string]*Account
	order    []string
	lastPick int
}

// Open 開啟（必要時建立）號池目錄並載入所有帳號。
func Open(root string) (*Store, error) {
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("建立 %s 失敗：%w", dir, err)
	}

	s := &Store{dir: dir, accounts: map[string]*Account{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("讀取 %s 失敗：%w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("讀取 %s 失敗：%w", path, err)
		}
		var acc Account
		if err := json.Unmarshal(data, &acc); err != nil {
			return nil, fmt.Errorf("解析 %s 失敗：%w", path, err)
		}
		if acc.ID == "" {
			acc.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		if acc.CreatedAt == "" {
			acc.CreatedAt = time.Now().Format(time.RFC3339)
		}
		applyJWT(&acc)
		acc.file = strings.TrimSuffix(entry.Name(), ".json")
		s.accounts[acc.ID] = &acc
		s.order = append(s.order, acc.ID)
	}

	sort.Strings(s.order)
	s.renameToEmailFiles()
	return s, nil
}

// renameToEmailFiles 把舊的 uuid 檔名改成以 email 為主的檔名。
// 目標檔名已經被其他帳號使用時保留原檔名，避免覆蓋掉別人的設定。
func (s *Store) renameToEmailFiles() {
	for _, id := range s.order {
		acc := s.accounts[id]
		if acc == nil {
			continue
		}
		desired := defaultFileName(*acc)
		if desired == "" || desired == acc.file || s.fileTaken(desired, id) {
			continue
		}
		if err := os.Rename(s.pathFor(acc.file), s.pathFor(desired)); err == nil {
			acc.file = desired
		}
	}
}

// pathFor 回傳檔名在號池目錄中的完整路徑。
func (s *Store) pathFor(name string) string {
	return filepath.Join(s.dir, name+".json")
}

// fileTaken 回報檔名是否已經被其他帳號使用。
func (s *Store) fileTaken(name, exceptID string) bool {
	for id, other := range s.accounts {
		if id == exceptID || other == nil {
			continue
		}
		if strings.EqualFold(other.FileName(), name) {
			return true
		}
	}
	return false
}

// assignFile 依帳號內容決定檔名，撞名時加上序號。
func (s *Store) assignFile(acc *Account) {
	base := defaultFileName(*acc)
	if base == "" {
		base = acc.ID
	}
	name := base
	for i := 2; s.fileTaken(name, acc.ID); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	acc.file = name
}

// Dir 回傳號池目錄的完整路徑。
func (s *Store) Dir() string { return s.dir }

// List 回傳所有帳號（依建立時間排序）的副本。
func (s *Store) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := append([]string(nil), s.order...)
	sort.SliceStable(ids, func(i, j int) bool {
		left, right := s.accounts[ids[i]], s.accounts[ids[j]]
		if left == nil || right == nil {
			return ids[i] < ids[j]
		}
		if left.CreatedAt != right.CreatedAt {
			return left.CreatedAt < right.CreatedAt
		}
		return left.ID < right.ID
	})

	out := make([]Account, 0, len(ids))
	for _, id := range ids {
		if acc, ok := s.accounts[id]; ok {
			out = append(out, *acc)
		}
	}
	return out
}

// Get 取得單一帳號的副本。
func (s *Store) Get(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	acc, ok := s.accounts[id]
	if !ok {
		return Account{}, false
	}
	return *acc, true
}

// Count 回傳帳號總數。
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.accounts)
}

// Create 新增一個帳號並寫入 auths/{email}.json。
func (s *Store) Create(acc Account) (Account, error) {
	if strings.TrimSpace(acc.JWT) == "" {
		return Account{}, errors.New("JWT 不可為空")
	}

	now := time.Now().Format(time.RFC3339)
	if acc.ID == "" {
		acc.ID = NewUUID()
	}
	acc.CreatedAt = now
	acc.UpdatedAt = now
	acc.JWT = strings.TrimSpace(acc.JWT)
	acc.CSRFCookie = strings.TrimSpace(acc.CSRFCookie)
	acc.CSRFToken = strings.TrimSpace(acc.CSRFToken)
	acc.Name = strings.TrimSpace(acc.Name)
	applyJWT(&acc)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.accounts[acc.ID]; exists {
		return Account{}, fmt.Errorf("帳號 %s 已存在", acc.ID)
	}
	s.assignFile(&acc)
	if err := s.saveLocked(&acc); err != nil {
		return Account{}, err
	}
	stored := acc
	s.accounts[acc.ID] = &stored
	s.order = append(s.order, acc.ID)
	return acc, nil
}

// Modify 以 mutate 函式修改帳號並寫回檔案。
func (s *Store) Modify(id string, mutate func(*Account) error) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	if err := mutate(acc); err != nil {
		return Account{}, err
	}
	acc.UpdatedAt = time.Now().Format(time.RFC3339)
	applyJWT(acc)
	previous := acc.file
	s.assignFile(acc)
	if err := s.saveLocked(acc); err != nil {
		return Account{}, err
	}
	if previous != "" && previous != acc.file {
		// 檔名跟著 email 改變時，把舊檔案清掉。
		_ = os.Remove(s.pathFor(previous))
	}
	return *acc, nil
}

// Delete 刪除帳號與其檔案。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return ErrNotFound
	}
	if err := os.Remove(s.pathFor(acc.FileName())); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(s.accounts, id)
	for i, existing := range s.order {
		if existing == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return nil
}

// Record 記錄一次請求結果；cooldown 大於 0 時，失敗會讓帳號進入冷卻。
func (s *Store) Record(id string, requestErr error, cooldown time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return
	}
	now := time.Now()
	acc.Stats.Requests++
	acc.LastUsedAt = now.Format(time.RFC3339)
	if requestErr == nil {
		acc.Stats.Success++
		acc.LastError = ""
		acc.LastErrorAt = ""
		acc.CooldownUntil = time.Time{}
	} else {
		acc.Stats.Failed++
		acc.LastError = requestErr.Error()
		acc.LastErrorAt = now.Format(time.RFC3339)
		if cooldown > 0 {
			acc.CooldownUntil = now.Add(cooldown)
		}
	}
	_ = s.saveLocked(acc)
}

// MarkQuotaExceeded 記錄帳號在「指定模型」上額度不足。
//
// 這只會讓該帳號在挑選該模型時排到最後，並不會停用帳號；
// 其他還有額度的帳號會優先被選到，全部都沒額度時仍會輪到它。
func (s *Store) MarkQuotaExceeded(id, modelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[id]
	if !ok {
		return
	}
	if acc.QuotaExceededAt == nil {
		acc.QuotaExceededAt = map[string]time.Time{}
	}
	acc.QuotaExceededAt[modelID] = time.Now()
}

// ClearQuotaExceeded 清除帳號在「指定模型」上的額度不足標記。
func (s *Store) ClearQuotaExceeded(id, modelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if acc, ok := s.accounts[id]; ok {
		delete(acc.QuotaExceededAt, modelID)
	}
}

// MarkUsed 標記帳號剛被選用（僅更新記憶體狀態）。
func (s *Store) MarkUsed(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if acc, ok := s.accounts[id]; ok {
		acc.LastUsedAt = time.Now().Format(time.RFC3339)
	}
}

// SetError 只更新錯誤訊息，不計入統計（例如手動測試失敗）。
func (s *Store) SetError(id, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[id]
	if !ok {
		return
	}
	acc.LastError = message
	if message == "" {
		acc.LastErrorAt = ""
	} else {
		acc.LastErrorAt = time.Now().Format(time.RFC3339)
	}
	_ = s.saveLocked(acc)
}

// Pick 依「最久未使用」挑選一個可用帳號。
//
// 在 modelID 這個模型上額度不足的帳號會被排到候選順位的最後，
// 但仍然可以被挑中（不冷卻、不停用）。
func (s *Store) Pick(exclude map[string]bool, modelID string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var candidates []*Account
	for _, acc := range s.accounts {
		if exclude[acc.ID] || !acc.Enabled || acc.CoolingDown() {
			continue
		}
		candidates = append(candidates, acc)
	}
	if len(candidates) == 0 {
		return Account{}, ErrNoAccount
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if left, right := candidates[i].QuotaExceededFor(modelID), candidates[j].QuotaExceededFor(modelID); left != right {
			return !left
		}
		left, right := candidates[i].LastUsed(), candidates[j].LastUsed()
		if !left.Equal(right) {
			return left.Before(right)
		}
		if candidates[i].CreatedAt != candidates[j].CreatedAt {
			return candidates[i].CreatedAt < candidates[j].CreatedAt
		}
		return candidates[i].ID < candidates[j].ID
	})
	s.lastPick++

	chosen := candidates[0]
	chosen.LastUsedAt = time.Now().Format(time.RFC3339)
	return *chosen, nil
}

// Summary 回傳號池的整體統計。
func (s *Store) Summary() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := map[string]int64{"total": int64(len(s.accounts))}
	for _, acc := range s.accounts {
		if acc.Enabled {
			summary["enabled"]++
		} else {
			summary["disabled"]++
		}
		if acc.CoolingDown() {
			summary["cooldown"]++
		}
		if len(acc.QuotaExceededModels()) > 0 {
			summary["quota"]++
		}
		if acc.Expired() {
			summary["expired"]++
		}
		summary["requests"] += acc.Stats.Requests
		summary["success"] += acc.Stats.Success
		summary["failed"] += acc.Stats.Failed
	}
	return summary
}

func (s *Store) saveLocked(acc *Account) error {
	data, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	path := s.pathFor(acc.FileName())
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("寫入 %s 失敗：%w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("更新 %s 失敗：%w", path, err)
	}
	return nil
}

// NewUUID 產生一組 v4 UUID。
func NewUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 幾乎不會失敗，退而求其次用時間戳。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// applyJWT 從 JWT 解出信箱與到期時間，方便介面顯示。
func applyJWT(acc *Account) {
	claims, ok := ParseJWT(acc.JWT)
	if !ok {
		return
	}
	if claims.Email != "" {
		acc.Email = claims.Email
	}
	if !claims.Expires.IsZero() {
		acc.ExpiresAt = claims.Expires.Format(time.RFC3339)
	}
}

// JWTClaims 是從 JWT payload 解析出來的重點欄位。
type JWTClaims struct {
	Email   string
	Subject string
	Expires time.Time
	Issued  time.Time
}

// ParseJWT 解析 JWT 的 payload（不驗證簽章，只用於顯示資訊）。
func ParseJWT(token string) (JWTClaims, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return JWTClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return JWTClaims{}, false
	}

	var raw struct {
		Email string          `json:"email"`
		ID    string          `json:"id"`
		Sub   string          `json:"sub"`
		Exp   json.RawMessage `json:"exp"`
		Iat   json.RawMessage `json:"iat"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return JWTClaims{}, false
	}

	claims := JWTClaims{Email: raw.Email, Subject: raw.ID}
	if claims.Subject == "" {
		claims.Subject = raw.Sub
	}
	claims.Expires = parseNumericDate(raw.Exp)
	claims.Issued = parseNumericDate(raw.Iat)
	return claims, true
}

func parseNumericDate(raw json.RawMessage) time.Time {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return time.Time{}
	}
	var seconds float64
	if _, err := fmt.Sscanf(text, "%g", &seconds); err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(seconds), 0)
}

// MaskToken 是提供給外部使用的權杖遮蔽函式。
func MaskToken(token string) string { return mask(token) }

// RandomHex 產生指定長度的隨機十六進位字串。
func RandomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf)
}
