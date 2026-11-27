package game

import (
	"slices"
	"strings"
)

// Đánh giá bài poker: chọn 5 lá tốt nhất trong 5–7 lá. Lá bài là chuỗi 2 ký tự: hạng
// "23456789TJQKA" + chất "shdc" (bích, cơ, rô, chuồn), ví dụ "As", "Td".

const (
	pokerRanks = "23456789TJQKA"
	pokerSuits = "shdc"
)

// Hạng bộ bài, từ thấp tới cao.
const (
	handHighCard = iota
	handPair
	handTwoPair
	handTrips
	handStraight
	handFlush
	handFullHouse
	handQuads
	handStraightFlush
)

var handNames = []string{
	"Mậu thầu", "Một đôi", "Hai đôi", "Sám cô", "Sảnh", "Thùng", "Cù lũ", "Tứ quý", "Thùng phá sảnh",
}

// pokerHand: kết quả đánh giá. Score so sánh trực tiếp được (lớn hơn là mạnh hơn).
type pokerHand struct {
	Score int64
	Cat   int
	Cards []string // 5 lá tạo nên bộ
}

func (h pokerHand) name() string {
	if h.Cat == handStraightFlush && h.Score>>16&0xF == 14 {
		return "Thùng phá sảnh lớn (Royal)"
	}
	return handNames[h.Cat]
}

func cardRank(c string) int { return strings.IndexByte(pokerRanks, c[0]) + 2 }

func newPokerDeck() []string {
	d := make([]string, 0, 52)
	for _, s := range pokerSuits {
		for _, r := range pokerRanks {
			d = append(d, string(r)+string(s))
		}
	}
	return d
}

// eval5: điểm của đúng 5 lá. Score = hạng bộ << 20 | 5 hạng phân định (mỗi hạng 4 bit).
func eval5(cards []string) int64 {
	var ranks [5]int
	flush := true
	count := map[int]int{}
	for i, c := range cards {
		ranks[i] = cardRank(c)
		count[ranks[i]]++
		if c[1] != cards[0][1] {
			flush = false
		}
	}
	// thứ tự phân định: nhiều lá cùng hạng trước, rồi hạng cao trước
	order := make([]int, 0, 5)
	for r := range count {
		order = append(order, r)
	}
	slices.SortFunc(order, func(a, b int) int {
		if count[a] != count[b] {
			return count[b] - count[a]
		}
		return b - a
	})
	straightHigh := 0
	if len(order) == 5 {
		if order[0]-order[4] == 4 {
			straightHigh = order[0]
		} else if order[0] == 14 && order[1] == 5 { // A-2-3-4-5: sảnh thấp nhất, A tính là 1
			straightHigh = 5
		}
	}
	var cat int
	switch {
	case straightHigh > 0 && flush:
		cat = handStraightFlush
	case count[order[0]] == 4:
		cat = handQuads
	case count[order[0]] == 3 && count[order[1]] == 2:
		cat = handFullHouse
	case flush:
		cat = handFlush
	case straightHigh > 0:
		cat = handStraight
	case count[order[0]] == 3:
		cat = handTrips
	case count[order[0]] == 2 && count[order[1]] == 2:
		cat = handTwoPair
	case count[order[0]] == 2:
		cat = handPair
	default:
		cat = handHighCard
	}
	score := int64(cat) << 20
	if straightHigh > 0 {
		return score | int64(straightHigh)<<16
	}
	for i, r := range order {
		score |= int64(r) << (16 - 4*i)
	}
	return score
}

// bestHand: bộ 5 lá mạnh nhất trong cards (5..7 lá).
func bestHand(cards []string) pokerHand {
	best := pokerHand{Score: -1}
	n := len(cards)
	pick := make([]string, 5)
	var rec func(start, k int)
	rec = func(start, k int) {
		if k == 5 {
			if s := eval5(pick); s > best.Score {
				best = pokerHand{Score: s, Cat: int(s >> 20), Cards: slices.Clone(pick)}
			}
			return
		}
		for i := start; i <= n-(5-k); i++ {
			pick[k] = cards[i]
			rec(i+1, k+1)
		}
	}
	rec(0, 0)
	return best
}

// partialHandName: tên bộ khi chưa đủ 5 lá (chỉ có 2 lá tẩy trước flop).
func partialHandName(cards []string) string {
	if len(cards) >= 5 {
		return bestHand(cards).name()
	}
	if len(cards) == 2 && cards[0][0] == cards[1][0] {
		return "Đôi " + rankName(cards[0])
	}
	return "Mậu thầu"
}

func rankName(c string) string {
	switch c[0] {
	case 'A':
		return "A"
	case 'K':
		return "K"
	case 'Q':
		return "Q"
	case 'J':
		return "J"
	case 'T':
		return "10"
	}
	return string(c[0])
}
