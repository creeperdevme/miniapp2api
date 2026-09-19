// Package config 負責讀寫 miniapp2api 的設定檔 config.json。
//
// config.json 內只儲存登入密碼的雜湊值（PBKDF2-HMAC-SHA256）與 API 金鑰，
// 不會以明文保存密碼。
package config

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// FileName 是設定檔的檔名。
	FileName = "config.json"

	pbkdf2Iterations = 210_000
	pbkdf2KeyLength  = 32
	saltLength       = 16

	// MinPasswordLength 是登入密碼的最短長度。
	MinPasswordLength = 6

	DefaultHost    = "127.0.0.1"
	DefaultPort    = 8787
	DefaultTimeout = 180

	// DefaultToolID 是所有模型共用的 MiniApp ID，寫死在程式裡不開放設定。
	DefaultToolID = "a109c325-fe40-4f50-a815-1bfac2ddb7bb"
)

var (
	// ErrPasswordNotSet 表示系統尚未設定登入密碼。
	ErrPasswordNotSet = errors.New("尚未設定登入密碼")
	// ErrWrongPassword 表示輸入的密碼錯誤。
	ErrWrongPassword = errors.New("密碼錯誤")
	// ErrModelExists 表示同名的模型已經存在。
	ErrModelExists = errors.New("模型已經存在")
	// ErrModelNotFound 表示找不到指定的模型。
	ErrModelNotFound = errors.New("找不到模型")
)

// Model 是一個可被 /v1/models 列出的上游模型設定。
//
// 上游的 toolId 由 DefaultToolID 統一決定，不存放在設定檔裡。
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ModelID  string `json:"model_id"`
	Revision int    `json:"revision"`
	Language string `json:"language"`

	// ToolID 一律由 normalize 填入 DefaultToolID，不會寫進 config.json。
	ToolID string `json:"-"`
}

// normalize 補上模型的預設值。
func (m *Model) normalize() {
	m.ID = strings.TrimSpace(m.ID)
	m.Name = strings.TrimSpace(m.Name)
	m.ModelID = strings.TrimSpace(m.ModelID)
	m.Language = strings.TrimSpace(m.Language)
	m.ToolID = DefaultToolID
	if m.Revision <= 0 {
		m.Revision = 1
	}
	if m.Language == "" {
		m.Language = "zh"
	}
	if m.Name == "" {
		m.Name = m.ID
	}
}

// validate 檢查模型設定的必填欄位。
func (m Model) validate() error {
	switch {
	case m.ID == "":
		return errors.New("模型名稱不可為空")
	case strings.ContainsAny(m.ID, " \t\r\n"):
		return fmt.Errorf("模型名稱不可包含空白：%q", m.ID)
	case m.ModelID == "":
		return fmt.Errorf("模型 %s 缺少 modelId", m.ID)
	}
	return nil
}

// Config 是 config.json 的內容。
type Config struct {
	PasswordSalt   string  `json:"password_salt"`
	PasswordHash   string  `json:"password_hash"`
	APIKey         string  `json:"api_key"`
	RequireAPIKey  *bool   `json:"require_api_key"`
	Host           string  `json:"host"`
	Port           int     `json:"port"`
	RequestTimeout int     `json:"request_timeout_seconds"`
	Models         []Model `json:"models"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`

	path string
	mu   sync.RWMutex
}

// Load 讀取設定檔；檔案不存在時會回傳一份預設設定。
func Load(path string) (*Config, error) {
	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析 %s 失敗：%w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		cfg.CreatedAt = timestamp()
	default:
		return nil, fmt.Errorf("讀取 %s 失敗：%w", path, err)
	}

	cfg.path = path
	cfg.normalize()
	return cfg, nil
}

// Path 回傳設定檔的完整路徑。
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// normalize 補上缺少的預設值。
//
// 模型預設是空的，一律由使用者從模型目錄加入，程式不會自動塞入任何模型。
func (c *Config) normalize() {
	if c.CreatedAt == "" {
		c.CreatedAt = timestamp()
	}
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = DefaultPort
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = DefaultTimeout
	}
	for i := range c.Models {
		c.Models[i].normalize()
	}
	if c.RequireAPIKey == nil {
		required := true
		c.RequireAPIKey = &required
	}
}

func hasModel(models []Model, id string) bool {
	for _, model := range models {
		if strings.EqualFold(model.ID, id) {
			return true
		}
	}
	return false
}

// Save 將設定寫回 config.json。
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	if c.path == "" {
		return errors.New("設定檔路徑為空")
	}
	c.UpdatedAt = timestamp()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}

	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// HasPassword 回報是否已經設定登入密碼。
func (c *Config) HasPassword() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PasswordHash != "" && c.PasswordSalt != ""
}

// SetPassword 設定（或變更）登入密碼並寫入設定檔。
func (c *Config) SetPassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("密碼長度至少需要 %d 個字元", MinPasswordLength)
	}

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("產生亂數失敗：%w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return fmt.Errorf("密碼雜湊失敗：%w", err)
	}

	c.mu.Lock()
	c.PasswordSalt = base64.StdEncoding.EncodeToString(salt)
	c.PasswordHash = base64.StdEncoding.EncodeToString(key)
	c.mu.Unlock()

	return c.Save()
}

// CheckPassword 驗證密碼是否正確。
func (c *Config) CheckPassword(password string) error {
	c.mu.RLock()
	saltB64, hashB64 := c.PasswordSalt, c.PasswordHash
	c.mu.RUnlock()

	if saltB64 == "" || hashB64 == "" {
		return ErrPasswordNotSet
	}

	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return fmt.Errorf("設定檔的密碼鹽值損毀：%w", err)
	}
	want, err := base64.StdEncoding.DecodeString(hashB64)
	if err != nil {
		return fmt.Errorf("設定檔的密碼雜湊損毀：%w", err)
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, len(want))
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrWrongPassword
	}
	return nil
}

// RequireKey 回報 /v1 API 是否必須帶 API 金鑰。
func (c *Config) RequireKey() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RequireAPIKey == nil || *c.RequireAPIKey
}

// SetRequireKey 設定 /v1 API 是否需要 API 金鑰。
func (c *Config) SetRequireKey(required bool) error {
	c.mu.Lock()
	c.RequireAPIKey = &required
	c.mu.Unlock()
	return c.Save()
}

// Key 回傳目前的 API 金鑰。
func (c *Config) Key() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.APIKey
}

// MaskKey 只保留金鑰的頭尾，供介面顯示用。
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 14 {
		return strings.Repeat("•", len(key))
	}
	return key[:10] + strings.Repeat("•", 8) + key[len(key)-4:]
}

// KeyPreview 回傳遮罩後的 API 金鑰。
func (c *Config) KeyPreview() string {
	return MaskKey(c.Key())
}

// EnsureAPIKey 在金鑰不存在時自動產生一組。
func (c *Config) EnsureAPIKey() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.APIKey != "" {
		return false, nil
	}
	key, err := newAPIKey()
	if err != nil {
		return false, err
	}
	c.APIKey = key
	return true, nil
}

// RegenerateAPIKey 重新產生一組 API 金鑰。
func (c *Config) RegenerateAPIKey() (string, error) {
	key, err := newAPIKey()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.APIKey = key
	c.mu.Unlock()
	if err := c.Save(); err != nil {
		return "", err
	}
	return key, nil
}

// CheckKey 比對 API 金鑰。
func (c *Config) CheckKey(key string) bool {
	current := c.Key()
	if current == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(key)), []byte(current)) == 1
}

// ListenAddr 回傳 http 伺服器要監聽的位址。
func (c *Config) ListenAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// SetListenAddr 以 "host:port" 的形式覆寫監聽位址。
func (c *Config) SetListenAddr(addr string) error {
	host, port, err := splitAddr(addr)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.Host, c.Port = host, port
	c.mu.Unlock()
	return nil
}

// Timeout 回傳單次請求等待上游回應的最長時間。
func (c *Config) Timeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seconds := c.RequestTimeout
	if seconds <= 0 {
		seconds = DefaultTimeout
	}
	return time.Duration(seconds) * time.Second
}

// ModelList 回傳模型設定的副本。
func (c *Config) ModelList() []Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Model, len(c.Models))
	copy(out, c.Models)
	return out
}

// FindModel 依名稱尋找模型設定，找不到時回傳第一個模型。
func (c *Config) FindModel(id string) Model {
	models := c.ModelList()
	if len(models) == 0 {
		return Model{}
	}
	id = strings.TrimSpace(id)
	for _, m := range models {
		if strings.EqualFold(m.ID, id) {
			return m
		}
	}
	return models[0]
}

// AddModel 新增一個上游模型設定並寫回 config.json。
func (c *Config) AddModel(model Model) error {
	model.normalize()
	if err := model.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	if hasModel(c.Models, model.ID) {
		c.mu.Unlock()
		return fmt.Errorf("%w：%s", ErrModelExists, model.ID)
	}
	c.Models = append(c.Models, model)
	c.mu.Unlock()
	return c.Save()
}

// RemoveModel 移除一個模型設定並寫回 config.json。
func (c *Config) RemoveModel(id string) error {
	id = strings.TrimSpace(id)

	c.mu.Lock()
	index := -1
	for i := range c.Models {
		if strings.EqualFold(c.Models[i].ID, id) {
			index = i
			break
		}
	}
	if index < 0 {
		c.mu.Unlock()
		return fmt.Errorf("%w：%s", ErrModelNotFound, id)
	}
	c.Models = append(c.Models[:index], c.Models[index+1:]...)
	c.mu.Unlock()
	return c.Save()
}

// BaseURL 回傳給客戶端使用的 OpenAI 相容網址。
func (c *Config) BaseURL() string {
	c.mu.RLock()
	host, port := c.Host, c.Port
	c.mu.RUnlock()
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = DefaultHost
	}
	return fmt.Sprintf("http://%s:%d/v1", host, port)
}

func newAPIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("產生 API 金鑰失敗：%w", err)
	}
	return "sk-m2a-" + hex.EncodeToString(buf), nil
}

func splitAddr(addr string) (string, int, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", 0, fmt.Errorf("位址格式錯誤，請使用 host:port（例如 127.0.0.1:8787）：%w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, fmt.Errorf("連接埠格式錯誤：%w", err)
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("連接埠必須介於 1-65535，收到 %d", port)
	}
	return host, port, nil
}

func timestamp() string {
	return time.Now().Format(time.RFC3339)
}
