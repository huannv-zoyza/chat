package game

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Ai là gián điệp: dân thường cùng nhận một từ, gián điệp nhận một từ gần giống (và không biết
// mình là gián điệp). Mỗi vòng, lần lượt từng người mô tả từ của mình bằng một câu ngắn
// (không được nói thẳng từ đó), rồi cả nhóm bỏ phiếu loại một người.
// Dân thắng khi loại hết gián điệp; gián điệp thắng khi số gián điệp >= số dân còn lại.

var spyWords = [][2]string{
	{"Cà phê", "Trà sữa"}, {"Phở", "Bún bò"}, {"Bánh chưng", "Bánh tét"}, {"Tết", "Trung thu"},
	{"Hà Nội", "Sài Gòn"}, {"Đà Lạt", "Sa Pa"}, {"Xe máy", "Xe đạp"}, {"Máy bay", "Trực thăng"},
	{"Con chó", "Con mèo"}, {"Con hổ", "Con sư tử"}, {"Con gà", "Con vịt"}, {"Cá heo", "Cá mập"},
	{"Bác sĩ", "Y tá"}, {"Giáo viên", "Gia sư"}, {"Ca sĩ", "Diễn viên"}, {"Cảnh sát", "Bảo vệ"},
	{"Điện thoại", "Máy tính bảng"}, {"Facebook", "Zalo"}, {"YouTube", "TikTok"}, {"Laptop", "Máy tính bàn"},
	{"Bóng đá", "Bóng chuyền"}, {"Cầu lông", "Tennis"}, {"Bơi lội", "Lặn biển"}, {"Cờ tướng", "Cờ vua"},
	{"Biển", "Hồ bơi"}, {"Núi", "Đồi"}, {"Mưa", "Tuyết"}, {"Mặt trời", "Mặt trăng"},
	{"Bánh mì", "Xôi"}, {"Trà đá", "Nước mía"}, {"Bia", "Rượu"}, {"Kem", "Sữa chua"},
	{"Lẩu", "Nướng"}, {"Cơm tấm", "Cơm gà"}, {"Mì tôm", "Phở gói"}, {"Sầu riêng", "Mít"},
	{"Xoài", "Cóc"}, {"Dưa hấu", "Dưa lưới"}, {"Áo dài", "Áo bà ba"}, {"Giày", "Dép"},
	{"Son môi", "Nước hoa"}, {"Kính râm", "Mũ lưỡi trai"}, {"Đám cưới", "Sinh nhật"}, {"Rạp phim", "Karaoke"},
	{"Thư viện", "Hiệu sách"}, {"Bệnh viện", "Nhà thuốc"}, {"Sân bay", "Ga tàu"}, {"Siêu thị", "Chợ"},
	{"Grab", "Taxi"}, {"Shopee", "Lazada"}, {"Ngân hàng", "Ví điện tử"}, {"Deadline", "Tăng ca"},
	{"Họp online", "Họp trực tiếp"}, {"Sếp", "Đồng nghiệp"}, {"Lương", "Thưởng"}, {"Nghỉ phép", "Nghỉ ốm"},
	{"Harry Potter", "Chúa tể những chiếc nhẫn"}, {"Doraemon", "Conan"}, {"Ông già Noel", "Táo quân"}, {"Ma", "Zombie"},
}

const (
	spyTurnTime = 45 * time.Second
	spyVoteTime = 60 * time.Second
	maxClue     = 80
)

type spyClue struct {
	Round  int    `json:"round"`
	Player int64  `json:"player"`
	Text   string `json:"text"`
}

type spyOptions struct {
	// Số gián điệp (0 = tự chọn theo số người).
	Spies int `json:"spies"`
}

type spy struct {
	Base
	Opts      spyOptions      `json:"opts"`
	Words     [2]string       `json:"words"` // [0] dân, [1] gián điệp
	Spies     map[int64]bool  `json:"spies"`
	Out       map[int64]bool  `json:"out"` // đã bị loại
	Phase     string          `json:"phase,omitempty"`
	Round     int             `json:"round,omitempty"`
	Order     []int64         `json:"order,omitempty"`
	Turn      int             `json:"turn,omitempty"`
	Clues     []spyClue       `json:"clues,omitempty"`
	Votes     map[int64]int64 `json:"votes"`
	LastVotes map[int64]int64 `json:"last_votes,omitempty"`
}

func (s *spy) limits() (int, int) { return 4, 12 }
func (s *spy) autoStart() bool    { return false }
func (s *spy) options() any       { return &s.Opts }
func (s *spy) normalize()         { s.Opts.Spies = min(max(s.Opts.Spies, 0), 3) }

func (s *spy) spyCount() int {
	n := len(s.Players)
	if s.Opts.Spies > 0 {
		return min(s.Opts.Spies, (n-1)/2)
	}
	if n >= 8 {
		return 2
	}
	return 1
}

func (s *spy) start(now time.Time) error {
	pair := pick(spyWords)
	if pick([]bool{true, false}) {
		pair[0], pair[1] = pair[1], pair[0]
	}
	s.Words = pair
	ids := make([]int64, len(s.Players))
	for i, p := range s.Players {
		ids[i] = p.ID
	}
	shuffle(ids)
	s.Spies = map[int64]bool{}
	for _, id := range ids[:s.spyCount()] {
		s.Spies[id] = true
	}
	s.Out = map[int64]bool{}
	s.logf("Ván bắt đầu với %d người, trong đó có %d gián điệp. Mỗi người đã nhận một từ.", len(s.Players), s.spyCount())
	s.startRound(now)
	return nil
}

func (s *spy) alive() []int64 {
	var out []int64
	for _, p := range s.Players {
		if !s.Out[p.ID] {
			out = append(out, p.ID)
		}
	}
	return out
}

func (s *spy) startRound(now time.Time) {
	s.Round++
	s.Phase = "describe"
	s.Order = s.alive()
	shuffle(s.Order)
	s.Turn = 0
	s.Votes = nil
	s.setDeadline(now, spyTurnTime)
	s.logf("🗣️ Vòng %d: lần lượt mô tả từ của mình. %s nói trước.", s.Round, s.name(s.Order[0]))
}

func (s *spy) nextTurn(now time.Time) {
	s.Turn++
	if s.Turn < len(s.Order) {
		s.setDeadline(now, spyTurnTime)
		return
	}
	s.Phase = "vote"
	s.Votes = map[int64]int64{}
	s.setDeadline(now, spyVoteTime)
	s.logf("🗳️ Vòng %d: bỏ phiếu loại người bạn nghi là gián điệp.", s.Round)
}

func (s *spy) resolveVote(now time.Time) {
	target, count, _ := tally(s.Votes)
	s.LastVotes = s.Votes
	if target == 0 {
		s.logf("Hoà phiếu, không ai bị loại.")
	} else {
		s.Out[target] = true
		if s.Spies[target] {
			s.logf("🎯 %s bị loại với %d phiếu — đúng là GIÁN ĐIỆP!", s.name(target), count)
		} else {
			s.logf("😢 %s bị loại với %d phiếu — là dân thường.", s.name(target), count)
		}
	}
	spies, civ := 0, 0
	for _, id := range s.alive() {
		if s.Spies[id] {
			spies++
		} else {
			civ++
		}
	}
	if spies == 0 || spies >= civ {
		var winners []int64
		for _, p := range s.Players {
			if s.Spies[p.ID] == (spies > 0) {
				winners = append(winners, p.ID)
			}
		}
		s.Phase = ""
		var names []string
		for _, p := range s.Players {
			if s.Spies[p.ID] {
				names = append(names, p.Name)
			}
		}
		reveal := fmt.Sprintf("Từ của dân: “%s”, của gián điệp: “%s” (%s).", s.Words[0], s.Words[1], strings.Join(names, ", "))
		if spies == 0 {
			s.logf("🎉 Đã tìm ra hết gián điệp. Dân thường thắng! %s", reveal)
			s.end("Dân thường thắng!", winners)
		} else {
			s.logf("🕵️ Gián điệp đã trà trộn thành công. Gián điệp thắng! %s", reveal)
			s.end("Gián điệp thắng!", winners)
		}
		return
	}
	s.startRound(now)
}

type clueData struct {
	Text string `json:"text"`
}

func (s *spy) act(p int64, action string, data json.RawMessage, now time.Time) error {
	if s.Out[p] {
		return Error("Bạn đã bị loại, chỉ được xem.")
	}
	switch action {
	case "clue":
		if s.Phase != "describe" || s.Order[s.Turn] != p {
			return Error("Chưa tới lượt bạn mô tả.")
		}
		var d clueData
		if err := decode(data, &d); err != nil {
			return err
		}
		text := strings.Join(strings.Fields(d.Text), " ")
		if text == "" || utf8.RuneCountInString(text) > maxClue {
			return Error(fmt.Sprintf("Mô tả cần 1–%d ký tự.", maxClue))
		}
		if strings.Contains(fold(text), fold(s.word(p))) {
			return Error("Không được nói thẳng từ của mình.")
		}
		s.Clues = append(s.Clues, spyClue{Round: s.Round, Player: p, Text: text})
		s.nextTurn(now)
	case "vote":
		if s.Phase != "vote" {
			return Error("Chưa tới lúc bỏ phiếu.")
		}
		var d targetData
		if err := decode(data, &d); err != nil {
			return err
		}
		if d.Target != 0 && (d.Target == p || !s.isPlayer(d.Target) || s.Out[d.Target]) {
			return Error("Không bỏ phiếu cho người này được.")
		}
		s.Votes[p] = d.Target
		if len(s.Votes) == len(s.alive()) && s.Deadline != nil && s.Deadline.Sub(now) > voteGrace {
			s.setDeadline(now, voteGrace)
		}
	default:
		return Error("Hành động không hợp lệ.")
	}
	return nil
}

func (s *spy) word(p int64) string {
	if s.Spies[p] {
		return s.Words[1]
	}
	return s.Words[0]
}

func (s *spy) tick(now time.Time) bool {
	switch s.Phase {
	case "describe":
		p := s.Order[s.Turn]
		s.Clues = append(s.Clues, spyClue{Round: s.Round, Player: p, Text: ""})
		s.nextTurn(now)
	case "vote":
		s.resolveVote(now)
	default:
		return false
	}
	return true
}

type spyPublic struct {
	Phase     string          `json:"phase,omitempty"`
	Round     int             `json:"round,omitempty"`
	Order     []int64         `json:"order,omitempty"`
	Speaker   int64           `json:"speaker,omitempty"`
	Clues     []spyClue       `json:"clues"`
	Votes     map[int64]int64 `json:"votes"`
	Out       []int64         `json:"out"`
	Spies     []int64         `json:"spies"` // đã bị loại và là gián điệp, hoặc tất cả khi hết ván
	SpyCount  int             `json:"spy_count"`
	Words     *[2]string      `json:"words,omitempty"` // chỉ khi hết ván
	LastVotes map[int64]int64 `json:"last_votes,omitempty"`
}

type spyMe struct {
	Word string `json:"word,omitempty"`
	Out  bool   `json:"out"`
}

func (s *spy) view(viewer int64) (any, any) {
	pub := spyPublic{Phase: s.Phase, Round: s.Round, Order: s.Order, Clues: s.Clues, Out: []int64{}, LastVotes: s.LastVotes}
	if pub.Clues == nil {
		pub.Clues = []spyClue{}
	}
	if s.Status == Lobby {
		pub.SpyCount = s.spyCount()
		if len(s.Players) < 4 {
			pub.SpyCount = 1
		}
	} else {
		pub.SpyCount = len(s.Spies)
	}
	if s.Phase == "describe" && s.Turn < len(s.Order) {
		pub.Speaker = s.Order[s.Turn]
	}
	if s.Phase == "vote" {
		pub.Votes = s.Votes
	}
	for _, p := range s.Players {
		if s.Out[p.ID] {
			pub.Out = append(pub.Out, p.ID)
		}
		if s.Spies[p.ID] && (s.Out[p.ID] || s.Status == Ended) {
			pub.Spies = append(pub.Spies, p.ID)
		}
	}
	if s.Status == Ended && s.Words[0] != "" {
		w := s.Words
		pub.Words = &w
	}
	if viewer == 0 || s.Spies == nil {
		return pub, nil
	}
	return pub, spyMe{Word: s.word(viewer), Out: s.Out[viewer]}
}

// fold: chữ thường, bỏ dấu tiếng Việt và khoảng trắng để so "nói thẳng từ".
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r == ' ' || r == '-' {
			continue
		}
		if base, ok := vnBase[r]; ok {
			r = base
		}
		b.WriteRune(r)
	}
	return b.String()
}

var vnBase = func() map[rune]rune {
	groups := map[rune]string{
		'a': "àáảãạăằắẳẵặâầấẩẫậ", 'e': "èéẻẽẹêềếểễệ", 'i': "ìíỉĩị", 'o': "òóỏõọôồốổỗộơờớởỡợ",
		'u': "ùúủũụưừứửữự", 'y': "ỳýỷỹỵ", 'd': "đ",
	}
	m := map[rune]rune{}
	for base, chars := range groups {
		for _, c := range chars {
			m[c] = base
		}
	}
	return m
}()
