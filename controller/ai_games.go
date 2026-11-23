package controller

// Công cụ đọc ván chơi cho Trợ lý AI: danh sách ván và toàn bộ nước đi của một ván, để AI tự
// phân tích chiến thuật, nước đi, phong cách từng người (thay vì chỉ đọc thống kê dựng sẵn).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/game"
	"chat/model"
	"chat/store"
)

const (
	aiGamesScan = 300 // số ván gần nhất quét khi lọc theo người chơi
	aiGamesMax  = 20  // số ván tối đa trả về một lần
	// ván dài hơn chừng này nửa nước thì chỉ giữ phần đầu và phần cuối (đủ cho khai cuộc và tàn cuộc)
	aiMovesMax  = 300
	aiMovesHead = 140
)

// gameSummary: phần cần để liệt kê, đọc nhanh từ state JSON (không dựng lại cả ván).
type gameSummary struct {
	Players []game.Player     `json:"players"`
	Winners []int64           `json:"winners"`
	Result  string            `json:"result"`
	History []json.RawMessage `json:"history"`
	Order   []json.RawMessage `json:"order"`
	Rounds  []json.RawMessage `json:"rounds"`
	Moves   int               `json:"moves"`
	Log     []string          `json:"log"`
}

func (g gameSummary) has(id int64) bool {
	for _, p := range g.Players {
		if p.ID == id {
			return true
		}
	}
	return false
}

func (g gameSummary) moveCount(kind string) string {
	switch {
	case len(g.History) > 0:
		return fmt.Sprintf("%d nước", (len(g.History)+1)/2)
	case len(g.Order) > 0:
		return fmt.Sprintf("%d nước", len(g.Order))
	case kind == "caro" && g.Moves > 0:
		return fmt.Sprintf("%d nước", g.Moves)
	case len(g.Rounds) > 0:
		return fmt.Sprintf("%d ván", len(g.Rounds))
	}
	return ""
}

var gameKinds = []string{"xiangqi", "chess", "caro", "rps", "werewolf", "spy", "station"}

func normKind(k string) (string, error) {
	k = strings.ToLower(strings.TrimSpace(k))
	switch k {
	case "", "all", "any":
		return "", nil
	case "cờ tướng", "co tuong":
		return "xiangqi", nil
	case "cờ vua", "co vua":
		return "chess", nil
	}
	for _, g := range gameKinds {
		if g == k {
			return k, nil
		}
	}
	return "", fmt.Errorf("không có loại game %q (dùng: %s)", k, strings.Join(gameKinds, ", "))
}

// listGames: các ván gần nhất, lọc theo người chơi / đối thủ / loại / trạng thái.
func (r *agentRun) listGames(ctx context.Context, player, opponent, kind, status string, limit float64) string {
	r.status("Đang tìm các ván đã chơi…")
	k, err := normKind(kind)
	if err != nil {
		return "Lỗi: " + err.Error()
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "ended" && status != "playing" {
		status = ""
	}
	var pl, op *model.PublicUser
	if strings.TrimSpace(player) != "" {
		if pl, err = r.findMember(ctx, player); err != nil {
			return err.Error()
		}
	}
	if strings.TrimSpace(opponent) != "" {
		if op, err = r.findMember(ctx, opponent); err != nil {
			return err.Error()
		}
	}
	n := int(limit)
	if n <= 0 {
		n = 10
	}
	n = min(n, aiGamesMax)

	rows, err := r.a.store.VisibleGames(ctx, r.ch.ID, k, status, aiGamesScan)
	if err != nil {
		return "Lỗi khi đọc danh sách ván."
	}
	var lines []string
	for _, g := range rows {
		var sm gameSummary
		if json.Unmarshal(g.State, &sm) != nil {
			continue
		}
		if (pl != nil && !sm.has(pl.ID)) || (op != nil && !sm.has(op.ID)) {
			continue
		}
		lines = append(lines, r.gameLine(g, sm))
		if len(lines) >= n {
			break
		}
	}
	if len(lines) == 0 {
		return "Không tìm thấy ván nào khớp (chỉ xem được ván trong kênh này và các kênh công khai)."
	}
	return fmt.Sprintf("%d ván (mới nhất trước). Dùng get_game_moves với số #id để xem từng nước:\n%s",
		len(lines), strings.Join(lines, "\n"))
}

func (r *agentRun) gameLine(g store.GameListRow, sm gameSummary) string {
	names := make([]string, len(sm.Players))
	for i, p := range sm.Players {
		names[i] = p.Name
	}
	title := game.Title(g.Kind)
	if title == "" {
		title = g.Kind
	}
	parts := []string{fmt.Sprintf("#%d %s: %s", g.MessageID, title, strings.Join(names, " vs "))}
	switch {
	case g.Status != "ended":
		parts = append(parts, "đang chơi")
	case sm.Result != "":
		parts = append(parts, sm.Result)
	default:
		parts = append(parts, "đã kết thúc")
	}
	if mc := sm.moveCount(g.Kind); mc != "" {
		parts = append(parts, mc)
	}
	where := "kênh này"
	if g.ChannelID != r.ch.ID {
		where = "#" + g.ChannelName
	}
	parts = append(parts, where, g.UpdatedAt.In(aiLocation()).Format("02/01/2006 15:04"))
	return strings.Join(parts, " · ")
}

// gameMoves: toàn bộ diễn biến một ván (nước đi theo thứ tự, thế cờ cuối, kết quả).
func (r *agentRun) gameMoves(ctx context.Context, id float64) string {
	r.status("Đang đọc lại từng nước đi…")
	if id <= 0 {
		return "Lỗi: cần game_id (số #id của tin chứa ván, lấy từ list_games)."
	}
	row, err := r.a.store.GetGame(ctx, int64(id))
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Sprintf("Không có ván #%d.", int64(id))
	}
	if err != nil {
		return "Lỗi khi đọc ván."
	}
	if row.ChannelID != r.ch.ID {
		ch, err := r.a.store.GetChannel(ctx, row.ChannelID)
		if err != nil || ch.IsDM() || ch.IsPrivate {
			return "Ván này ở kênh riêng tư hoặc tin nhắn riêng, không xem được từ đây."
		}
	}
	rec, err := game.Describe(row.Kind, row.State)
	if err != nil {
		return "Lỗi: không đọc được ván này."
	}
	return formatRecord(int64(id), rec)
}

func formatRecord(id int64, rec *game.Record) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Ván #%d — %s", id, rec.Title)
	switch rec.Status {
	case game.Ended:
		sb.WriteString(" (đã kết thúc)")
	case game.Playing:
		sb.WriteString(" (đang chơi)")
	default:
		sb.WriteString(" (đang chờ người chơi)")
	}
	sb.WriteString("\nNgười chơi: ")
	for i, p := range rec.Players {
		if i > 0 {
			sb.WriteString(" vs ")
		}
		sb.WriteString(p.Name)
		if i < 2 && rec.Sides[i] != "" {
			fmt.Fprintf(&sb, " (%s)", rec.Sides[i])
		}
	}
	if rec.Result != "" {
		fmt.Fprintf(&sb, "\nKết quả: %s", rec.Result)
	}
	sb.WriteByte('\n')

	switch {
	case rec.NoOrder:
		sb.WriteString("\nVán này lưu từ trước khi có ghi thứ tự nước đi: chỉ biết vị trí các quân lúc cuối, không biết nước nào trước.\n")
	case rec.FromLog:
		sb.WriteString("\nLưu ý: ván cũ, nước đi đọc lại từ nhật ký (giữ tối đa 200 dòng cuối) nên có thể thiếu phần đầu; không có tên quân.\n")
	}

	if len(rec.Moves) > 0 {
		sb.WriteString("\nDiễn biến:\n")
		writeMoves(&sb, rec)
	}
	for _, n := range rec.Notes {
		fmt.Fprintf(&sb, "%s\n", n)
	}
	if rec.Board != "" {
		label := "Thế cờ cuối ván"
		if rec.Status != game.Ended {
			label = "Thế cờ hiện tại"
		}
		fmt.Fprintf(&sb, "\n%s:\n%s", label, rec.Board)
	}
	// diễn biến khác ngoài nước đi (xin hoà, hết giờ, đầu hàng…), vài dòng cuối
	var other []string
	for _, l := range rec.Log {
		if !strings.Contains(l, " → ") {
			other = append(other, l)
		}
	}
	if len(other) > 0 {
		if len(other) > 8 {
			other = other[len(other)-8:]
		}
		sb.WriteString("\nNhật ký khác:\n- " + strings.Join(other, "\n- ") + "\n")
	}
	return sb.String()
}

// writeMoves: mỗi dòng một lượt "N. nước của bên đi trước | nước của bên đi sau".
func writeMoves(sb *strings.Builder, rec *game.Record) {
	moves := rec.Moves
	if moves[0].Side < 0 { // không chia phe (Oẳn tù tì): mỗi dòng một ván
		for _, m := range moves {
			fmt.Fprintf(sb, "%s\n", m.Text)
		}
		return
	}
	skipFrom, skipTo := -1, -1
	if len(moves) > aiMovesMax {
		skipFrom = aiMovesHead
		skipTo = len(moves) - (aiMovesMax - aiMovesHead)
		if skipFrom%2 == 1 {
			skipFrom++
		}
		if skipTo%2 == 1 {
			skipTo--
		}
	}
	for i := 0; i < len(moves); i += 2 {
		if i == skipFrom {
			fmt.Fprintf(sb, "… (lược bớt %d nửa nước ở giữa ván) …\n", skipTo-skipFrom)
			i = skipTo - 2
			continue
		}
		line := fmt.Sprintf("%d. %s: %s", i/2+1, rec.Sides[0], moves[i].Text)
		if i+1 < len(moves) {
			line += fmt.Sprintf(" | %s: %s", rec.Sides[1], moves[i+1].Text)
		}
		sb.WriteString(line + "\n")
	}
}

// aiLocation: giờ Việt Nam cho ngày giờ trong danh sách ván.
func aiLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Ho_Chi_Minh"); err == nil {
		return loc
	}
	return time.FixedZone("ICT", 7*3600)
}
