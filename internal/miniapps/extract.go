package miniapps

import (
	"encoding/json"
	"sort"
	"strings"
)

// messageKeys 是可能裝著訊息陣列的欄位名稱（依優先順序）。
var messageKeys = []string{"messages", "items", "history", "turns", "chats", "entries"}

// ExtractAnswer 從完整對話 JSON 中取出最後一則 AI 回覆。
//
// 上游的欄位結構可能會變動，因此這裡採用寬鬆的解析方式：
// 找出訊息陣列、逐則取出文字，略過使用者自己送出的內容，
// 最後以「AI 角色」的訊息優先。
func ExtractAnswer(raw []byte, userText string) string {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}

	messages := findMessageList(root)
	if len(messages) == 0 {
		return ""
	}

	want := strings.TrimSpace(userText)
	if !sortByIndex(messages) && len(messages) > 1 && messageText(messages[len(messages)-1]) == want {
		// 陣列是新的在前，翻轉成時間順序。
		reversed := make([]map[string]any, len(messages))
		for i, msg := range messages {
			reversed[len(messages)-1-i] = msg
		}
		messages = reversed
	}

	var (
		lastAnyIndex       = -1
		lastAnyText        string
		lastAssistantIndex = -1
		lastAssistantText  string
	)

	for index, msg := range messages {
		text := strings.TrimSpace(messageText(msg))
		if text == "" || text == want {
			continue
		}
		role := messageRole(msg)
		if strings.Contains(role, "user") || strings.Contains(role, "human") {
			continue
		}
		if isAssistantRole(role) {
			lastAssistantIndex, lastAssistantText = index, text
			continue
		}
		lastAnyIndex, lastAnyText = index, text
	}

	if lastAssistantIndex >= lastAnyIndex && lastAssistantText != "" {
		return lastAssistantText
	}
	return lastAnyText
}

func isAssistantRole(role string) bool {
	for _, keyword := range []string{"assistant", "ai", "bot", "model", "agent", "gpt"} {
		if strings.Contains(role, keyword) {
			return true
		}
	}
	return false
}

func isUserRole(role string) bool {
	return strings.Contains(role, "user") || strings.Contains(role, "human")
}

// ExtractLastAnswer 從訊息列表中，取出使用者訊息之後的最後一則 AI 回覆。
//
// 這是主要解析路徑：先依 index 排序，再以使用者訊息為基準往後找，
// 因此不會誤把對話開頭的招呼語當成答案。缺少 index 時退回 ExtractAnswer。
func ExtractLastAnswer(raw []byte, userText string) (string, bool) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return "", false
	}

	messages := findMessageList(root)
	if len(messages) == 0 {
		return "", false
	}

	want := strings.TrimSpace(userText)
	if !sortByIndex(messages) {
		answer := ExtractAnswer(raw, userText)
		if answer == "" || answer == want {
			return "", false
		}
		return answer, true
	}

	userIndex := -1
	for index, msg := range messages {
		if strings.TrimSpace(messageText(msg)) == want && isUserRole(messageRole(msg)) {
			userIndex = index
		}
	}
	if userIndex < 0 {
		return "", false
	}

	for index := len(messages) - 1; index > userIndex; index-- {
		text := strings.TrimSpace(messageText(messages[index]))
		if text == "" || text == want || isUserRole(messageRole(messages[index])) {
			continue
		}
		return text, true
	}
	return "", false
}

// findMessageList 遞迴尋找最像「訊息陣列」的欄位。
func findMessageList(node any) []map[string]any {
	if messages := searchMessages(node, messageKeys, 0); len(messages) > 0 {
		return messages
	}
	return searchMessages(node, nil, 0)
}

func searchMessages(node any, keys []string, depth int) []map[string]any {
	if depth > 6 {
		return nil
	}
	switch typed := node.(type) {
	case map[string]any:
		if len(keys) > 0 {
			for _, key := range keys {
				if messages := toMessageList(typed[key]); len(messages) > 0 {
					return messages
				}
			}
		}
		for _, value := range typed {
			if messages := searchMessages(value, keys, depth+1); len(messages) > 0 {
				return messages
			}
		}
	case []any:
		for _, value := range typed {
			if messages := searchMessages(value, keys, depth+1); len(messages) > 0 {
				return messages
			}
		}
	}
	return nil
}

func toMessageList(value any) []map[string]any {
	array, ok := value.([]any)
	if !ok || len(array) == 0 {
		return nil
	}
	messages := make([]map[string]any, 0, len(array))
	for _, item := range array {
		object, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		if !looksLikeMessage(object) {
			return nil
		}
		messages = append(messages, object)
	}
	return messages
}

func looksLikeMessage(object map[string]any) bool {
	for _, key := range []string{"elements", "text", "content", "role", "sender", "actorId", "senderId", "createdAt", "type"} {
		if _, ok := object[key]; ok {
			return true
		}
	}
	return false
}

func messageText(msg map[string]any) string {
	for _, key := range []string{"text", "content", "message", "body", "output", "value"} {
		if text := textValue(msg[key]); text != "" {
			return text
		}
	}
	if text := textValue(msg["elements"]); text != "" {
		return text
	}
	for _, key := range []string{"data", "payload", "message"} {
		if inner, ok := msg[key].(map[string]any); ok {
			if text := messageText(inner); text != "" {
				return text
			}
		}
	}
	return ""
}

func textValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		return joinTexts(typed)
	case map[string]any:
		for _, key := range []string{"text", "content", "value"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func joinTexts(parts []any) string {
	collected := make([]string, 0, len(parts))
	for _, part := range parts {
		switch typed := part.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				collected = append(collected, text)
			}
		case map[string]any:
			kind, _ := typed["type"].(string)
			kind = strings.ToLower(kind)
			if kind != "" && !strings.Contains(kind, "text") {
				continue
			}
			if text := textValue(typed); text != "" {
				collected = append(collected, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

func messageRole(msg map[string]any) string {
	for _, key := range []string{"origin", "role", "sender", "senderType", "actorType", "author", "actor", "from", "type"} {
		if role := roleValue(msg[key]); role != "" {
			return role
		}
	}
	if inner, ok := msg["data"].(map[string]any); ok {
		return messageRole(inner)
	}
	return ""
}

func roleValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(typed))
	case map[string]any:
		for _, key := range []string{"role", "type", "kind", "id", "name", "slug"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return strings.ToLower(strings.TrimSpace(text))
			}
		}
	}
	return ""
}

// sortByIndex 依訊息上的 index 欄位由小到大排序，成功時回傳 true。
func sortByIndex(messages []map[string]any) bool {
	for _, msg := range messages {
		if _, ok := messageIndex(msg); !ok {
			return false
		}
	}
	sort.SliceStable(messages, func(i, j int) bool {
		left, _ := messageIndex(messages[i])
		right, _ := messageIndex(messages[j])
		return left < right
	})
	return true
}

func messageIndex(msg map[string]any) (int, bool) {
	value, ok := msg["index"].(float64)
	if !ok {
		return 0, false
	}
	return int(value), true
}

// findString 遞迴尋找指定鍵的字串值。
func findString(raw []byte, key string) string {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	return searchString(root, key, 0)
}

func searchString(node any, key string, depth int) string {
	if depth > 6 {
		return ""
	}
	switch typed := node.(type) {
	case map[string]any:
		if value, ok := typed[key].(string); ok && value != "" {
			return value
		}
		for _, value := range typed {
			if found := searchString(value, key, depth+1); found != "" {
				return found
			}
		}
	case []any:
		for _, value := range typed {
			if found := searchString(value, key, depth+1); found != "" {
				return found
			}
		}
	}
	return ""
}

// findArray 遞迴尋找指定鍵的陣列值。
func findArray(raw []byte, key string) []any {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	return searchArray(root, key, 0)
}

func searchArray(node any, key string, depth int) []any {
	if depth > 6 {
		return nil
	}
	switch typed := node.(type) {
	case map[string]any:
		if value, ok := typed[key].([]any); ok {
			return value
		}
		for _, value := range typed {
			if found := searchArray(value, key, depth+1); found != nil {
				return found
			}
		}
	case []any:
		for _, value := range typed {
			if found := searchArray(value, key, depth+1); found != nil {
				return found
			}
		}
	}
	return nil
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

// isWriting 判斷對話中的 AI 是否正在輸出。
func isWriting(item map[string]any) bool {
	for _, actor := range actors(item) {
		if writing, ok := actor["isWriting"].(bool); ok && writing {
			return true
		}
		if phase, ok := actor["phase"].(string); ok && strings.EqualFold(phase, "streaming") {
			return true
		}
	}
	return false
}

func actors(item map[string]any) []map[string]any {
	paths := [][]string{
		{"data", "status", "actors"},
		{"status", "actors"},
		{"actors"},
		{"data", "actors"},
	}
	for _, path := range paths {
		node := any(item)
		for _, key := range path {
			object, ok := node.(map[string]any)
			if !ok {
				node = nil
				break
			}
			node = object[key]
		}
		if node == nil {
			continue
		}
		if result := toActorList(node); len(result) > 0 {
			return result
		}
	}
	return nil
}

func toActorList(node any) []map[string]any {
	switch typed := node.(type) {
	case map[string]any:
		list := make([]map[string]any, 0, len(typed))
		for _, value := range typed {
			if object, ok := value.(map[string]any); ok {
				list = append(list, object)
			}
		}
		return list
	case []any:
		list := make([]map[string]any, 0, len(typed))
		for _, value := range typed {
			if object, ok := value.(map[string]any); ok {
				list = append(list, object)
			}
		}
		return list
	}
	return nil
}
