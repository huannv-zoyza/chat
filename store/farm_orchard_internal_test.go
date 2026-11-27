package store

import "testing"

// Quả chỉ vào pool đơn hàng khi người chơi có cây loại đó.
func TestOrderPoolGatesFruit(t *testing.T) {
	s := &Store{}
	n := 0
	s.SetRand(func() float64 { n++; return float64(n%97) / 97 })
	count := func(fruits map[string]bool) (cam, tao int) {
		for range 400 {
			items, _, _ := s.newOrder(20, map[string]int{kitchenID: 1}, fruits)
			for _, it := range items {
				switch it.Item {
				case "cam":
					cam++
				case "tao":
					tao++
				}
			}
		}
		return
	}
	if cam, tao := count(nil); cam != 0 || tao != 0 {
		t.Fatalf("no trees: cam %d tao %d", cam, tao)
	}
	if cam, tao := count(map[string]bool{"cam": true}); cam == 0 || tao != 0 {
		t.Fatalf("cam tree: cam %d tao %d", cam, tao)
	}
}

func TestOrchardSpotsFn(t *testing.T) {
	for lv, want := range map[int]int{1: 0, 4: 0, 5: 3, 7: 3, 8: 6, 11: 6, 12: 9, 15: 9, 16: 12, 99: 12} {
		if got := orchardSpots(lv); got != want {
			t.Fatalf("orchardSpots(%d) = %d, want %d", lv, got, want)
		}
	}
}
