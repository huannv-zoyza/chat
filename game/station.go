package game

import (
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Trạm Vũ Trụ: trò suy luận thời gian thực. Thuyền viên đi khắp trạm làm nhiệm vụ; kẻ phản
// bội trà trộn, giết người, phá hoại. Thấy xác thì báo, hoặc bấm chuông khẩn cấp để họp:
// cả trạm thảo luận rồi bỏ phiếu đẩy một người ra ngoài không gian.
//
// Thuyền viên thắng khi xong hết nhiệm vụ hoặc đẩy hết kẻ phản bội ra ngoài.
// Kẻ phản bội thắng khi số kẻ phản bội >= số thuyền viên còn sống, hoặc sự cố oxy không được sửa kịp.
//
// Vị trí do client gửi lên (để tự đi mượt), máy chủ kiểm tra tốc độ và tường rồi mới nhận.
// Mỗi người chỉ nhận những gì mình nhìn thấy (frame).

const (
	stRadius      = 22.0
	stSpeed       = 350.0 // đơn vị/giây
	stVisionCrew  = 360.0
	stVisionDark  = 150.0 // khi mất điện
	stVisionBad   = 520.0 // kẻ phản bội nhìn xa hơn
	stKillRange   = 120.0
	stReportRange = 160.0
	stUseRange    = 120.0
	stButtonRange = 130.0
	stIntro       = 5 * time.Second
	stEject       = 7 * time.Second
	stO2Time      = 45 * time.Second
	stSabCooldown = 30 * time.Second
	stFirstKill   = 12 * time.Second
)

type rect struct {
	X, Y, W, H float64
}

func (r rect) has(x, y, inset float64) bool {
	return x >= r.X+inset && x <= r.X+r.W-inset && y >= r.Y+inset && y <= r.Y+r.H-inset
}

type stRoom struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
}

// Trạm: kind là loại trò nhỏ trên client (wires, hold, code, align, scan, fix).
type stStation struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Room string  `json:"room"`
	Kind string  `json:"kind"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

type stMap struct {
	W        float64     `json:"w"`
	H        float64     `json:"h"`
	Rooms    []stRoom    `json:"rooms"`
	Halls    []rect      `json:"halls"`
	Stations []stStation `json:"stations"`
	Button   [2]float64  `json:"button"`
	Spawn    [2]float64  `json:"spawn"`
	Radius   float64     `json:"radius"`
	Speed    float64     `json:"speed"`
}

// Bản đồ "Trạm Hải Đăng": sảnh giữa, bốn phòng chính bốn hướng, bốn phòng góc nối vòng ngoài.
var stationMap = func() stMap {
	rooms := []stRoom{
		{"hub", "Sảnh trung tâm", 1200, 700, 600, 450},
		{"bridge", "Đài chỉ huy", 1250, 100, 500, 350},
		{"engine", "Phòng máy", 150, 650, 500, 550},
		{"lab", "Phòng thí nghiệm", 2350, 650, 500, 550},
		{"storage", "Kho hàng", 1250, 1450, 500, 400},
		{"medbay", "Khoang y tế", 150, 100, 500, 350},
		{"power", "Phòng điện", 2350, 100, 500, 350},
		{"oxygen", "Trạm oxy", 150, 1450, 500, 400},
		{"comms", "Trạm liên lạc", 2350, 1450, 500, 400},
	}
	// hành lang rộng 140 (người chơi bán kính 22 còn ~100 để lách qua cửa), lấn vào phòng
	// 60 đơn vị mỗi đầu để đi qua chỗ nối không bị kẹt
	halls := []rect{
		{1430, 390, 140, 370},  // đài chỉ huy - sảnh
		{590, 850, 670, 140},   // phòng máy - sảnh
		{1740, 850, 670, 140},  // sảnh - thí nghiệm
		{1430, 1090, 140, 420}, // sảnh - kho
		{330, 390, 140, 320},   // y tế - phòng máy
		{590, 210, 720, 140},   // y tế - đài chỉ huy
		{1690, 210, 720, 140},  // đài chỉ huy - phòng điện
		{2530, 390, 140, 320},  // phòng điện - thí nghiệm
		{330, 1140, 140, 370},  // phòng máy - oxy
		{590, 1580, 720, 140},  // oxy - kho
		{1690, 1580, 720, 140}, // kho - liên lạc
		{2530, 1140, 140, 370}, // thí nghiệm - liên lạc
	}
	stations := []stStation{
		{"eng-fuel", "Nạp nhiên liệu", "engine", "hold", 260, 760},
		{"eng-align", "Căn chỉnh động cơ", "engine", "align", 540, 1090},
		{"med-scan", "Quét sinh trắc", "medbay", "scan", 400, 200},
		{"med-sample", "Kiểm tra mẫu máu", "medbay", "code", 250, 380},
		{"bri-course", "Chỉnh hướng bay", "bridge", "align", 1500, 170},
		{"bri-data", "Tải nhật ký bay", "bridge", "hold", 1690, 390},
		{"pow-wires", "Nối dây điện", "power", "wires", 2450, 190},
		{"lab-code", "Phân tích mẫu", "lab", "code", 2780, 760},
		{"lab-wires", "Sửa mạch cảm biến", "lab", "wires", 2420, 1120},
		{"sto-crates", "Xếp thùng hàng", "storage", "wires", 1330, 1540},
		{"sto-trash", "Đổ rác", "storage", "hold", 1680, 1790},
		{"oxy-filter", "Thay lõi lọc", "oxygen", "align", 260, 1780},
		{"com-upload", "Gửi dữ liệu", "comms", "hold", 2780, 1540},
		{"com-code", "Giải mã tín hiệu", "comms", "code", 2450, 1790},
		{"hub-wires", "Sửa đèn sảnh", "hub", "wires", 1260, 760},
		// chỗ sửa sự cố
		{"fix-power", "Bảng điện chính", "power", "fix", 2780, 380},
		{"fix-oxygen", "Van oxy", "oxygen", "fix", 560, 1540},
	}
	return stMap{
		W: 3000, H: 1950, Rooms: rooms, Halls: halls, Stations: stations,
		Button: [2]float64{1500, 925}, Spawn: [2]float64{1500, 925},
		Radius: stRadius, Speed: stSpeed,
	}
}()

func stStationByID(id string) (stStation, bool) {
	for _, s := range stationMap.Stations {
		if s.ID == id {
			return s, true
		}
	}
	return stStation{}, false
}

func stRoomName(x, y float64) string {
	for _, r := range stationMap.Rooms {
		if (rect{r.X, r.Y, r.W, r.H}).has(x, y, 0) {
			return r.Name
		}
	}
	return "hành lang"
}

// walkable: hình tròn bán kính stRadius tâm (x, y) nằm trọn trong phòng hoặc hành lang.
func walkable(x, y float64) bool {
	for _, r := range stationMap.Rooms {
		if (rect{r.X, r.Y, r.W, r.H}).has(x, y, stRadius) {
			return true
		}
	}
	for _, h := range stationMap.Halls {
		if h.has(x, y, stRadius) {
			return true
		}
	}
	return false
}

// Thời gian tối thiểu làm một trò nhỏ (chặn bấm "xong" ngay lập tức).
var stMinTime = map[string]time.Duration{
	"wires": 1500 * time.Millisecond, "hold": 3 * time.Second, "code": 2 * time.Second,
	"align": 1 * time.Second, "scan": 6 * time.Second, "fix": 2500 * time.Millisecond,
}

type stOptions struct {
	Traitors     int  `json:"traitors"`      // 0: tự chọn theo số người
	KillCooldown int  `json:"kill_cooldown"` // giây
	Tasks        int  `json:"tasks"`
	Discuss      int  `json:"discuss"` // giây thảo luận trước khi được bỏ phiếu
	Vote         int  `json:"vote"`    // giây bỏ phiếu
	Emergencies  int  `json:"emergencies"`
	HideEjects   bool `json:"hide_ejects"` // không cho biết người bị đẩy ra có phải kẻ phản bội
}

type stTask struct {
	Station string `json:"station"`
	Done    bool   `json:"done"`
}

type stPlayer struct {
	ID          int64     `json:"id"`
	Color       int       `json:"color"`
	Traitor     bool      `json:"traitor"`
	Dead        bool      `json:"dead"`
	X           float64   `json:"x"`
	Y           float64   `json:"y"`
	Tasks       []stTask  `json:"tasks"`
	Using       string    `json:"using,omitempty"`
	UsingAt     time.Time `json:"using_at,omitzero"`
	KillReadyAt time.Time `json:"kill_ready_at,omitzero"`
	Emergencies int       `json:"emergencies"`
	Correction  int       `json:"correction"`
	lastMove    time.Time
}

type stBody struct {
	ID int64   `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

type stChat struct {
	From int64  `json:"from"`
	Text string `json:"text"`
	Dead bool   `json:"dead,omitempty"` // tin của hồn ma: chỉ hồn ma thấy
}

type stMeeting struct {
	Caller  int64           `json:"caller"`
	Body    int64           `json:"body,omitempty"` // 0: chuông khẩn cấp
	VoteAt  time.Time       `json:"vote_at"`
	Votes   map[int64]int64 `json:"votes"`
	Chat    []stChat        `json:"chat"`
	Ejected int64           `json:"ejected,omitempty"`
	Tie     bool            `json:"tie,omitempty"`
	Counts  map[int64]int   `json:"counts,omitempty"` // kết quả (sau khi chốt), 0 = bỏ qua
}

type station struct {
	Base
	Opts       stOptions           `json:"opts"`
	P          map[int64]*stPlayer `json:"p"`
	Phase      string              `json:"phase,omitempty"` // intro | play | meeting | eject
	PhaseEnds  time.Time           `json:"phase_ends,omitzero"`
	Bodies     []stBody            `json:"bodies"`
	Sab        string              `json:"sab,omitempty"` // power | oxygen
	SabEnds    time.Time           `json:"sab_ends,omitzero"`
	SabReadyAt time.Time           `json:"sab_ready_at,omitzero"`
	Meeting    *stMeeting          `json:"meeting,omitempty"`
	Meetings   int                 `json:"meetings"`
	dirty      bool
}

func (s *station) limits() (int, int) { return 4, 12 }
func (s *station) autoStart() bool    { return false }
func (s *station) options() any       { return &s.Opts }

func (s *station) normalize() {
	o := &s.Opts
	def := func(v *int, d, lo, hi int) {
		if *v == 0 {
			*v = d
		}
		*v = min(max(*v, lo), hi)
	}
	o.Traitors = min(max(o.Traitors, 0), 3)
	def(&o.KillCooldown, 25, 10, 60)
	def(&o.Tasks, 5, 2, 8)
	def(&o.Discuss, 30, 5, 120)
	def(&o.Vote, 60, 15, 180)
	if o.Emergencies == 0 {
		o.Emergencies = 1
	}
	o.Emergencies = min(max(o.Emergencies, 0), 3)
}

func (s *station) traitorCount() int {
	n := len(s.Players)
	if s.Opts.Traitors > 0 {
		return min(s.Opts.Traitors, (n-1)/2)
	}
	switch {
	case n <= 6:
		return 1
	case n <= 10:
		return 2
	}
	return 3
}

func (s *station) start(now time.Time) error {
	ids := make([]int64, len(s.Players))
	for i, p := range s.Players {
		ids[i] = p.ID
	}
	shuffle(ids)
	traitors := map[int64]bool{}
	for _, id := range ids[:s.traitorCount()] {
		traitors[id] = true
	}
	var taskIDs []string
	for _, st := range stationMap.Stations {
		if st.Kind != "fix" {
			taskIDs = append(taskIDs, st.ID)
		}
	}
	colors := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	shuffle(colors)
	s.P = map[int64]*stPlayer{}
	for i, p := range s.Players {
		pool := slices.Clone(taskIDs)
		shuffle(pool)
		tasks := make([]stTask, s.Opts.Tasks)
		for j := range tasks {
			tasks[j] = stTask{Station: pool[j]}
		}
		s.P[p.ID] = &stPlayer{ID: p.ID, Color: colors[i%len(colors)], Traitor: traitors[p.ID], Tasks: tasks, Emergencies: s.Opts.Emergencies}
	}
	s.Bodies = []stBody{}
	s.gather()
	s.Phase = "intro"
	s.PhaseEnds = now.Add(stIntro)
	for _, p := range s.P {
		p.KillReadyAt = now.Add(stIntro + stFirstKill)
	}
	s.SabReadyAt = now.Add(stIntro + stFirstKill)
	s.logf("🚀 Trạm Hải Đăng khởi động với %d người, trong đó có %d kẻ phản bội.", len(s.Players), len(traitors))
	return nil
}

// gather: đưa mọi người về quanh chuông ở sảnh (đầu ván, sau mỗi cuộc họp).
func (s *station) gather() {
	n := len(s.Players)
	for i, pl := range s.Players {
		p := s.P[pl.ID]
		a := 2 * math.Pi * float64(i) / float64(n)
		p.X = stationMap.Spawn[0] + 140*math.Cos(a)
		p.Y = stationMap.Spawn[1] + 140*math.Sin(a)
		p.Using = ""
		p.Correction++
		p.lastMove = time.Time{}
	}
}

func dist(ax, ay, bx, by float64) float64 { return math.Hypot(ax-bx, ay-by) }

func (s *station) alive(traitor bool) int {
	n := 0
	for _, p := range s.P {
		if !p.Dead && p.Traitor == traitor {
			n++
		}
	}
	return n
}

func (s *station) taskProgress() (done, total int) {
	for _, p := range s.P {
		if p.Traitor {
			continue
		}
		for _, t := range p.Tasks {
			total++
			if t.Done {
				done++
			}
		}
	}
	return
}

func (s *station) finish(traitorsWin bool, why string) {
	var winners, bad []int64
	for _, pl := range s.Players {
		p := s.P[pl.ID]
		if p.Traitor {
			bad = append(bad, pl.ID)
		}
		if p.Traitor == traitorsWin {
			winners = append(winners, pl.ID)
		}
	}
	names := make([]string, len(bad))
	for i, id := range bad {
		names[i] = s.name(id)
	}
	s.Phase = ""
	s.Meeting = nil
	s.Sab = ""
	if traitorsWin {
		s.logf("💀 %s Kẻ phản bội thắng! (%s)", why, strings.Join(names, ", "))
		s.end("Kẻ phản bội thắng!", winners)
	} else {
		s.logf("🎉 %s Thuyền viên thắng! Kẻ phản bội là %s.", why, strings.Join(names, ", "))
		s.end("Thuyền viên thắng!", winners)
	}
	s.dirty = true
}

// checkWin sau khi có người chết / bị đẩy ra / xong nhiệm vụ.
func (s *station) checkWin() bool {
	bad, good := s.alive(true), s.alive(false)
	switch {
	case bad == 0:
		s.finish(false, "Đã đẩy hết kẻ phản bội ra ngoài.")
	case bad >= good:
		s.finish(true, "Kẻ phản bội đã áp đảo thuyền viên.")
	default:
		if d, t := s.taskProgress(); t > 0 && d == t {
			s.finish(false, "Toàn bộ nhiệm vụ đã hoàn thành.")
			return true
		}
		return false
	}
	return true
}

/* ---------- Di chuyển (client gửi vị trí) ---------- */

func (s *station) move(id int64, x, y float64, now time.Time) {
	p := s.P[id]
	if p == nil || s.Status != Playing || s.Phase != "play" {
		return
	}
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return
	}
	dt := 0.15
	if !p.lastMove.IsZero() {
		dt = min(now.Sub(p.lastMove).Seconds(), 0.5)
	}
	ok := dist(p.X, p.Y, x, y) <= stSpeed*dt*1.4+30
	if p.Dead {
		ok = ok && x >= 0 && y >= 0 && x <= stationMap.W && y <= stationMap.H // hồn ma đi xuyên tường
	} else {
		ok = ok && walkable(x, y)
	}
	p.lastMove = now
	if !ok {
		p.Correction++ // client nhảy về vị trí máy chủ đang giữ
		return
	}
	p.X, p.Y = x, y
}

/* ---------- Hành động ---------- */

type stUseData struct {
	Station string `json:"station"`
}

type stKillData struct {
	Target int64 `json:"target"`
}

type stSabData struct {
	Kind string `json:"kind"`
}

func (s *station) act(id int64, action string, data json.RawMessage, now time.Time) error {
	p := s.P[id]
	if p == nil {
		return Error("Bạn không chơi ván này.")
	}
	switch action {
	case "vote", "chat":
		return s.meetingAct(p, action, data, now)
	}
	if s.Phase != "play" {
		return Error("Chưa thể làm việc này lúc này.")
	}
	switch action {
	case "use":
		var d stUseData
		if err := decode(data, &d); err != nil {
			return err
		}
		st, ok := stStationByID(d.Station)
		if !ok || dist(p.X, p.Y, st.X, st.Y) > stUseRange*1.3 {
			return Error("Hãy lại gần hơn.")
		}
		if st.Kind == "fix" {
			if p.Dead || (st.ID == "fix-power" && s.Sab != "power") || (st.ID == "fix-oxygen" && s.Sab != "oxygen") {
				return Error("Không có gì cần sửa ở đây.")
			}
		} else if p.Traitor {
			// nhiệm vụ của kẻ phản bội là giả: chỉ đứng cạnh để giả vờ, không mở máy được
			return Error("Kẻ phản bội không làm được nhiệm vụ thật, chỉ đứng giả vờ thôi.")
		} else if !slices.ContainsFunc(p.Tasks, func(t stTask) bool { return t.Station == st.ID && !t.Done }) {
			return Error("Đây không phải nhiệm vụ của bạn.")
		}
		p.Using, p.UsingAt = st.ID, now
	case "task", "fix":
		var d stUseData
		if err := decode(data, &d); err != nil {
			return err
		}
		st, ok := stStationByID(d.Station)
		if !ok || p.Using != st.ID || now.Sub(p.UsingAt) < stMinTime[st.Kind] {
			return Error("Chưa làm xong.")
		}
		if dist(p.X, p.Y, st.X, st.Y) > stUseRange*1.6 {
			return Error("Bạn đã đi quá xa.")
		}
		p.Using = ""
		if st.Kind == "fix" {
			if (st.ID == "fix-power" && s.Sab == "power") || (st.ID == "fix-oxygen" && s.Sab == "oxygen") {
				s.logf("🔧 Sự cố %s đã được khắc phục.", map[string]string{"power": "mất điện", "oxygen": "rò oxy"}[s.Sab])
				s.Sab, s.SabEnds = "", time.Time{}
				s.SabReadyAt = now.Add(stSabCooldown)
				s.dirty = true
			}
			return nil
		}
		for i := range p.Tasks {
			if p.Tasks[i].Station == st.ID {
				p.Tasks[i].Done = true
			}
		}
		s.dirty = true
		s.checkWin()
	case "cancel_use":
		p.Using = ""
	case "kill":
		var d stKillData
		if err := decode(data, &d); err != nil {
			return err
		}
		v := s.P[d.Target]
		switch {
		case !p.Traitor || p.Dead:
			return Error("Bạn không thể làm việc này.")
		case now.Before(p.KillReadyAt):
			return Error("Chưa hồi chiêu xong.")
		case v == nil || v.Dead || v.Traitor:
			return Error("Không nhắm được người này.")
		case dist(p.X, p.Y, v.X, v.Y) > stKillRange*1.25:
			return Error("Mục tiêu ở quá xa.")
		}
		v.Dead = true
		v.Using = ""
		s.Bodies = append(s.Bodies, stBody{ID: v.ID, X: v.X, Y: v.Y})
		p.X, p.Y = v.X, v.Y
		p.Correction++
		p.KillReadyAt = now.Add(time.Duration(s.Opts.KillCooldown) * time.Second)
		s.dirty = true
		s.checkWin()
	case "report":
		var d stKillData
		if err := decode(data, &d); err != nil {
			return err
		}
		if p.Dead {
			return Error("Hồn ma không báo được.")
		}
		i := slices.IndexFunc(s.Bodies, func(b stBody) bool { return b.ID == d.Target })
		if i < 0 || dist(p.X, p.Y, s.Bodies[i].X, s.Bodies[i].Y) > stReportRange*1.3 {
			return Error("Không thấy xác nào gần đây.")
		}
		s.logf("📢 %s báo có xác của %s ở %s.", s.name(id), s.name(d.Target), stRoomName(s.Bodies[i].X, s.Bodies[i].Y))
		s.startMeeting(id, d.Target, now)
	case "emergency":
		switch {
		case p.Dead:
			return Error("Hồn ma không bấm chuông được.")
		case p.Emergencies <= 0:
			return Error("Bạn đã hết lượt gọi họp khẩn.")
		case s.Sab != "":
			return Error("Không gọi họp được khi đang có sự cố.")
		case dist(p.X, p.Y, stationMap.Button[0], stationMap.Button[1]) > stButtonRange*1.3:
			return Error("Hãy lại gần chuông khẩn cấp.")
		}
		p.Emergencies--
		s.logf("🚨 %s bấm chuông họp khẩn cấp.", s.name(id))
		s.startMeeting(id, 0, now)
	case "sabotage":
		var d stSabData
		if err := decode(data, &d); err != nil {
			return err
		}
		switch {
		case !p.Traitor:
			return Error("Bạn không thể làm việc này.")
		case s.Sab != "":
			return Error("Đang có sự cố rồi.")
		case now.Before(s.SabReadyAt):
			return Error("Phá hoại chưa hồi chiêu xong.")
		}
		switch d.Kind {
		case "power":
			s.Sab = "power"
		case "oxygen":
			s.Sab, s.SabEnds = "oxygen", now.Add(stO2Time)
		default:
			return Error("Không có kiểu phá hoại này.")
		}
		s.dirty = true
	default:
		return Error("Hành động không hợp lệ.")
	}
	return nil
}

func (s *station) startMeeting(caller, body int64, now time.Time) {
	s.Meetings++
	s.Phase = "meeting"
	s.Meeting = &stMeeting{Caller: caller, Body: body, VoteAt: now.Add(time.Duration(s.Opts.Discuss) * time.Second), Votes: map[int64]int64{}, Chat: []stChat{}}
	s.PhaseEnds = s.Meeting.VoteAt.Add(time.Duration(s.Opts.Vote) * time.Second)
	s.Sab, s.SabEnds = "", time.Time{} // cuộc họp dập mọi sự cố
	for _, p := range s.P {
		p.Using = ""
	}
	s.dirty = true
}

type stChatData struct {
	Text string `json:"text"`
}

func (s *station) meetingAct(p *stPlayer, action string, data json.RawMessage, now time.Time) error {
	m := s.Meeting
	if s.Phase != "meeting" || m == nil {
		return Error("Không có cuộc họp nào.")
	}
	if action == "chat" {
		var d stChatData
		if err := decode(data, &d); err != nil {
			return err
		}
		text := strings.Join(strings.Fields(d.Text), " ")
		if text == "" || utf8.RuneCountInString(text) > 200 {
			return Error("Tin nhắn cần 1–200 ký tự.")
		}
		if len(m.Chat) >= 300 {
			return Error("Cuộc họp đã quá nhiều tin.")
		}
		m.Chat = append(m.Chat, stChat{From: p.ID, Text: text, Dead: p.Dead})
		return nil
	}
	var d stKillData
	if err := decode(data, &d); err != nil {
		return err
	}
	switch {
	case p.Dead:
		return Error("Hồn ma không bỏ phiếu được.")
	case now.Before(m.VoteAt):
		return Error("Đang thảo luận, chưa bỏ phiếu được.")
	case hasKey(m.Votes, p.ID):
		return Error("Bạn đã bỏ phiếu rồi.")
	case d.Target != 0 && (s.P[d.Target] == nil || s.P[d.Target].Dead):
		return Error("Không bỏ phiếu cho người này được.")
	}
	m.Votes[p.ID] = d.Target
	if len(m.Votes) == s.alive(true)+s.alive(false) {
		s.resolveMeeting(now)
	}
	return nil
}

func hasKey(m map[int64]int64, k int64) bool {
	_, ok := m[k]
	return ok
}

func (s *station) resolveMeeting(now time.Time) {
	m := s.Meeting
	target, count, counts := tally(m.Votes)
	m.Counts = counts
	if target == 0 {
		m.Tie = true
		s.logf("🗳️ Cuộc họp %d: không ai bị đẩy ra (hoà phiếu hoặc bỏ qua).", s.Meetings)
	} else {
		m.Ejected = target
		v := s.P[target]
		v.Dead = true
		what := ""
		if !s.Opts.HideEjects {
			if v.Traitor {
				what = " — đó là kẻ phản bội!"
			} else {
				what = " — đó không phải kẻ phản bội."
			}
		}
		s.logf("🗳️ Cuộc họp %d: %s bị đẩy ra ngoài không gian với %d phiếu%s", s.Meetings, s.name(target), count, what)
	}
	s.Phase = "eject"
	s.PhaseEnds = now.Add(stEject)
	s.dirty = true
}

func (s *station) endEject(now time.Time) {
	s.Bodies = []stBody{}
	s.Meeting = nil
	if s.checkWin() {
		return
	}
	s.gather()
	for _, p := range s.P {
		p.KillReadyAt = now.Add(time.Duration(s.Opts.KillCooldown) * time.Second)
	}
	if s.SabReadyAt.Before(now.Add(15 * time.Second)) {
		s.SabReadyAt = now.Add(15 * time.Second)
	}
	s.Phase = "play"
	s.dirty = true
}

// step: đồng hồ của ván (gọi nhiều lần mỗi giây).
func (s *station) step(now time.Time) bool {
	if s.Status != Playing {
		return false
	}
	switch s.Phase {
	case "intro":
		if !now.Before(s.PhaseEnds) {
			s.Phase = "play"
			s.dirty = true
		}
	case "play":
		if s.Sab == "oxygen" && !now.Before(s.SabEnds) {
			s.finish(true, "Oxy đã cạn trước khi kịp sửa.")
		}
	case "meeting":
		if !now.Before(s.PhaseEnds) {
			s.resolveMeeting(now)
		}
	case "eject":
		if !now.Before(s.PhaseEnds) {
			s.endEject(now)
		}
	}
	d := s.dirty
	s.dirty = false
	return d
}

func (s *station) tick(now time.Time) bool { return s.step(now) }

/* ---------- Những gì mỗi người thấy ---------- */

type stFramePlayer struct {
	ID      int64   `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Color   int     `json:"color"`
	Ghost   bool    `json:"ghost,omitempty"`
	Traitor bool    `json:"traitor,omitempty"` // chỉ kẻ phản bội thấy đồng bọn
}

type stFrameMe struct {
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	Color      int      `json:"color"`
	Traitor    bool     `json:"traitor"`
	Ghost      bool     `json:"ghost"`
	Correction int      `json:"correction"`
	Tasks      []stTask `json:"tasks"`
	Using      string   `json:"using,omitempty"`
	KillIn     int      `json:"kill_in"` // ms tới khi giết được
	Emergency  int      `json:"emergencies"`
	Vision     float64  `json:"vision"`
}

type stFrameMeeting struct {
	Caller  int64           `json:"caller"`
	Body    int64           `json:"body,omitempty"`
	VoteIn  int             `json:"vote_in"` // ms tới lúc được bỏ phiếu
	Voted   []int64         `json:"voted"`   // ai đã bỏ phiếu (không lộ bầu ai)
	MyVote  *int64          `json:"my_vote,omitempty"`
	Chat    []stChat        `json:"chat"`
	Alive   []int64         `json:"alive"`
	Ejected int64           `json:"ejected,omitempty"`
	Tie     bool            `json:"tie,omitempty"`
	Votes   map[int64]int64 `json:"votes,omitempty"` // lộ ra sau khi chốt
	WasBad  *bool           `json:"was_traitor,omitempty"`
}

type stFrame struct {
	Phase    string          `json:"phase"`
	EndsIn   int             `json:"ends_in,omitempty"` // ms
	Me       stFrameMe       `json:"me"`
	Players  []stFramePlayer `json:"players"`
	Bodies   []stBody        `json:"bodies"`
	TaskDone int             `json:"task_done"`
	TaskAll  int             `json:"task_all"`
	Sab      string          `json:"sab,omitempty"`
	SabIn    int             `json:"sab_in,omitempty"`    // ms tới khi oxy cạn
	SabReady int             `json:"sab_ready,omitempty"` // ms tới khi phá hoại được (kẻ phản bội)
	Meeting  *stFrameMeeting `json:"meeting,omitempty"`
	Allies   []int64         `json:"allies,omitempty"` // đồng bọn (kẻ phản bội)
	Colors   map[int64]int   `json:"colors"`
}

func ms(d time.Duration) int { return max(0, int(d/time.Millisecond)) }

func (s *station) frame(viewer int64, now time.Time) any {
	p := s.P[viewer]
	if p == nil {
		return nil
	}
	vision := stVisionCrew
	if p.Traitor {
		vision = stVisionBad
	} else if s.Sab == "power" {
		vision = stVisionDark
	}
	f := stFrame{Phase: s.Phase, Players: []stFramePlayer{}, Bodies: []stBody{}, Sab: s.Sab, Colors: map[int64]int{}}
	if s.Status == Ended {
		f.Phase = "ended"
	}
	if s.Phase == "intro" || s.Phase == "meeting" || s.Phase == "eject" {
		f.EndsIn = ms(s.PhaseEnds.Sub(now))
	}
	f.Me = stFrameMe{X: p.X, Y: p.Y, Color: p.Color, Traitor: p.Traitor, Ghost: p.Dead, Correction: p.Correction,
		Tasks: p.Tasks, Using: p.Using, KillIn: ms(p.KillReadyAt.Sub(now)), Emergency: p.Emergencies, Vision: vision}
	if p.Dead {
		f.Me.Vision = 10000
	}
	f.TaskDone, f.TaskAll = s.taskProgress()
	if s.Sab == "oxygen" {
		f.SabIn = ms(s.SabEnds.Sub(now))
	}
	if p.Traitor {
		f.SabReady = ms(s.SabReadyAt.Sub(now))
	}
	for _, pl := range s.Players {
		o := s.P[pl.ID]
		f.Colors[pl.ID] = o.Color
		if p.Traitor && o.Traitor && o.ID != viewer {
			f.Allies = append(f.Allies, o.ID)
		}
		if o.ID == viewer {
			continue
		}
		if o.Dead && !p.Dead { // người sống không thấy hồn ma
			continue
		}
		if !p.Dead && dist(p.X, p.Y, o.X, o.Y) > vision+60 {
			continue
		}
		f.Players = append(f.Players, stFramePlayer{ID: o.ID, X: o.X, Y: o.Y, Color: o.Color, Ghost: o.Dead, Traitor: p.Traitor && o.Traitor})
	}
	for _, b := range s.Bodies {
		if p.Dead || dist(p.X, p.Y, b.X, b.Y) <= vision+60 {
			f.Bodies = append(f.Bodies, b)
		}
	}
	if m := s.Meeting; m != nil {
		fm := &stFrameMeeting{Caller: m.Caller, Body: m.Body, VoteIn: ms(m.VoteAt.Sub(now)), Voted: []int64{}, Chat: []stChat{}, Alive: []int64{}}
		for _, pl := range s.Players {
			if !s.P[pl.ID].Dead || pl.ID == m.Ejected {
				fm.Alive = append(fm.Alive, pl.ID)
			}
		}
		for v := range m.Votes {
			fm.Voted = append(fm.Voted, v)
		}
		sort.Slice(fm.Voted, func(i, j int) bool { return fm.Voted[i] < fm.Voted[j] })
		if v, ok := m.Votes[viewer]; ok {
			fm.MyVote = &v
		}
		for _, c := range m.Chat {
			if !c.Dead || p.Dead {
				fm.Chat = append(fm.Chat, c)
			}
		}
		if s.Phase == "eject" {
			fm.Votes = m.Votes
			fm.Ejected, fm.Tie = m.Ejected, m.Tie
			if m.Ejected != 0 && !s.Opts.HideEjects {
				b := s.P[m.Ejected].Traitor
				fm.WasBad = &b
			}
		}
		f.Meeting = fm
	}
	return f
}

/* ---------- Thẻ trong kênh chat ---------- */

type stPublic struct {
	Phase    string        `json:"phase,omitempty"`
	TaskDone int           `json:"task_done"`
	TaskAll  int           `json:"task_all"`
	Dead     []int64       `json:"dead"`
	Traitors []int64       `json:"traitors,omitempty"` // khi hết ván
	Colors   map[int64]int `json:"colors"`
	Meetings int           `json:"meetings"`
	Count    int           `json:"traitor_count"`
	Opts     stOptions     `json:"opts"`
}

type stMe struct {
	Traitor bool `json:"traitor"`
}

func (s *station) view(viewer int64) (any, any) {
	pub := stPublic{Phase: s.Phase, Dead: []int64{}, Colors: map[int64]int{}, Meetings: s.Meetings, Opts: s.Opts}
	if s.Status == Lobby {
		pub.Count = s.traitorCount()
		if len(s.Players) < 4 {
			pub.Count = 1
		}
	}
	pub.TaskDone, pub.TaskAll = s.taskProgress()
	for _, pl := range s.Players {
		p := s.P[pl.ID]
		if p == nil {
			continue
		}
		pub.Colors[pl.ID] = p.Color
		if p.Traitor {
			pub.Count++
			if s.Status == Ended {
				pub.Traitors = append(pub.Traitors, pl.ID)
			}
		}
		if p.Dead {
			pub.Dead = append(pub.Dead, pl.ID)
		}
	}
	if p := s.P[viewer]; p != nil {
		return pub, stMe{Traitor: p.Traitor}
	}
	return pub, nil
}

func (s *station) mapData() any { return stationMap }
