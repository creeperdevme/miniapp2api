package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"miniapp2api/internal/config"
	"miniapp2api/internal/miniapps"
	"miniapp2api/internal/openai"
	"miniapp2api/internal/store"
)

// maxAttempts 是單次 API 請求最多嘗試幾個帳號。
const maxAttempts = 3

// v1Auth 檢查 OpenAI 相容 API 的金鑰。
func (s *Server) v1Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.RequireKey() {
			next(w, r)
			return
		}
		key := bearerToken(r)
		if key == "" {
			writeAPIError(w, http.StatusUnauthorized,
				"缺少 API 金鑰，請在 Authorization 標頭帶上 Bearer <key>。",
				"invalid_request_error", "missing_api_key")
			return
		}
		if !s.cfg.CheckKey(key) {
			writeAPIError(w, http.StatusUnauthorized,
				"API 金鑰錯誤。",
				"invalid_request_error", "invalid_api_key")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := s.cfg.ModelList()
	list := openai.ModelList{Object: "list", Data: make([]openai.ModelInfo, 0, len(models))}
	for _, model := range models {
		list.Data = append(list.Data, modelInfo(model))
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, model := range s.cfg.ModelList() {
		if strings.EqualFold(model.ID, id) {
			writeJSON(w, http.StatusOK, modelInfo(model))
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, fmt.Sprintf("找不到模型 %q。", id), "invalid_request_error", "model_not_found")
}

func modelInfo(model config.Model) openai.ModelInfo {
	return openai.ModelInfo{
		ID:      model.ID,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "miniapps",
	}
}

// handleChatCompletions 是 /v1/chat/completions 的主要進入點。
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var request openai.ChatRequest
	if err := decodeJSON(r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_body")
		return
	}
	if len(request.Messages) == 0 && strings.TrimSpace(request.Prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "缺少 messages 欄位。", "invalid_request_error", "missing_messages")
		return
	}

	prompt := request.BuildPrompt()
	if prompt == "" {
		writeAPIError(w, http.StatusBadRequest, "messages 中沒有可送出的文字內容。", "invalid_request_error", "empty_prompt")
		return
	}

	model := s.cfg.FindModel(request.Model)
	completionID := "chatcmpl-" + store.RandomHex(12)
	created := time.Now().Unix()

	if request.Stream {
		s.streamCompletion(w, r, completionID, created, model, prompt, request.IncludeUsage())
		return
	}

	answer, account, err := s.complete(r.Context(), model, prompt)
	if err != nil {
		status, kind, code := upstreamError(err)
		s.log.Printf("請求失敗：%v", err)
		writeAPIError(w, status, err.Error(), kind, code)
		return
	}
	_ = account

	usage := buildUsage(prompt, answer)
	writeJSON(w, http.StatusOK, openai.ChatResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: created,
		Model:   model.ID,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      openai.ResponseMessage{Role: "assistant", Content: answer},
			FinishReason: "stop",
		}},
		Usage: usage,
	})
}

// streamCompletion 以 SSE 形式回傳結果。
//
// 上游是「送訊息後輪詢」的模型，無法逐字串流，
// 因此這裡先送出角色事件與 keep-alive 註解，等取得完整回覆後再分段送出。
func (s *Server) streamCompletion(
	w http.ResponseWriter,
	r *http.Request,
	completionID string,
	created int64,
	model config.Model,
	prompt string,
	includeUsage bool,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "此連線不支援串流回應。", "server_error", "stream_unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sendChunk := func(chunk openai.Chunk) bool {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	base := func(choices []openai.ChunkChoice) openai.Chunk {
		return openai.Chunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model.ID,
			Choices: choices,
		}
	}

	if !sendChunk(base([]openai.ChunkChoice{{Index: 0, Delta: openai.Delta{Role: "assistant"}}})) {
		return
	}

	type result struct {
		answer string
		err    error
	}
	results := make(chan result, 1)
	go func() {
		answer, _, err := s.complete(r.Context(), model, prompt)
		results <- result{answer: answer, err: err}
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var outcome result
	waiting := true
	for waiting {
		select {
		case outcome = <-results:
			waiting = false
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}

	if outcome.err != nil {
		s.log.Printf("串流請求失敗：%v", outcome.err)
		_, kind, code := upstreamError(outcome.err)
		payload, _ := json.Marshal(openai.ErrorResponse{Error: openai.ErrorDetail{
			Message: outcome.err.Error(),
			Type:    kind,
			Code:    code,
		}})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	for _, part := range splitForStream(outcome.answer) {
		if !sendChunk(base([]openai.ChunkChoice{{Index: 0, Delta: openai.Delta{Content: part}}})) {
			return
		}
	}

	stop := "stop"
	sendChunk(base([]openai.ChunkChoice{{Index: 0, FinishReason: &stop}}))

	if includeUsage {
		usage := buildUsage(prompt, outcome.answer)
		chunk := base([]openai.ChunkChoice{})
		chunk.Usage = &usage
		sendChunk(chunk)
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// complete 依序挑選帳號送出提示，失敗時自動換下一個帳號。
func (s *Server) complete(ctx context.Context, model config.Model, prompt string) (string, store.Account, error) {
	excluded := map[string]bool{}
	attempts := maxAttempts
	if total := s.pool.Count(); total > 0 && total < attempts {
		attempts = total
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		account, err := s.pool.Pick(excluded, model.ID)
		if err != nil {
			lastErr = err
			break
		}
		excluded[account.ID] = true

		client := miniapps.New(credentialsFor(account, model))
		answer, conversationID, err := client.Ask(ctx, prompt, s.cfg.Timeout())
		s.pool.Record(account.ID, err, cooldownFor(err))
		switch {
		case err == nil:
			s.pool.ClearQuotaExceeded(account.ID, model.ID)
		case miniapps.IsQuotaError(err):
			s.pool.MarkQuotaExceeded(account.ID, model.ID)
		}

		if err != nil {
			lastErr = err
			s.log.Printf("帳號 %s 失敗：%v", account.DisplayName(), err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if strings.TrimSpace(answer) == "" {
			lastErr = errors.New("上游回覆為空")
			s.log.Printf("帳號 %s 回覆為空（對話 %s）", account.DisplayName(), conversationID)
			continue
		}

		s.log.Printf("帳號 %s 完成（對話 %s，%d 字）", account.DisplayName(), conversationID, len([]rune(answer)))
		return answer, account, nil
	}

	if lastErr == nil {
		lastErr = store.ErrNoAccount
	}
	return "", store.Account{}, lastErr
}

// cooldownFor 決定這次錯誤要讓帳號冷卻多久。
//
// 額度不足（402／412）通常代表「這個帳號沒額度」而不是帳號失效，
// 若讓帳號冷卻會連帶讓其他模型一起不能用，因此不冷卻；
// 改由 MarkQuotaExceeded 把它排到候選順位的最後。
func cooldownFor(err error) time.Duration {
	if err == nil || miniapps.IsQuotaError(err) {
		return 0
	}
	return store.CooldownDuration
}

func buildUsage(prompt, answer string) openai.Usage {
	promptTokens := openai.EstimateTokens(prompt)
	completionTokens := openai.EstimateTokens(answer)
	return openai.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

// splitForStream 把完整回覆切成數段，讓客戶端有逐段輸出的效果。
func splitForStream(answer string) []string {
	const chunkSize = 24

	runes := []rune(answer)
	if len(runes) == 0 {
		return nil
	}
	parts := make([]string, 0, len(runes)/chunkSize+1)
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}

func upstreamError(err error) (int, string, string) {
	var timeoutErr *miniapps.TimeoutError
	switch {
	case errors.As(err, &timeoutErr):
		return http.StatusGatewayTimeout, "server_error", "upstream_timeout"
	case errors.Is(err, store.ErrNoAccount):
		return http.StatusServiceUnavailable, "server_error", "no_available_account"
	case miniapps.IsQuotaError(err):
		return http.StatusPaymentRequired, "insufficient_quota", "insufficient_credits"
	case miniapps.IsRateLimited(err):
		return http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limited"
	case miniapps.IsAuthError(err):
		return http.StatusBadGateway, "server_error", "upstream_unauthorized"
	default:
		return http.StatusBadGateway, "server_error", "upstream_error"
	}
}
