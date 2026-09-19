package miniapps

import "testing"

// 以下 payload 取自真實的 api.miniapps.ai 回應。
const messagesPayload = `{
  "items": [
    {"id": "1", "index": 1, "text": "", "origin": "assistant",
     "elements": [{"text": "您好，有什么我可以帮您的吗？", "type": "text"}],
     "modelId": null, "modelTitle": null},
    {"id": "2", "index": 2, "text": "", "origin": "user",
     "elements": [{"text": "請只回答四個字：測試成功", "type": "text"}],
     "modelId": "f57145fe-a761-4ac4-9cc5-676ac291c433"},
    {"id": "3", "index": 3, "text": "", "origin": "assistant",
     "elements": [{"text": "測試成功", "type": "text"}],
     "modelId": "f57145fe-a761-4ac4-9cc5-676ac291c433", "modelTitle": "GPT 6 Astra"}
  ],
  "hasMore": false,
  "nextBeforeMessageId": null
}`

func TestExtractLastAnswer(t *testing.T) {
	answer, ok := ExtractLastAnswer([]byte(messagesPayload), "請只回答四個字：測試成功")
	if !ok {
		t.Fatalf("應該要找到答案")
	}
	if answer != "測試成功" {
		t.Fatalf("答案錯誤：%q", answer)
	}
}

func TestExtractLastAnswerIgnoresGreeting(t *testing.T) {
	// 只有招呼語、使用者訊息尚未出現時，不應該把招呼語當成答案。
	payload := `{"items":[{"id":"1","index":1,"origin":"assistant","elements":[{"text":"您好，有什么我可以帮您的吗？","type":"text"}]}]}`
	if answer, ok := ExtractLastAnswer([]byte(payload), "哈囉"); ok {
		t.Fatalf("不應該回傳答案，卻得到 %q", answer)
	}
}

func TestExtractLastAnswerShuffledIndexes(t *testing.T) {
	// 就算伺服器回傳的順序是亂的，也應該依 index 還原。
	payload := `{"items":[
	  {"index":3,"origin":"assistant","elements":[{"text":"第二個回覆","type":"text"}]},
	  {"index":1,"origin":"assistant","elements":[{"text":"招呼語","type":"text"}]},
	  {"index":2,"origin":"user","elements":[{"text":"問題一","type":"text"}]}
	]}`
	answer, ok := ExtractLastAnswer([]byte(payload), "問題一")
	if !ok || answer != "第二個回覆" {
		t.Fatalf("得到 %q（ok=%v）", answer, ok)
	}
}

func TestExtractAnswerFallback(t *testing.T) {
	// 沒有 index 時，退化成「最後一則不是使用者的訊息」。
	payload := `{"messages":[
	  {"role":"user","content":"嗨"},
	  {"role":"assistant","content":"哈囉，需要什麼協助？"}
	]}`
	if answer := ExtractAnswer([]byte(payload), "嗨"); answer != "哈囉，需要什麼協助？" {
		t.Fatalf("得到 %q", answer)
	}
}

func TestExtractAnswerContentParts(t *testing.T) {
	payload := `{"messages":[
	  {"role":"user","content":"問題"},
	  {"role":"assistant","content":[{"type":"text","text":"第一段"},{"type":"text","text":"第二段"}]}
	]}`
	if answer := ExtractAnswer([]byte(payload), "問題"); answer != "第一段\n第二段" {
		t.Fatalf("得到 %q", answer)
	}
}

func TestIsWriting(t *testing.T) {
	writing := map[string]any{
		"data": map[string]any{
			"status": map[string]any{
				"actors": map[string]any{
					"model": map[string]any{"phase": "streaming", "isWriting": true},
				},
			},
		},
	}
	if !isWriting(writing) {
		t.Fatal("應該判定為正在輸出")
	}

	idle := map[string]any{
		"data": map[string]any{
			"status": map[string]any{
				"actors": map[string]any{
					"model": map[string]any{"phase": "idle", "isWriting": false},
				},
			},
		},
	}
	if isWriting(idle) {
		t.Fatal("應該判定為閒置")
	}
}

func TestFindStringAndArray(t *testing.T) {
	raw := []byte(`{"data":{"conversationId":"abc-123"},"items":[{"id":"x"}]}`)
	if got := findString(raw, "conversationId"); got != "abc-123" {
		t.Fatalf("findString 得到 %q", got)
	}
	if got := findArray(raw, "items"); len(got) != 1 {
		t.Fatalf("findArray 得到 %v", got)
	}
}
