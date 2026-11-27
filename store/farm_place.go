package store

import "context"

// Đặt công trình ở chỗ tuỳ chọn (specs/FARM_SPEC.md §10). Bản đồ nông trại là lưới GW × GH ô; công
// trình "tự do" (Size khác 0, hiện là xưởng chế biến) chiếm một khối Size.W × Size.D ô bắt đầu ở ô
// (X, Y) nguyên, không được ra ngoài bản đồ, không chồng lên vùng cố định (FarmBlocked: ruộng, chuồng,
// ao, nhà kho, đường đi...) và không chồng lên công trình tự do khác. Chuồng gà, đồng cỏ cố định chỗ.

const (
	FarmMapW = 24
	FarmMapH = 20
)

// Spot: ô góc trên (nhỏ nhất) của công trình trên bản đồ.
type Spot struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Size: số ô công trình chiếm theo hai trục.
type Size struct {
	W int `json:"w"`
	D int `json:"d"`
}

// Rect: một vùng trên bản đồ (toạ độ ô, có thể lẻ).
type Rect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	D float64 `json:"d"`
}

func (a Rect) overlaps(b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.D && b.Y < a.Y+a.D
}

// FarmBlocked: vùng cố định không xây đè được, khớp cách vẽ ở desktop/src/components/FarmWorld.tsx.
var FarmBlocked = []Rect{
	{0, 0, FarmMapW, 1},   // hàng cây phía sau
	{0, 0, 1, FarmMapH},   // hàng cây bên trái
	{1.5, 1.5, 6, 5.4},    // chuồng gà
	{1.5, 8.5, 6.4, 6.6},  // đồng cỏ
	{9, 6, 6, 5},          // ruộng (cỡ lớn nhất) và biển mở rộng
	{9.5, 0.5, 7, 3},      // nhà kho, silo, đống rơm
	{15.5, 2.5, 6.5, 4.8}, // vườn cây ăn quả (lưới 4×3 chỗ trồng, farm_orchard.go)
	{17, 9.5, 4.5, 3.5},   // ao vịt
	{14.5, 10, 1, 1},      // chó giữ nhà
	{8, 0, 1, 16},         // đường đá dọc
	{0, 16, FarmMapW, 1},  // đường đá ngang
	{8, 10.5, 1.5, 1.5},   // bác nông dân
	{19.5, 15.5, 2, 2},    // xe tải đơn hàng
	{9, 17, 6.5, 1.5},     // luống hoa
}

var (
	ErrFarmSpotOut     = &FarmError{"Chỗ này ra ngoài nông trại rồi."}
	ErrFarmSpotBlocked = &FarmError{"Chỗ này vướng ruộng, chuồng, ao hoặc đường đi, chọn chỗ khác nhé."}
	ErrFarmSpotTaken   = &FarmError{"Chỗ này đã có công trình khác."}
	ErrFarmNotMovable  = &FarmError{"Công trình này không dời chỗ được."}
)

func (b buildingDef) movable() bool { return b.Size.W > 0 }

func (b buildingDef) rectAt(s Spot) Rect {
	return Rect{float64(s.X), float64(s.Y), float64(b.Size.W), float64(b.Size.D)}
}

// spots: chỗ của các công trình tự do đã xây của userID (chưa chọn chỗ thì là chỗ mặc định).
func loadSpots(ctx context.Context, q queryer, userID int64) (map[string]Spot, error) {
	rows, err := q.QueryContext(ctx, `SELECT building, x, y FROM farm_buildings WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Spot{}
	for rows.Next() {
		var id string
		var x, y *int
		if err := rows.Scan(&id, &x, &y); err != nil {
			return nil, err
		}
		b, ok := buildingByID(id)
		if !ok || !b.movable() {
			continue
		}
		s := b.Default
		if x != nil && y != nil {
			s = Spot{*x, *y}
		}
		out[id] = s
	}
	return out, rows.Err()
}

// checkSpot: công trình b đặt ở s có hợp lệ không (others: chỗ các công trình tự do khác, bỏ qua b).
func checkSpot(b buildingDef, s Spot, others map[string]Spot) error {
	r := b.rectAt(s)
	if s.X < 0 || s.Y < 0 || s.X+b.Size.W > FarmMapW || s.Y+b.Size.D > FarmMapH {
		return ErrFarmSpotOut
	}
	for _, z := range FarmBlocked {
		if r.overlaps(z) {
			return ErrFarmSpotBlocked
		}
	}
	for id, o := range others {
		if id == b.ID {
			continue
		}
		ob, _ := buildingByID(id)
		if r.overlaps(ob.rectAt(o)) {
			return ErrFarmSpotTaken
		}
	}
	return nil
}

// placeSpot: chỗ sẽ đặt công trình b (nil: chỗ mặc định) sau khi kiểm tra.
func (op *farmOp) placeSpot(userID int64, b buildingDef, want *Spot) (*Spot, error) {
	if !b.movable() {
		return nil, nil // công trình cố định: bỏ qua chỗ gửi lên
	}
	s := b.Default
	if want != nil {
		s = *want
	}
	others, err := loadSpots(op.ctx, op.tx, userID)
	if err != nil {
		return nil, err
	}
	if err := checkSpot(b, s, others); err != nil {
		return nil, err
	}
	return &s, nil
}

// FarmMoveBuilding: dời công trình tự do id (đã xây) tới chỗ s.
func (s *Store) FarmMoveBuilding(ctx context.Context, userID int64, id string, to Spot) (FarmResult, error) {
	res := FarmResult{OwnerID: userID}
	var err error
	res.Wallets, err = s.farmTx(ctx, func(op *farmOp) error {
		b, ok := buildingByID(id)
		if !ok {
			return ErrFarmUnknownBuild
		}
		if !b.movable() {
			return ErrFarmNotMovable
		}
		built, err := loadBuildings(ctx, op.tx, userID)
		if err != nil {
			return err
		}
		if built[b.ID] == 0 {
			return farmErr("Chưa xây %s.", b.Name)
		}
		spot, err := op.placeSpot(userID, b, &to)
		if err != nil {
			return err
		}
		_, err = op.tx.ExecContext(ctx, `UPDATE farm_buildings SET x = ?, y = ? WHERE user_id = ? AND building = ?`,
			spot.X, spot.Y, userID, b.ID)
		return err
	})
	return res, err
}
