// Package server 提供網頁管理介面與 OpenAI 相容的 /v1 API。
package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"miniapp2api/internal/config"
	"miniapp2api/internal/miniapps"
	"miniapp2api/internal/openai"
	"miniapp2api/internal/store"
)

//go:embed web
var webFiles embed.FS

const (
	sessionCookie = "m2a_session"
	sessionTTL    = 24 * time.Hour
	maxBodySize   = 32 << 20
)

// Server 是 miniapp2api 的 HTTP 伺服器。
type Server struct {
	cfg     *config.Config
	pool    *store.Store
	log     *log.Logger
	sess    *sessionStore
	catalog *catalogCache
	mux     *http.ServeMux
}

// New 建立伺服器並註冊所有路由。
func New(cfg *config.Config, pool *store.Store, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	s := &Server{
		cfg:     cfg,
		pool:    pool,
		log:     logger,
		sess:    newSessionStore(sessionTTL),
		catalog: newCatalogCache(),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

// Handler 回傳 HTTP 處理器。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/session", s.handleSession)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)

	s.mux.HandleFunc("GET /api/accounts", s.auth(s.handleListAccounts))
	s.mux.HandleFunc("POST /api/accounts", s.auth(s.handleCreateAccount))
	s.mux.HandleFunc("PUT /api/accounts/{id}", s.auth(s.handleUpdateAccount))
	s.mux.HandleFunc("DELETE /api/accounts/{id}", s.auth(s.handleDeleteAccount))
	s.mux.HandleFunc("POST /api/accounts/{id}/check", s.auth(s.handleCheckAccount))
	s.mux.HandleFunc("PUT /api/settings", s.auth(s.handleUpdateSettings))
	s.mux.HandleFunc("GET /api/models/catalog", s.auth(s.handleModelCatalog))
	s.mux.HandleFunc("POST /api/models", s.auth(s.handleCreateModel))
	s.mux.HandleFunc("DELETE /api/models/{id}", s.auth(s.handleDeleteModel))

	s.mux.HandleFunc("GET /v1/models", s.v1Auth(s.handleModels))
	s.mux.HandleFunc("GET /v1/models/{id}", s.v1Auth(s.handleModel))
	s.mux.HandleFunc("POST /v1/chat/completions", s.v1Auth(s.handleChatCompletions))

	s.mux.HandleFunc("/", s.handleStatic)
}

// ---------------------------------------------------------------------------
// 工作階段
// ---------------------------------------------------------------------------

type sessionStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]time.Time
}

func newSessionStore(ttl time.Duration) *sessionStore {
	return &sessionStore{ttl: ttl, items: map[string]time.Time{}}
}

func (s *sessionStore) create() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := store.RandomHex(32)
	now := time.Now()
	for key, expiry := range s.items {
		if now.After(expiry) {
			delete(s.items, key)
		}
	}
	s.items[token] = now.Add(s.ttl)
	return token
}

func (s *sessionStore) touch(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	expiry, ok := s.items[token]
	if !ok || time.Now().After(expiry) {
		delete(s.items, token)
		return false
	}
	s.items[token] = time.Now().Add(s.ttl)
	return true
}

func (s *sessionStore) drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, token)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func (s *Server) loggedIn(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	return s.sess.touch(cookie.Value)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.HasPassword() {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "尚未設定登入密碼"})
			return
		}
		if !s.loggedIn(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "請先登入"})
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// 登入／設定
// ---------------------------------------------------------------------------

type sessionPayload struct {
	NeedsSetup    bool             `json:"needs_setup"`
	LoggedIn      bool             `json:"logged_in"`
	RequireAPIKey bool             `json:"require_api_key"`
	APIKey        string           `json:"api_key,omitempty"`
	BaseURL       string           `json:"base_url"`
	Models        []config.Model   `json:"models"`
	Pool          map[string]int64 `json:"pool"`
	DataDir       string           `json:"data_dir"`
	Version       string           `json:"version"`
}

// Version 是程式版本，會顯示在網頁介面上。
const Version = "1.0.0"

func (s *Server) sessionPayload(loggedIn bool) sessionPayload {
	payload := sessionPayload{
		NeedsSetup:    !s.cfg.HasPassword(),
		LoggedIn:      loggedIn,
		RequireAPIKey: s.cfg.RequireKey(),
		BaseURL:       s.cfg.BaseURL(),
		Models:        s.cfg.ModelList(),
		Pool:          s.pool.Summary(),
		DataDir:       s.pool.Dir(),
		Version:       Version,
	}
	if loggedIn {
		payload.APIKey = s.cfg.Key()
	}
	return payload
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sessionPayload(s.loggedIn(r)))
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.cfg.HasPassword() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "已經設定過密碼，請直接登入"})
		return
	}

	var payload struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.SetPassword(payload.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	token := s.sess.create()
	s.setSessionCookie(w, token, int(sessionTTL.Seconds()))
	s.log.Printf("已設定登入密碼")
	writeJSON(w, http.StatusOK, s.sessionPayload(true))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.cfg.CheckPassword(payload.Password); err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, config.ErrPasswordNotSet) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	token := s.sess.create()
	s.setSessionCookie(w, token, int(sessionTTL.Seconds()))
	writeJSON(w, http.StatusOK, s.sessionPayload(true))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sess.drop(cookie.Value)
	}
	s.setSessionCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		RequireAPIKey    *bool  `json:"require_api_key"`
		RegenerateAPIKey bool   `json:"regenerate_api_key"`
		CurrentPassword  string `json:"current_password"`
		NewPassword      string `json:"new_password"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if payload.RequireAPIKey != nil {
		if err := s.cfg.SetRequireKey(*payload.RequireAPIKey); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if payload.RegenerateAPIKey {
		key, err := s.cfg.RegenerateAPIKey()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.log.Printf("已重新產生 API 金鑰：%s", store.MaskToken(key))
	}
	if payload.NewPassword != "" {
		if err := s.cfg.CheckPassword(payload.CurrentPassword); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "目前密碼錯誤"})
			return
		}
		if err := s.cfg.SetPassword(payload.NewPassword); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.log.Printf("登入密碼已變更")
	}

	writeJSON(w, http.StatusOK, s.sessionPayload(true))
}

// ---------------------------------------------------------------------------
// 模型目錄
// ---------------------------------------------------------------------------

// catalogTTL 是模型目錄的快取時間；目錄一次要抓三百多筆而且很少變動。
const catalogTTL = 10 * time.Minute

// errNoCatalogAccount 表示號池中沒有帳號可以讀取模型目錄。
var errNoCatalogAccount = errors.New("號池中沒有可用的帳號，無法讀取模型目錄")

type catalogEntry struct {
	models    []miniapps.AIModel
	fetchedAt time.Time
}

type catalogCache struct {
	mu      sync.Mutex
	entries map[string]catalogEntry
}

func newCatalogCache() *catalogCache {
	return &catalogCache{entries: map[string]catalogEntry{}}
}

func (c *catalogCache) get(toolID string) ([]miniapps.AIModel, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[toolID]
	if !ok || time.Since(entry.fetchedAt) > catalogTTL {
		return nil, false
	}
	return entry.models, true
}

func (c *catalogCache) put(toolID string, models []miniapps.AIModel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[toolID] = catalogEntry{models: models, fetchedAt: time.Now()}
}

// catalogModel 是給網頁介面使用的模型目錄項目。
type catalogModel struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	NativeID     string `json:"native_id"`
	Platform     string `json:"platform"`
	Type         string `json:"type"`
	CreditPrice  int    `json:"credit_price"`
	VariantLabel string `json:"variant_label,omitempty"`
	HasTools     bool   `json:"has_tools"`
	HasVision    bool   `json:"has_vision"`
	IsReasoning  bool   `json:"is_reasoning"`
	Available    bool   `json:"available"`
}

// handleModelCatalog 讀取上游 /ai-models 的模型目錄（只讀取，不消耗 AI 額度）。
//
// 目錄每個 toolId 有三百多筆而且很少變動，因此會快取；
// 要忽略快取重新抓取時帶上 ?refresh=1。
func (s *Server) handleModelCatalog(w http.ResponseWriter, r *http.Request) {
	toolID := strings.TrimSpace(r.URL.Query().Get("tool_id"))
	if toolID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 tool_id 參數"})
		return
	}

	models, fromCache := s.catalog.get(toolID)
	if !fromCache || r.URL.Query().Get("refresh") != "" {
		fetched, err := s.fetchCatalog(r.Context(), toolID)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, errNoCatalogAccount) {
				status = http.StatusServiceUnavailable
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		s.catalog.put(toolID, fetched)
		models, fromCache = fetched, false
	}

	list := make([]catalogModel, 0, len(models))
	for _, model := range models {
		list = append(list, catalogModel{
			ID:           model.ID,
			Title:        model.Title,
			NativeID:     model.NativeID,
			Platform:     model.PlatformID,
			Type:         model.Type,
			CreditPrice:  model.CreditPrice,
			VariantLabel: model.VariantLabel,
			HasTools:     model.HasTools,
			HasVision:    model.HasVision,
			IsReasoning:  model.IsReasoning,
			Available:    model.Enabled && model.IsVisible && !model.IsDown,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tool_id": toolID,
		"cached":  fromCache,
		"total":   len(list),
		"models":  list,
	})
}

// fetchCatalog 用號池中第一個可用的帳號讀取模型目錄。
func (s *Server) fetchCatalog(ctx context.Context, toolID string) ([]miniapps.AIModel, error) {
	account, ok := s.catalogAccount()
	if !ok {
		return nil, errNoCatalogAccount
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client := miniapps.New(miniapps.Credentials{
		JWT:        account.JWT,
		CSRFCookie: account.CSRFCookie,
		CSRFToken:  account.CSRFToken,
		ToolID:     toolID,
	})

	// 目錄比較大，上游偶爾會傳到一半就斷線，因此容許重試一次。
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		models, err := client.AIModels(ctx, toolID)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if miniapps.IsAuthError(err) || ctx.Err() != nil {
			break
		}
	}
	s.log.Printf("讀取模型目錄失敗：%v", lastErr)
	return nil, lastErr
}

// catalogAccount 挑一個帳號來做唯讀查詢。
//
// 這裡刻意不用 pool.Pick，免得更新最後使用時間而影響號池的輪替順序。
func (s *Server) catalogAccount() (store.Account, bool) {
	for _, account := range s.pool.List() {
		if !account.Enabled || account.CoolingDown() {
			continue
		}
		if account.JWT == "" || account.CSRFCookie == "" {
			continue
		}
		return account, true
	}
	return store.Account{}, false
}

// handleCreateModel 新增一個模型設定（對外名稱由呼叫方指定）。
func (s *Server) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		ToolID   string `json:"tool_id"`
		ModelID  string `json:"model_id"`
		Revision int    `json:"revision"`
		Language string `json:"language"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model := config.Model{
		ID:       payload.ID,
		Name:     payload.Name,
		ToolID:   payload.ToolID,
		ModelID:  payload.ModelID,
		Revision: payload.Revision,
		Language: payload.Language,
	}
	switch err := s.cfg.AddModel(model); {
	case errors.Is(err, config.ErrModelExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.log.Printf("已新增模型 %s（toolId %s / modelId %s）", model.ID, model.ToolID, model.ModelID)
	writeJSON(w, http.StatusOK, s.sessionPayload(true))
}

// handleDeleteModel 移除一個模型設定。
func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch err := s.cfg.RemoveModel(id); {
	case errors.Is(err, config.ErrModelNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	case errors.Is(err, config.ErrLastModel):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.log.Printf("已移除模型 %s", id)
	writeJSON(w, http.StatusOK, s.sessionPayload(true))
}

// ---------------------------------------------------------------------------
// 號池
// ---------------------------------------------------------------------------

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := s.pool.List()
	views := make([]store.View, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, account.View())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": views,
		"summary":  s.pool.Summary(),
		"dir":      s.pool.Dir(),
	})
}

type accountPayload struct {
	Name       string `json:"name"`
	JWT        string `json:"jwt"`
	CSRFCookie string `json:"csrf_cookie"`
	CSRFToken  string `json:"csrf_token"`
	Enabled    *bool  `json:"enabled"`
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var payload accountPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	account, err := s.pool.Create(store.Account{
		Name:       payload.Name,
		JWT:        payload.JWT,
		CSRFCookie: payload.CSRFCookie,
		CSRFToken:  payload.CSRFToken,
		Enabled:    enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.log.Printf("新增帳號 %s（%s）", account.DisplayName(), account.ID)
	writeJSON(w, http.StatusOK, account.View())
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var payload struct {
		Name       *string `json:"name"`
		JWT        *string `json:"jwt"`
		CSRFCookie *string `json:"csrf_cookie"`
		CSRFToken  *string `json:"csrf_token"`
		Enabled    *bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	account, err := s.pool.Modify(id, func(acc *store.Account) error {
		if payload.Name != nil {
			acc.Name = strings.TrimSpace(*payload.Name)
		}
		if payload.JWT != nil && strings.TrimSpace(*payload.JWT) != "" {
			acc.JWT = strings.TrimSpace(*payload.JWT)
		}
		if payload.CSRFCookie != nil && strings.TrimSpace(*payload.CSRFCookie) != "" {
			acc.CSRFCookie = strings.TrimSpace(*payload.CSRFCookie)
		}
		if payload.CSRFToken != nil && strings.TrimSpace(*payload.CSRFToken) != "" {
			acc.CSRFToken = strings.TrimSpace(*payload.CSRFToken)
		}
		if payload.Enabled != nil {
			acc.Enabled = *payload.Enabled
		}
		if payload.JWT != nil && strings.TrimSpace(*payload.JWT) != "" {
			acc.LastError = ""
			acc.CooldownUntil = time.Time{}
		}
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, account.View())
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.pool.Delete(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	s.log.Printf("已刪除帳號 %s", id)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleCheckAccount 只呼叫 quickAccess，不會消耗 AI 額度。
func (s *Server) handleCheckAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	account, ok := s.pool.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": store.ErrNotFound.Error()})
		return
	}
	if account.JWT == "" || account.CSRFCookie == "" || account.CSRFToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "此帳號缺少 JWT 或 CSRF 資訊"})
		return
	}

	model := s.cfg.FindModel("")
	client := miniapps.New(credentialsFor(account, model))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	summaries, err := client.QuickAccess(ctx)
	if err != nil {
		s.pool.SetError(id, err.Error())
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.pool.SetError(id, "")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"conversations": len(summaries),
	})
}

// ---------------------------------------------------------------------------
// 靜態檔案
// ---------------------------------------------------------------------------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "找不到 API 路徑"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/v1" {
		writeAPIError(w, http.StatusNotFound, "Unknown endpoint: "+r.URL.Path, "invalid_request_error", "unknown_endpoint")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "不支援的請求方法"})
		return
	}

	if path == "" {
		path = "index.html"
	}

	content, err := webFiles.ReadFile("web/" + path)
	if err != nil {
		// 單頁式介面：其他路徑一律回傳 index.html
		content, err = webFiles.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "找不到網頁介面", http.StatusInternalServerError)
			return
		}
		path = "index.html"
	}

	w.Header().Set("Content-Type", contentTypeOf(path))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}

func contentTypeOf(path string) string {
	switch {
	case strings.HasSuffix(path, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml"
	default:
		return "text/html; charset=utf-8"
	}
}

// ---------------------------------------------------------------------------
// 共用工具
// ---------------------------------------------------------------------------

func credentialsFor(account store.Account, model config.Model) miniapps.Credentials {
	return miniapps.Credentials{
		JWT:        account.JWT,
		CSRFCookie: account.CSRFCookie,
		CSRFToken:  account.CSRFToken,
		ToolID:     model.ToolID,
		ModelID:    model.ModelID,
		Revision:   model.Revision,
		Language:   model.Language,
	}
}

func decodeJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		return fmt.Errorf("讀取請求內容失敗：%w", err)
	}
	defer r.Body.Close()
	if len(strings.TrimSpace(string(body))) == 0 {
		return errors.New("請求內容為空")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("解析請求內容失敗：%w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, message, kind, code string) {
	writeJSON(w, status, openai.ErrorResponse{Error: openai.ErrorDetail{
		Message: message,
		Type:    kind,
		Code:    code,
	}})
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
		return header
	}
	if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
		return key
	}
	return strings.TrimSpace(r.URL.Query().Get("api_key"))
}
