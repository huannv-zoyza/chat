package store

import (
	"fmt"
	"testing"
)

// Nhiệm vụ ngày: 4 cái khác nhau, cố định theo (người, ngày), và mọi nhiệm vụ trong kho đều có lúc được chọn.
func TestDailyQuestsDeterministic(t *testing.T) {
	seen := map[string]bool{}
	for user := int64(1); user <= 20; user++ {
		for d := 1; d <= 28; d++ {
			day := fmt.Sprintf("2026-11-%02d", d)
			qs := dailyQuests(user, day)
			ids := map[string]bool{}
			for _, q := range qs {
				ids[q.ID] = true
				seen[q.ID] = true
			}
			if len(qs) != questsPerDay || len(ids) != questsPerDay {
				t.Fatalf("user %d %s: %+v", user, day, qs)
			}
			if fmt.Sprint(dailyQuests(user, day)) != fmt.Sprint(qs) {
				t.Fatalf("user %d %s: not deterministic", user, day)
			}
		}
	}
	if len(seen) != len(questDefs) {
		t.Fatalf("only %d of %d quests ever picked", len(seen), len(questDefs))
	}
}

func TestLevelFor(t *testing.T) {
	for xp, want := range map[int64]int{0: 1, 29: 1, 30: 2, 79: 2, 80: 3, 1899: 9, 1900: 10, 99999: 10} {
		if got := levelFor(xp); got != want {
			t.Fatalf("levelFor(%d) = %d, want %d", xp, got, want)
		}
	}
}
