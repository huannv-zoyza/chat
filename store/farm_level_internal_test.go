package store

import (
	"math"
	"testing"
)

func round10(x int64) int64 { return (x + 5) / 10 * 10 }

func TestFarmLevelCurve(t *testing.T) {
	for n := 1; n <= 10; n++ {
		if levelXP(n) != LevelXP[n-1] {
			t.Fatalf("levelXP(%d) = %d, want %d", n, levelXP(n), LevelXP[n-1])
		}
	}
	if levelXP(0) != 0 || levelXP(-3) != 0 {
		t.Fatal("levelXP(n<=1) must be 0")
	}
	if levelXP(11) != 2480 {
		t.Fatalf("levelXP(11) = %d", levelXP(11))
	}
	if want := 2480 + round10(580*115/100); want != 3150 || levelXP(12) != want {
		t.Fatalf("levelXP(12) = %d, want %d", levelXP(12), want)
	}
	prev := levelXP(2) - levelXP(1)
	for n := 3; n <= 200; n++ {
		gap := levelXP(n) - levelXP(n-1)
		if gap <= prev {
			t.Fatalf("gap(%d) = %d not > %d", n, gap, prev)
		}
		prev = gap
	}
	for n := 2; n <= 263; n++ {
		if got := levelFor(levelXP(n)); got != n {
			t.Fatalf("levelFor(levelXP(%d)) = %d", n, got)
		}
		if got := levelFor(levelXP(n) - 1); got != n-1 {
			t.Fatalf("levelFor(levelXP(%d)-1) = %d, want %d", n, got, n-1)
		}
	}
	if lv := levelFor(math.MaxInt64); lv != 263 {
		t.Fatalf("levelFor(MaxInt64) = %d, want 263", lv)
	}
	if levelXP(263) != 8837644963574695370 || levelXP(264) != math.MaxInt64 || levelXP(1000) != math.MaxInt64 {
		t.Fatalf("top levels: %d %d", levelXP(263), levelXP(264))
	}
}

func TestFarmPerks(t *testing.T) {
	cases := []struct {
		lv   int
		want Perks
	}{
		{1, Perks{0, 0, 0, 0}},
		{5, Perks{8, 4, 1, 8}},
		{10, Perks{18, 9, 2, 18}},
		{21, Perks{40, 20, 4, 40}},
		{26, Perks{50, 25, 5, 40}},
		{100, Perks{50, 30, 5, 40}},
	}
	for _, c := range cases {
		if got := farmPerks(c.lv); got != c.want {
			t.Fatalf("farmPerks(%d) = %+v, want %+v", c.lv, got, c.want)
		}
	}
}

// Mua rồi bán lại ở mức thưởng giá cao nhất (+40%) vẫn phải lỗ.
func TestFarmPerksNoArbitrage(t *testing.T) {
	for _, it := range Items {
		if it.BuyPrice > 0 && it.Price*(100+40)/100 >= it.BuyPrice {
			t.Fatalf("%s: sell %d +40%% >= buy %d", it.ID, it.Price, it.BuyPrice)
		}
	}
}
