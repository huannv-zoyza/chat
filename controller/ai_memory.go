package controller

// Trí nhớ của Trợ lý AI theo kênh: cả kênh là một "phiên" chung. Mỗi lần hỏi, AI đọc bản tóm tắt
// các trao đổi trước (ai là ai, dự án, quyết định, việc còn dở…) + các tin gần nhất nguyên văn.
// Khi số tin chưa được tóm tắt nhiều lên, sau khi trả lời sẽ gộp (compact) các tin cũ vào bản tóm tắt.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"chat/ai"
	"chat/model"
	"chat/store"
)

const (
	aiMemoryChars    = 2000                // độ dài tối đa của bản tóm tắt (vừa hạn mức token của gói miễn phí)
	aiMemoryWindow   = 40                  // tin gần nhất tối đa đọc nguyên văn khi đã có trí nhớ
	aiCompactAfter   = 15                  // số tin dòng chính chưa nhớ (ngoài 25 tin gần nhất) thì gộp
	aiCompactChunk   = 9000                // ký tự tin nhắn mỗi lần gộp
	aiCompactRounds  = 2                   // số lần gộp tối đa mỗi lượt (kênh cũ gộp dần qua nhiều lượt)
	aiMemoryLookback = 14 * 24 * time.Hour // kênh chưa có trí nhớ: chỉ nhớ từ 2 tuần gần đây
)

var compacting sync.Map // channelID -> struct{}: đang gộp, lượt khác bỏ qua

// recentWithMemory: tin dòng chính gần nhất cần đọc nguyên văn. Có trí nhớ thì đọc mọi tin sau phần đã
// nhớ (ít nhất aiAgentRecent, nhiều nhất aiMemoryWindow) để không hổng đoạn nào.
func (a *API) recentWithMemory(ctx context.Context, ch model.Channel, beforeID int64, mem int64) []model.Message {
	list, _, err := a.store.ListMessages(ctx, ch.ID, beforeID, aiMemoryWindow)
	if err != nil {
		return nil
	}
	keep := len(list) - aiAgentRecent // chỉ số đầu của 25 tin gần nhất
	if keep < 0 {
		keep = 0
	}
	start := keep
	for start > 0 && list[start-1].ID > mem && mem > 0 {
		start--
	}
	return list[start:]
}

// memorySection: phần trí nhớ trong lời dặn ("" nếu chưa có).
func memorySection(m store.AIMemory) string {
	if m.Summary == "" {
		return ""
	}
	return fmt.Sprintf("\n\n## Trí nhớ của kênh (tóm tắt các trao đổi trước, cập nhật lúc %s)\n%s", m.UpdatedAt.Local().Format("15:04 02/01"), m.Summary)
}

func (a *API) loadMemory(ctx context.Context, channelID int64) store.AIMemory {
	m, _ := a.store.AIMemory(ctx, channelID)
	return m
}

// compactMemory gộp các tin cũ (ngoài 25 tin gần nhất) vào trí nhớ của kênh. Chạy nền sau khi trả lời.
func (a *API) compactMemory(ch model.Channel) {
	if a.ai == nil || a.ai.chat == nil {
		return
	}
	if _, busy := compacting.LoadOrStore(ch.ID, struct{}{}); busy {
		return
	}
	defer compacting.Delete(ch.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for round := 0; round < aiCompactRounds; round++ {
		recent, _, err := a.store.ListMessages(ctx, ch.ID, 0, aiAgentRecent)
		if err != nil || len(recent) < aiAgentRecent {
			return // kênh còn ngắn: 25 tin gần nhất đã đủ
		}
		boundary := recent[0].ID
		mem := a.loadMemory(ctx, ch.ID)
		since := time.Time{}
		if mem.UptoID == 0 {
			since = time.Now().Add(-aiMemoryLookback)
		}
		msgs, err := a.store.MessagesRange(ctx, ch.ID, mem.UptoID, boundary, since, 600)
		if err != nil {
			return
		}
		top := 0
		for _, m := range msgs {
			if m.ParentID == nil {
				top++
			}
		}
		if top < aiCompactAfter {
			return
		}
		// lấy các tin cũ nhất vừa một lần gộp; chép kèm dòng "--- thứ, ngày ---" để model đổi "mai",
		// "thứ 6 này" ra đúng ngày
		caps := map[string]string{}
		for _, m := range msgs {
			for _, at := range m.Attachments {
				if im, ok := imageOf(at); ok {
					if c, _ := a.store.Caption(ctx, im.Key); c != "" {
						caps[im.Key] = c
					}
				}
			}
		}
		size, upto, n := 0, mem.UptoID, 0
		for _, m := range msgs {
			l := len(transcriptLine(m, a.ai.bot.ID, caps, 0))
			if n > 0 && size+l > aiCompactChunk {
				break
			}
			size += l + 1
			upto = m.ID
			n++
		}
		lines := transcript(msgs[:n], a.ai.bot.ID, caps, 0)
		summary, err := a.summarize(ctx, ch, mem.Summary, lines)
		if err != nil {
			log.Printf("ai memory: kênh %d: %v", ch.ID, err)
			return
		}
		if err := a.store.SaveAIMemory(ctx, ch.ID, summary, upto); err != nil {
			return
		}
		log.Printf("ai memory: kênh %d nhớ thêm %d tin (tới #%d), %d ký tự", ch.ID, n, upto, len([]rune(summary)))
	}
}

func (a *API) summarize(ctx context.Context, ch model.Channel, old string, lines []string) (string, error) {
	if len(lines) == 0 {
		return old, nil
	}
	sys := fmt.Sprintf(`Bạn giữ "trí nhớ dài hạn" của Trợ lý AI cho kênh chat nhóm #%s (nhóm làm việc nội bộ).
Cập nhật trí nhớ hiện có bằng các tin nhắn mới. Viết tiếng Việt, gạch đầu dòng ngắn, tối đa %d ký tự, theo các mục (bỏ mục trống):
**Thành viên**: ai là ai, vai trò, đang phụ trách gì.
**Dự án / chủ đề**: đang làm gì, tiến độ.
**Đã chốt**: quyết định, kèm ngày.
**Việc còn dở**: việc gì, ai làm, hạn.
**Thông tin hữu ích**: link, môi trường, quy ước, số liệu hay được hỏi lại.
Giữ thông tin còn giá trị từ trí nhớ cũ, thông tin mới mâu thuẫn thì theo cái mới, bỏ việc đã xong lâu và chuyện phiếm.
Ngày giờ: mỗi ngày tin nhắn có dòng "--- thứ, ngày ---". Đổi mốc tương đối ("mai", "thứ 6 này", "tuần sau") ra ngày cụ thể
tính từ ngày của chính tin đó, ghi dạng "thứ Sáu 20/11". Hôm nay là %s.
Chỉ ghi điều có trong tin nhắn, không bịa. Chỉ trả về nội dung trí nhớ.`, ch.Name, aiMemoryChars, vnDate(time.Now()))
	user := "## Trí nhớ hiện có:\n"
	if old == "" {
		user += "(chưa có)"
	} else {
		user += old
	}
	user += "\n\n## Tin nhắn mới (cũ đến mới):\n" + strings.Join(lines, "\n")
	out, err := a.ai.chat.Complete(ctx, []ai.Message{{Role: "system", Content: sys}, {Role: "user", Content: user}})
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("tóm tắt rỗng")
	}
	if r := []rune(out); len(r) > aiMemoryChars*5/4 {
		out = string(r[:aiMemoryChars*5/4]) + "…"
	}
	return out, nil
}
