package store

import "math"

// LevelXP[n-1]: tổng XP cần để đạt cấp n (cấp 1..10). Từ cấp 11 trở đi tính theo levelXP (không giới hạn cấp).
var LevelXP = []int64{0, 30, 80, 160, 280, 450, 680, 1000, 1400, 1900}

// nextGap: khoảng XP giữa hai cấp liên tiếp sau cấp 10: gap × 115 / 100 làm tròn đến hàng chục.
// Viết g*115/100 dưới dạng g/100*115 + (g%100)*115/100 (tương đương số nguyên) để không tràn int64.
func nextGap(g int64) int64 { return (g/100*115 + (g%100)*115/100 + 5) / 10 * 10 }

// levelXP: tổng XP cần để đạt cấp n (n ≤ 1: 0; vượt int64: bão hòa MaxInt64). Cấp 1..10 theo LevelXP, sau đó khoảng cách tăng 15% mỗi cấp.
func levelXP(n int) int64 {
	if n <= 1 {
		return 0
	}
	if n <= len(LevelXP) {
		return LevelXP[n-1]
	}
	xp, gap := LevelXP[len(LevelXP)-1], LevelXP[len(LevelXP)-1]-LevelXP[len(LevelXP)-2]
	for i := len(LevelXP) + 1; i <= n; i++ {
		gap = nextGap(gap)
		if xp > math.MaxInt64-gap {
			return math.MaxInt64 // cấp không thể đạt được: bão hòa thay vì tràn
		}
		xp += gap
	}
	return xp
}

// levelFor: cấp ứng với tổng XP. Dừng an toàn khi sắp tràn int64 (xp = MaxInt64 vẫn trả về cấp hữu hạn).
func levelFor(xp int64) int {
	lv := 1
	for i := 1; i < len(LevelXP); i++ {
		if xp >= LevelXP[i] {
			lv = i + 1
		}
	}
	if lv < len(LevelXP) {
		return lv
	}
	cur, gap := LevelXP[len(LevelXP)-1], LevelXP[len(LevelXP)-1]-LevelXP[len(LevelXP)-2]
	for {
		gap = nextGap(gap)
		if cur > math.MaxInt64-gap || xp < cur+gap {
			break
		}
		cur += gap
		lv++
	}
	return lv
}

// Perks: quyền lợi theo cấp nông trại.
type Perks struct {
	GrowPct      int   `json:"grow_pct"`      // cây chín nhanh hơn
	AnimalPct    int   `json:"animal_pct"`    // vật nuôi ra sản phẩm nhanh hơn
	HarvestBonus int64 `json:"harvest_bonus"` // đơn vị nông sản cộng thêm mỗi vụ của chủ
	SellPct      int   `json:"sell_pct"`      // giá bán ở chợ cao hơn
}

func farmPerks(level int) Perks {
	l := max(level, 1)
	return Perks{
		GrowPct:      min(2*(l-1), 50),
		AnimalPct:    min(l-1, 30),
		HarvestBonus: int64(min(l/5, 5)),
		SellPct:      min(2*(l-1), 40),
	}
}
