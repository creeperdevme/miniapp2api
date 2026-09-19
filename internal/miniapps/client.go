// Package miniapps 是 api.miniapps.ai 的 Go 用戶端，
// 負責送出訊息並等待 AI 回覆，作為中轉（proxy）的上游。
package miniapps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// APIBase 是上游 API 的網址。
const APIBase = "https://api.miniapps.ai"

const (
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"
	pollInterval    = 500 * time.Millisecond
	maxResponseSize = 16 << 20
	graceBeforeDone = 3 * time.Second
)

// Credentials 是一次中轉所需的登入資訊。
type Credentials struct {
	JWT        string
	CSRFCookie string
	CSRFToken  string
	ToolID     string
	ModelID    string
	Revision   int
	Language   string
}

// Validate 檢查必填欄位。
func (c Credentials) Validate() error {
	switch {
	case strings.TrimSpace(c.JWT) == "":
		return errors.New("缺少 JWT")
	case strings.TrimSpace(c.CSRFCookie) == "":
		return errors.New("缺少 CSRF_Cookie")
	case strings.TrimSpace(c.CSRFToken) == "":
		return errors.New("缺少 CSRF_Token")
	case strings.TrimSpace(c.ToolID) == "":
		return errors.New("缺少 toolId")
	case strings.TrimSpace(c.ModelID) == "":
		return errors.New("缺少 modelId")
	}
	return nil
}

// Error 是上游回傳的錯誤。
type Error struct {
	Op     string
	Status int
	Body   string
	Err    error
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Op)
	b.WriteString(" 失敗")
	if e.Status != 0 {
		fmt.Fprintf(&b, "（HTTP %d）", e.Status)
	}
	if e.Body != "" {
		b.WriteString("：")
		b.WriteString(e.Body)
	}
	if e.Err != nil {
		if e.Body == "" {
			b.WriteString("：")
		}
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

func (e *Error) Unwrap() error { return e.Err }

// Auth 回報這是否為帳號憑證相關的錯誤。
func (e *Error) Auth() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// Quota 回報這是否為額度不足的錯誤（402、412）。
func (e *Error) Quota() bool {
	return e.Status == http.StatusPaymentRequired || e.Status == http.StatusPreconditionFailed
}

// RateLimited 回報是否被上游限流。
func (e *Error) RateLimited() bool {
	return e.Status == http.StatusTooManyRequests
}

// IsAuthError 判斷錯誤是否代表帳號憑證失效。
func IsAuthError(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Auth()
	}
	return false
}

// IsQuotaError 判斷錯誤是否代表帳號額度不足。
func IsQuotaError(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Quota()
	}
	return false
}

// IsRateLimited 判斷錯誤是否為上游限流。
func IsRateLimited(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.RateLimited()
	}
	return false
}

// IsTimeout 判斷錯誤是否為等待逾時。
func IsTimeout(err error) bool {
	var timeoutErr *TimeoutError
	return errors.As(err, &timeoutErr)
}

// TimeoutError 表示等待 AI 回覆逾時。
type TimeoutError struct {
	ConversationID string
	Waited         time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("等待 AI 回覆逾時（已等待 %s）", e.Waited.Round(time.Second))
}

// Client 是單一帳號的上游連線。
type Client struct {
	creds   Credentials
	baseURL string
	http    *http.Client

	mu             sync.Mutex
	conversationID string
}

// New 建立一個使用指定帳號的上游用戶端。
func New(creds Credentials) *Client {
	if creds.Revision <= 0 {
		creds.Revision = 1
	}
	if strings.TrimSpace(creds.Language) == "" {
		creds.Language = "zh"
	}
	return &Client{
		creds:   creds,
		baseURL: APIBase,
		http: &http.Client{
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

// Credentials 回傳此用戶端使用的登入資訊。
func (c *Client) Credentials() Credentials { return c.creds }

// ConversationID 回傳目前的對話 ID。
func (c *Client) ConversationID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conversationID
}

// SetConversation 指定要延續的對話 ID。
func (c *Client) SetConversation(id string) {
	c.mu.Lock()
	c.conversationID = id
	c.mu.Unlock()
}

// NewConversation 讓下一次送出訊息時建立新對話。
func (c *Client) NewConversation() {
	c.SetConversation("")
}

// Ask 送出訊息並等待回覆，回傳（回覆內容, conversationId, error）。
func (c *Client) Ask(ctx context.Context, text string, timeout time.Duration) (string, string, error) {
	conversationID, err := c.Send(ctx, text)
	if err != nil {
		return "", "", err
	}
	answer, err := c.Wait(ctx, conversationID, text, timeout)
	if err != nil {
		return "", conversationID, err
	}
	return answer, conversationID, nil
}

// Send 送出使用者訊息，回傳上游建立的 conversationId。
func (c *Client) Send(ctx context.Context, text string) (string, error) {
	if err := c.creds.Validate(); err != nil {
		return "", err
	}

	payload := chatPayload{
		ToolID:    c.creds.ToolID,
		Revision:  c.creds.Revision,
		ModelID:   c.creds.ModelID,
		RequestID: newRequestID(),
		Elements:  []chatElement{{Type: "text", Text: text}},
		Language:  c.creds.Language,
	}
	if existing := c.ConversationID(); existing != "" {
		payload.ConversationID = existing
	}

	body, status, err := c.do(ctx, http.MethodPost, "/chat", nil, payload)
	if err != nil {
		return "", &Error{Op: "POST /chat", Err: err}
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", &Error{Op: "POST /chat", Status: status, Body: truncate(string(body), 800)}
	}

	conversationID := findString(body, "conversationId")
	if conversationID == "" {
		return "", &Error{Op: "POST /chat", Status: status, Body: "回應中找不到 conversationId：" + truncate(string(body), 400)}
	}
	c.SetConversation(conversationID)
	return conversationID, nil
}

// QuickAccess 取得最近的對話清單（含 AI 是否正在輸出）。
func (c *Client) QuickAccess(ctx context.Context) ([]Summary, error) {
	options := fmt.Sprintf(`{"itemsPerPage":20,"page":1,"lang":%q}`, c.creds.Language)
	query := url.Values{"options": {options}}

	body, status, err := c.do(ctx, http.MethodGet, "/conversations/quickAccess", query, nil)
	if err != nil {
		return nil, &Error{Op: "GET /conversations/quickAccess", Err: err}
	}
	if status != http.StatusOK {
		return nil, &Error{Op: "GET /conversations/quickAccess", Status: status, Body: truncate(string(body), 800)}
	}

	items := findArray(body, "items")
	if items == nil {
		items = findArray(body, "data")
	}

	summaries := make([]Summary, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		summaries = append(summaries, Summary{
			ID:                 stringField(object, "id"),
			LastMessageExcerpt: stringField(object, "lastMessageExcerpt"),
			Writing:            isWriting(object),
		})
	}
	return summaries, nil
}

// Conversation 取得完整對話內容。
func (c *Client) Conversation(ctx context.Context, conversationID string) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/conversations/"+url.PathEscape(conversationID), nil, nil)
	if err != nil {
		return nil, &Error{Op: "GET /conversations/{id}", Err: err}
	}
	if status != http.StatusOK {
		return nil, &Error{Op: "GET /conversations/{id}", Status: status, Body: truncate(string(body), 800)}
	}
	return body, nil
}

// Messages 取得對話的完整訊息列表（含完整的 AI 回覆內容）。
func (c *Client) Messages(ctx context.Context, conversationID string) ([]byte, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/conversations/"+url.PathEscape(conversationID)+"/messages", nil, nil)
	if err != nil {
		return nil, &Error{Op: "GET /conversations/{id}/messages", Err: err}
	}
	if status != http.StatusOK {
		return nil, &Error{Op: "GET /conversations/{id}/messages", Status: status, Body: truncate(string(body), 800)}
	}
	return body, nil
}

// AIModel 是 /ai-models 目錄中的單一上游模型。
type AIModel struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	NativeID     string `json:"nativeId"`
	PlatformID   string `json:"platformId"`
	Type         string `json:"type"`
	CreditPrice  int    `json:"creditPrice"`
	VariantLabel string `json:"variantLabel"`
	HasTools     bool   `json:"hasTools"`
	HasVision    bool   `json:"hasVision"`
	IsReasoning  bool   `json:"isReasoning"`
	Enabled      bool   `json:"enabled"`
	IsVisible    bool   `json:"isVisible"`
	IsDown       bool   `json:"isDown"`
}

// AIModels 取得指定 toolId 可用的模型目錄（只讀取清單，不會消耗 AI 額度）。
//
// 目錄只依賴帳號的 JWT / CSRF，不需要 modelId，回傳結果會依名稱排序。
func (c *Client) AIModels(ctx context.Context, toolID string) ([]AIModel, error) {
	options := `{"itemsPerPage":10000}`
	query := url.Values{"options": {options}}
	if tool := strings.TrimSpace(toolID); tool != "" {
		query.Set("toolId", tool)
	}

	body, status, err := c.do(ctx, http.MethodGet, "/ai-models", query, nil)
	if err != nil {
		return nil, &Error{Op: "GET /ai-models", Err: err}
	}
	if status != http.StatusOK {
		return nil, &Error{Op: "GET /ai-models", Status: status, Body: truncate(string(body), 800)}
	}

	var payload struct {
		Items []AIModel `json:"items"`
		Data  []AIModel `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, &Error{Op: "GET /ai-models", Body: "回應格式不是預期的 JSON：" + truncate(string(body), 400), Err: err}
	}
	models := payload.Items
	if models == nil {
		models = payload.Data
	}
	sort.SliceStable(models, func(i, j int) bool {
		left, right := strings.ToLower(models[i].Title), strings.ToLower(models[j].Title)
		if left != right {
			return left < right
		}
		return models[i].ID < models[j].ID
	})
	return models, nil
}

// Wait 輪詢直到 AI 回覆完成。
func (c *Client) Wait(ctx context.Context, conversationID, userText string, timeout time.Duration) (string, error) {
	start := time.Now()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	sawWriting := false
	consecutiveErrors := 0
	var lastErr error
	var firstExcerpt string
	excerptSeen := false

	for {
		summaries, err := c.QuickAccess(ctx)
		switch {
		case err != nil:
			consecutiveErrors++
			lastErr = err
			if IsAuthError(err) || consecutiveErrors >= 5 {
				return "", err
			}
		default:
			consecutiveErrors = 0
			if summary, ok := findSummary(summaries, conversationID); ok {
				if summary.Writing {
					sawWriting = true
				}
				excerpt := strings.TrimSpace(summary.LastMessageExcerpt)
				if !excerptSeen {
					// 第一次看到的內容通常是對話開頭的招呼語，不能當成答案。
					firstExcerpt = excerpt
					excerptSeen = true
				}
				done := !summary.Writing && excerpt != "" &&
					excerpt != strings.TrimSpace(userText) &&
					excerpt != firstExcerpt &&
					(sawWriting || time.Since(start) > graceBeforeDone)
				if done {
					if answer, ok := c.answerFromMessages(ctx, conversationID, userText); ok {
						return answer, nil
					}
					return excerpt, nil
				}
			}
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				if lastErr != nil && !IsTimeout(lastErr) {
					return "", lastErr
				}
				return "", &TimeoutError{ConversationID: conversationID, Waited: time.Since(start)}
			}
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// answerFromMessages 取得完整訊息列表，並確認 AI 已經回覆使用者。
func (c *Client) answerFromMessages(ctx context.Context, conversationID, userText string) (string, bool) {
	raw, err := c.Messages(ctx, conversationID)
	if err != nil {
		return "", false
	}
	return ExtractLastAnswer(raw, userText)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, int, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://miniapps.ai")
	req.Header.Set("Referer", "https://miniapps.ai/")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", c.cookieHeader())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("x-csrf-token", c.creds.CSRFToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) cookieHeader() string {
	return fmt.Sprintf("jwt=%s; __Host-miniapps.x-csrf-token=%s", c.creds.JWT, c.creds.CSRFCookie)
}

// Summary 是對話清單中的單筆資料。
type Summary struct {
	ID                 string
	LastMessageExcerpt string
	Writing            bool
}

type chatPayload struct {
	ToolID         string        `json:"toolId"`
	Revision       int           `json:"revision"`
	ModelID        string        `json:"modelId"`
	RequestID      string        `json:"requestId"`
	Elements       []chatElement `json:"elements"`
	Language       string        `json:"language"`
	ConversationID string        `json:"conversationId,omitempty"`
}

type chatElement struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func findSummary(items []Summary, id string) (Summary, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return Summary{}, false
}

func truncate(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// newRequestID 產生一組 v4 UUID 當作 requestId。
func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
