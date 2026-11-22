package muse

// Gọi công cụ kiểu OpenAI trên nền Muse.
//
// Muse không nhận trường `tools`, nhưng nó tuân thủ rất tốt lệnh "chỉ trả lời bằng JSON" —
// chính bước lập kế hoạch của go-chat đã dùng cách đó và luôn nhận về JSON sạch. Ở đây mô tả
// công cụ được dịch thành một đoạn prompt, câu trả lời được đọc ngược lại thành tool_calls.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// openaiTool là một công cụ theo khai báo của OpenAI.
type openaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// toolCall là lệnh gọi công cụ đã đọc được từ câu trả lời của Muse.
type toolCall struct {
	Name string
	Args string // JSON thô của tham số
}

// toolPrompt dựng phần mô tả công cụ chèn vào cuối lượt gửi.
func toolPrompt(tools []openaiTool) string {
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## Công cụ dùng được\n")
	b.WriteString("Cần dùng một công cụ thì trả lời ĐÚNG một đối tượng JSON, không thêm bất kỳ chữ nào trước hay sau nó, không bọc trong khối code:\n")
	b.WriteString(`{"tool": "<tên công cụ>", "args": {<tham số>}}` + "\n")
	b.WriteString("Không cần công cụ nào thì trả lời bình thường bằng lời.\n")
	b.WriteString("Mỗi lượt gọi tối đa một công cụ. Đã có kết quả rồi thì dùng nó để trả lời, đừng gọi lại cùng công cụ với cùng tham số.\n\n")
	for _, t := range tools {
		f := t.Function
		if f.Name == "" {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", f.Name, strings.TrimSpace(f.Description))
		if len(f.Parameters) > 0 {
			if p, err := json.Marshal(f.Parameters); err == nil && len(p) < 4000 {
				fmt.Fprintf(&b, "  tham số: %s\n", p)
			}
		}
	}
	return b.String()
}

// stripFence bỏ khối ```json … ``` nếu model lỡ bọc câu trả lời.
func stripFence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	t = strings.TrimPrefix(t, "```")
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		if first := strings.TrimSpace(t[:i]); first == "" || !strings.ContainsAny(first, "{} ") {
			t = t[i+1:] // bỏ nhãn ngôn ngữ (```json)
		}
	}
	if i := strings.LastIndex(t, "```"); i >= 0 {
		t = t[:i]
	}
	return strings.TrimSpace(t)
}

// parseToolCall đọc câu trả lời của Muse thành một lệnh gọi công cụ.
// Chỉ nhận khi toàn bộ câu trả lời là JSON đúng khuôn và tên công cụ nằm trong danh sách đã khai —
// nhờ vậy một câu văn có nhắc tới tên công cụ không bị hiểu nhầm thành lệnh gọi.
func parseToolCall(answer string, tools []openaiTool) (toolCall, bool) {
	t := stripFence(answer)
	if !strings.HasPrefix(t, "{") || !strings.HasSuffix(t, "}") {
		return toolCall{}, false
	}
	var v struct {
		Tool string          `json:"tool"`
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
		Arg2 json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return toolCall{}, false
	}
	name := v.Tool
	if name == "" {
		name = v.Name
	}
	if name == "" {
		return toolCall{}, false
	}
	known := false
	for _, x := range tools {
		if x.Function.Name == name {
			known = true
			break
		}
	}
	if !known {
		return toolCall{}, false
	}
	args := v.Args
	if len(args) == 0 {
		args = v.Arg2
	}
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	return toolCall{Name: name, Args: string(args)}, true
}
