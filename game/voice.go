package game

// Thoại trong ván: người chơi nói chuyện bằng micro qua kênh thoại riêng của ván (không dùng
// cuộc gọi nhóm của kênh). Máy chủ quyết định ai nghe được ai theo luật của trò và gửi cho
// từng người danh sách này trong View; máy người nói chỉ gửi tiếng tới những người được nghe.
//
// Phòng chờ và khi hết ván: mọi người chơi nghe nhau. Đang chơi: theo luật riêng của trò
// (Ma Sói: ban đêm chỉ bầy Sói nghe nhau, người chết chỉ nói với người chết).

// Voice: Talk = những người nghe được mình, Hear = những người mình nghe được.
type Voice struct {
	Talk []int64 `json:"talk"`
	Hear []int64 `json:"hear"`
}

type voiceRules interface {
	// canHear: lúc đang chơi, listener có nghe được speaker nói không (hai người khác nhau, đều là người chơi).
	canHear(speaker, listener int64) bool
}

// HasVoice: trò có kênh thoại riêng.
func (g *Game) HasVoice() bool {
	_, ok := g.r.(voiceRules)
	return ok
}

// CanHear: listener nghe được speaker trong kênh thoại của ván lúc này không.
func (g *Game) CanHear(speaker, listener int64) bool {
	vr, ok := g.r.(voiceRules)
	b := g.r.base()
	if !ok || speaker == listener || !b.isPlayer(speaker) || !b.isPlayer(listener) {
		return false
	}
	if b.Status != Playing {
		return true
	}
	return vr.canHear(speaker, listener)
}

// voiceFor: ai nghe được viewer và viewer nghe được ai (nil nếu trò không có thoại / không phải người chơi).
func (g *Game) voiceFor(viewer int64) *Voice {
	if !g.HasVoice() || !g.r.base().isPlayer(viewer) {
		return nil
	}
	v := &Voice{Talk: []int64{}, Hear: []int64{}}
	for _, p := range g.r.base().Players {
		if g.CanHear(viewer, p.ID) {
			v.Talk = append(v.Talk, p.ID)
		}
		if g.CanHear(p.ID, viewer) {
			v.Hear = append(v.Hear, p.ID)
		}
	}
	return v
}
