package store_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"chat/model"
	"chat/store"
)

// farmEnv: store với đồng hồ và nguồn ngẫu nhiên điều khiển được.
type farmEnv struct {
	t    *testing.T
	ctx  context.Context
	st   *store.Store
	now  time.Time
	roll float64 // giá trị rand() trả về (< 0.25: có sâu; < 0.5: chó đuổi)
	mu   sync.Mutex
}

func newFarmEnv(t *testing.T) *farmEnv {
	e := &farmEnv{t: t, ctx: context.Background(), st: newStore(t), roll: 0.9,
		// 01:00 giờ VN: cả ngày còn dài cho các bước tua giờ trong cùng một ngày
		now: time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)}
	e.st.SetClock(func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now })
	e.st.SetRand(func() float64 { e.mu.Lock(); defer e.mu.Unlock(); return e.roll })
	return e
}

func (e *farmEnv) advance(d time.Duration) { e.mu.Lock(); e.now = e.now.Add(d); e.mu.Unlock() }
func (e *farmEnv) setRoll(r float64)       { e.mu.Lock(); e.roll = r; e.mu.Unlock() }

func (e *farmEnv) user(name string) model.User {
	e.t.Helper()
	u, err := e.st.CreateUser(e.ctx, name, name+"@x.vn", "h", name)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *farmEnv) balance(id int64) int64 {
	e.t.Helper()
	w, err := e.st.GetWallet(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return w.Balance
}

func (e *farmEnv) view(owner, viewer int64) store.FarmView {
	e.t.Helper()
	v, err := e.st.GetFarm(e.ctx, owner, viewer)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *farmEnv) plant(id int64, plot int, crop string) {
	e.t.Helper()
	if _, err := e.st.FarmPlant(e.ctx, id, plot, crop); err != nil {
		e.t.Fatalf("plant %s at %d: %v", crop, plot, err)
	}
}

func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func wantFarmErr(t *testing.T, err error) {
	t.Helper()
	var fe *store.FarmError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want FarmError", err)
	}
}

func TestFarmPlantHarvestEconomics(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")

	v := e.view(alice.ID, alice.ID)
	if !v.Me || v.Level != 1 || v.PlotCount != 6 || len(v.Plots) != 6 || v.Plots[0].State != "empty" ||
		v.NextPlot == nil || v.NextPlot.Price != 400 || v.NextPlot.Level != 2 || v.Wallet == nil ||
		len(v.Quests) != 4 || v.Chest == nil || v.Log == nil || v.StealsLeft != 5 || v.DogUntil != nil ||
		v.NextLevelXP == nil || *v.NextLevelXP != 30 || len(v.Crops) != 8 {
		t.Fatalf("fresh farm: %+v", v)
	}

	_, err := e.st.FarmPlant(e.ctx, alice.ID, 0, "ca_chua")
	wantFarmErr(t, err) // cần cấp 2
	_, err = e.st.FarmPlant(e.ctx, alice.ID, 0, "xoai")
	wantErr(t, err, store.ErrFarmUnknownCrop)
	_, err = e.st.FarmPlant(e.ctx, alice.ID, 6, "rau_muong")
	wantErr(t, err, store.ErrFarmBadPlot)

	// không sâu: bán đủ giá
	e.plant(alice.ID, 0, "rau_muong")
	if b := e.balance(alice.ID); b != 990 {
		t.Fatalf("after seed: %d", b)
	}
	_, err = e.st.FarmPlant(e.ctx, alice.ID, 0, "rau_muong")
	wantErr(t, err, store.ErrFarmPlotBusy)
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	wantErr(t, err, store.ErrFarmNotRipe)
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 1, false)
	wantErr(t, err, store.ErrFarmEmptyPlot)
	e.advance(2*time.Minute + 30*time.Second)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.State != "growing" || p.Progress != 0.5 || p.Bug || p.CanHarvest {
		t.Fatalf("half grown: %+v", p)
	}
	e.advance(2*time.Minute + 30*time.Second)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.State != "ripe" || p.Progress != 1 || !p.CanHarvest || p.CanSteal {
		t.Fatalf("ripe: %+v", p)
	}
	// thu hoạch không trả xu: 3 đơn vị vào kho, cộng XP
	res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	if err != nil || res.Gained == nil || res.Gained.Xu != 0 || res.Gained.XP != 2 || res.Gained.LevelUp != 0 ||
		fmt.Sprint(res.Gained.Items) != "map[rau_muong:3]" {
		t.Fatalf("harvest: %v %+v", err, res.Gained)
	}
	if b := e.balance(alice.ID); b != 990 {
		t.Fatalf("after harvest: %d", b)
	}
	if len(res.Wallets) != 0 {
		t.Fatalf("changed wallets: %+v", res.Wallets)
	}
	v = e.view(alice.ID, alice.ID)
	if v.XP != 2 || v.Plots[0].State != "empty" || v.Storage.Used != 3 || v.Storage.Stock["rau_muong"] != 3 {
		t.Fatalf("after harvest view: xp %d plot %+v storage %+v", v.XP, v.Plots[0], v.Storage)
	}

	// có sâu (lộ ra từ 50%), không bắt: −30%; bị trộm thì trừ thêm phần bị trộm
	e.setRoll(0.1)
	e.plant(alice.ID, 0, "ca_rot") // 30 xu, 15 phút
	e.setRoll(0.9)
	e.advance(7 * time.Minute)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.Bug {
		t.Fatalf("bug visible before 50%%: %+v", p)
	}
	e.advance(time.Minute)
	if p := e.view(alice.ID, bob.ID).Plots[0]; !p.Bug || !p.CanBug {
		t.Fatalf("bug hidden after 50%%: %+v", p)
	}
	e.advance(7 * time.Minute)
	if res, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0); err != nil || res.Gained.Items["ca_rot"] != 1 || *res.Dog {
		t.Fatalf("steal: %v %+v", err, res)
	}
	res, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	if err != nil || res.Gained.Items["ca_rot"] != 1 { // còn sâu 2, bị trộm 1
		t.Fatalf("buggy stolen harvest: %v %+v", err, res.Gained)
	}

	// bắt sâu (chủ tự bắt: không thưởng) thì bán đủ giá
	e.setRoll(0.1)
	e.plant(alice.ID, 0, "rau_muong")
	e.setRoll(0.9)
	_, err = e.st.FarmBug(e.ctx, alice.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmNoBug) // chưa lộ
	e.advance(3 * time.Minute)
	if res, err := e.st.FarmBug(e.ctx, alice.ID, alice.ID, 0); err != nil || res.Gained != nil || res.Log != nil {
		t.Fatalf("owner bug: %v %+v", err, res)
	}
	_, err = e.st.FarmBug(e.ctx, alice.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmNoBug)
	e.advance(2 * time.Minute)
	if res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, true); err != nil || res.Gained.Items["rau_muong"] != 3 {
		t.Fatalf("harvest after bug caught: %v %+v", err, res.Gained)
	}

	// harvest all: gộp nhiều ô, bỏ qua ô chưa chín
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmNothingRipe)
	e.plant(alice.ID, 0, "rau_muong")
	e.plant(alice.ID, 1, "rau_muong")
	e.plant(alice.ID, 2, "ca_rot")
	e.advance(5 * time.Minute)
	res, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
	if err != nil || res.Gained.Xu != 0 || res.Gained.XP != 4 || fmt.Sprint(res.Gained.Items) != "map[rau_muong:6]" {
		t.Fatalf("harvest all: %v %+v", err, res.Gained)
	}
	if p := e.view(alice.ID, alice.ID).Plots; p[0].State != "empty" || p[1].State != "empty" || p[2].State != "growing" {
		t.Fatalf("after harvest all: %+v", p)
	}

	// không đủ xu mua hạt
	e.st.TransferCoins(e.ctx, alice.ID, bob.ID, e.balance(alice.ID)-5, "")
	_, err = e.st.FarmPlant(e.ctx, alice.ID, 3, "rau_muong")
	wantErr(t, err, store.ErrInsufficientBalance)
	if b := e.balance(alice.ID); b != 5 {
		t.Fatalf("failed plant changed balance: %d", b)
	}
}

func TestFarmWatering(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol, dave := e.user("alice"), e.user("bob"), e.user("carol"), e.user("dave")
	e.plant(alice.ID, 0, "ca_rot") // 900s, mỗi lần tưới sớm 90s
	planted := e.now

	if _, err := e.st.FarmWater(e.ctx, alice.ID, alice.ID, 0); err != nil {
		t.Fatal(err)
	}
	_, err := e.st.FarmWater(e.ctx, alice.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmWatered) // chủ chỉ tưới 1 lần
	res, err := e.st.FarmWater(e.ctx, bob.ID, alice.ID, 0)
	if err != nil || res.Gained == nil || res.Gained.Xu != 2 || res.Gained.XP != 1 || res.Log == nil ||
		res.Log.Action != "water" || res.Log.Actor.ID != bob.ID || res.Log.Crop != "ca_rot" {
		t.Fatalf("bob water: %v %+v", err, res)
	}
	if b := e.balance(bob.ID); b != 1002 {
		t.Fatalf("bob reward: %d", b)
	}
	if p := e.view(alice.ID, carol.ID).Plots[0]; !p.CanWater || p.Waters != 2 {
		t.Fatalf("carol can water: %+v", p)
	}
	if _, err := e.st.FarmWater(e.ctx, carol.ID, alice.ID, 0); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmWater(e.ctx, dave.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmWaterMax)
	if p := e.view(alice.ID, dave.ID).Plots[0]; p.CanWater || p.Waters != 3 {
		t.Fatalf("dave after max: %+v", p)
	}
	want := planted.Add(900*time.Second - 3*90*time.Second).UTC().Format(time.RFC3339)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.ReadyAt != want {
		t.Fatalf("ready_at = %s, want %s", p.ReadyAt, want)
	}
	v := e.view(alice.ID, alice.ID)
	if v.Log == nil || len(*v.Log) != 2 || (*v.Log)[0].Actor.ID != carol.ID {
		t.Fatalf("log: %+v", v.Log)
	}

	// tưới không làm chín sớm hơn hiện tại; chín rồi thì không tưới được
	e.plant(alice.ID, 1, "rau_muong") // 300s, mỗi lần 30s
	e.advance(290 * time.Second)
	if _, err := e.st.FarmWater(e.ctx, bob.ID, alice.ID, 1); err != nil {
		t.Fatal(err)
	}
	if p := e.view(alice.ID, alice.ID).Plots[1]; p.State != "ripe" || p.ReadyAt != e.now.UTC().Format(time.RFC3339) {
		t.Fatalf("water near ripe: %+v", p)
	}
	_, err = e.st.FarmWater(e.ctx, carol.ID, alice.ID, 1)
	wantErr(t, err, store.ErrFarmNotGrowing)
	_, err = e.st.FarmWater(e.ctx, carol.ID, alice.ID, 5)
	wantErr(t, err, store.ErrFarmEmptyPlot)
}

func TestFarmStealLimits(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob, carol, dave := e.user("alice"), e.user("bob"), e.user("carol"), e.user("dave")
	for i := range 6 {
		e.plant(alice.ID, i, "rau_muong") // bán 14: trộm ceil(1.4) = 2
	}
	_, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmNotRipe)
	e.advance(5 * time.Minute)

	_, err = e.st.FarmSteal(e.ctx, alice.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmOwnSteal)
	res, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0)
	if err != nil || res.Gained.Xu != 0 || fmt.Sprint(res.Gained.Items) != "map[rau_muong:1]" || res.Dog == nil || *res.Dog ||
		res.Log.Action != "steal" || res.Log.Xu != 0 || res.Log.Crop != "rau_muong" {
		t.Fatalf("steal: %v %+v", err, res)
	}
	if b := e.balance(bob.ID); b != 1000 {
		t.Fatalf("thief balance: %d", b)
	}
	if st := e.view(bob.ID, bob.ID).Storage; st.Stock["rau_muong"] != 1 || st.Used != 1 {
		t.Fatalf("thief storage: %+v", st)
	}
	_, err = e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmStolen) // mỗi người một vụ một lần
	if _, err := e.st.FarmSteal(e.ctx, carol.ID, alice.ID, 0); err != nil {
		t.Fatal(err)
	}
	_, err = e.st.FarmSteal(e.ctx, dave.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmStealMax) // mỗi vụ bị trộm tối đa 2 lần
	if p := e.view(alice.ID, dave.ID).Plots[0]; p.CanSteal || p.Steals != 2 || p.Stolen != 2 {
		t.Fatalf("plot after 2 steals: %+v", p)
	}

	// 5 lần / ngày / người (tổng mọi nông trại)
	for i := 1; i <= 4; i++ {
		if _, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, i); err != nil {
			t.Fatalf("steal %d: %v", i, err)
		}
	}
	if v := e.view(alice.ID, bob.ID); v.StealsLeft != 0 || v.Plots[5].CanSteal {
		t.Fatalf("bob after 5 steals: left %d plot %+v", v.StealsLeft, v.Plots[5])
	}
	_, err = e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 5)
	wantErr(t, err, store.ErrFarmStealDaily)
	// sang ngày mới (giờ VN) thì được trộm tiếp
	e.advance(24 * time.Hour)
	e.plant(carol.ID, 0, "rau_muong")
	e.advance(5 * time.Minute)
	if _, err := e.st.FarmSteal(e.ctx, bob.ID, carol.ID, 0); err != nil {
		t.Fatalf("steal next day: %v", err)
	}
}

func TestFarmStealRace(t *testing.T) {
	e := newFarmEnv(t)
	owner := e.user("owner")
	e.plant(owner.ID, 0, "rau_muong")
	e.advance(5 * time.Minute)
	var thieves []int64
	for i := range 6 {
		thieves = append(thieves, e.user(fmt.Sprintf("thief%d", i)).ID)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, id := range thieves {
		wg.Go(func() {
			if _, err := e.st.FarmSteal(e.ctx, id, owner.ID, 0); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	// thu hoạch song song: chỉ một lần thành công
	harvests := 0
	for range 3 {
		wg.Go(func() {
			if _, err := e.st.FarmHarvest(e.ctx, owner.ID, 0, false); err == nil {
				mu.Lock()
				harvests++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if ok > 2 || harvests != 1 {
		t.Fatalf("steals %d (max 2), harvests %d (want 1)", ok, harvests)
	}
}

func TestFarmDog(t *testing.T) {
	e := newFarmEnv(t)
	e.at(time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)) // test tua 3 ngày: tránh mùa Noel (không trộm được)
	alice, bob, carol := e.user("alice"), e.user("bob"), e.user("carol")
	if _, err := e.st.FarmBuyDog(e.ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if b := e.balance(alice.ID); b != 850 {
		t.Fatalf("after dog: %d", b)
	}
	v := e.view(alice.ID, alice.ID)
	if v.DogUntil == nil || *v.DogUntil != e.now.Add(24*time.Hour).UTC().Format(time.RFC3339) {
		t.Fatalf("dog_until: %v", v.DogUntil)
	}
	e.st.FarmBuyDog(e.ctx, alice.ID)
	if _, err := e.st.FarmBuyDog(e.ctx, alice.ID); err != nil { // 72h
		t.Fatal(err)
	}
	_, err := e.st.FarmBuyDog(e.ctx, alice.ID)
	wantErr(t, err, store.ErrFarmDogMax)
	if b := e.balance(alice.ID); b != 1000-3*150 {
		t.Fatalf("after 3 dogs: %d", b)
	}

	e.plant(alice.ID, 0, "rau_muong")
	e.plant(alice.ID, 1, "rau_muong")
	e.advance(5 * time.Minute)
	before := e.balance(alice.ID)
	e.setRoll(0.1) // chó đuổi
	res, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0)
	if err != nil || res.Dog == nil || !*res.Dog || res.Gained.Xu != -10 || res.Log.Action != "dog" || res.Log.Xu != 10 {
		t.Fatalf("dog chase: %v %+v", err, res)
	}
	if e.balance(bob.ID) != 990 || e.balance(alice.ID) != before+10 || len(res.Wallets) != 2 {
		t.Fatalf("dog transfer: bob %d alice %d wallets %d", e.balance(bob.ID), e.balance(alice.ID), len(res.Wallets))
	}
	if p := e.view(alice.ID, bob.ID).Plots[0]; p.Steals != 0 || p.Stolen != 0 || p.CanSteal {
		t.Fatalf("plot after chase: %+v", p)
	}
	// kẻ trộm chỉ còn 4 xu: mất hết 4
	e.st.TransferCoins(e.ctx, carol.ID, bob.ID, 996, "")
	res, err = e.st.FarmSteal(e.ctx, carol.ID, alice.ID, 1)
	if err != nil || !*res.Dog || res.Gained.Xu != -4 || e.balance(carol.ID) != 0 {
		t.Fatalf("broke thief: %v %+v balance %d", err, res.Gained, e.balance(carol.ID))
	}
	// chó không đuổi (roll ≥ 0.5): trộm bình thường
	e.setRoll(0.7)
	if res, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 1); err != nil || *res.Dog || res.Gained.Items["rau_muong"] != 1 {
		t.Fatalf("lucky thief: %v %+v", err, res)
	}
	// hết hạn chó: không đuổi nữa
	e.plant(alice.ID, 2, "rau_muong")
	e.advance(73 * time.Hour)
	e.setRoll(0.1)
	if v := e.view(alice.ID, alice.ID); v.DogUntil != nil {
		t.Fatalf("dog still on: %v", *v.DogUntil)
	}
	e.plant(alice.ID, 3, "rau_muong")
	e.advance(5 * time.Minute)
	if res, err := e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 3); err != nil || *res.Dog {
		t.Fatalf("steal without dog: %v %+v", err, res)
	}
}

func TestFarmWitherAndClear(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.plant(alice.ID, 0, "rau_muong")
	_, err := e.st.FarmClear(e.ctx, alice.ID, 0)
	wantErr(t, err, store.ErrFarmNotWithered)
	_, err = e.st.FarmClear(e.ctx, alice.ID, 1)
	wantErr(t, err, store.ErrFarmEmptyPlot)
	e.advance(5*time.Minute + 12*time.Hour - time.Second)
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.State != "ripe" {
		t.Fatalf("just before wither: %+v", p)
	}
	e.advance(time.Second)
	p := e.view(alice.ID, alice.ID).Plots[0]
	if p.State != "withered" || p.CanHarvest || p.Progress != 1 || p.WitherAt != e.now.UTC().Format(time.RFC3339) {
		t.Fatalf("withered: %+v", p)
	}
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, false)
	wantErr(t, err, store.ErrFarmWithered)
	_, err = e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
	wantErr(t, err, store.ErrFarmNothingRipe)
	_, err = e.st.FarmSteal(e.ctx, bob.ID, alice.ID, 0)
	wantErr(t, err, store.ErrFarmWithered)
	_, err = e.st.FarmPlant(e.ctx, alice.ID, 0, "rau_muong")
	wantErr(t, err, store.ErrFarmWithered)
	bal := e.balance(alice.ID)
	if _, err := e.st.FarmClear(e.ctx, alice.ID, 0); err != nil {
		t.Fatal(err)
	}
	if p := e.view(alice.ID, alice.ID).Plots[0]; p.State != "empty" || e.balance(alice.ID) != bal {
		t.Fatalf("after clear: %+v", p)
	}
	e.plant(alice.ID, 0, "rau_muong")
}

func TestFarmLevelUpAndExpand(t *testing.T) {
	e := newFarmEnv(t)
	alice := e.user("alice")
	_, err := e.st.FarmExpand(e.ctx, alice.ID)
	wantFarmErr(t, err) // cần cấp 2

	// 6 ô cà rốt × 4 XP = 24; vụ thứ hai vượt 30 XP: lên cấp 2, thưởng 40 xu
	round := func() store.FarmResult {
		for i := range 6 {
			e.plant(alice.ID, i, "ca_rot")
		}
		e.advance(15 * time.Minute)
		res, err := e.st.FarmHarvest(e.ctx, alice.ID, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := round(); res.Gained.LevelUp != 0 || res.Gained.XP != 24 {
		t.Fatalf("round 1: %+v", res.Gained)
	}
	before := e.balance(alice.ID)
	res := round()
	if res.Gained.LevelUp != 2 || res.Gained.Xu != 0 || res.Gained.Items["ca_rot"] != 18 || len(res.Wallets) != 1 {
		t.Fatalf("round 2: %+v", res.Gained)
	}
	if got := e.balance(alice.ID); got != before-120+40 {
		t.Fatalf("level-up reward: %d, want %d", got, before-120+40)
	}
	v := e.view(alice.ID, alice.ID)
	if v.Level != 2 || v.XP != 48 || v.LevelXP != 30 || *v.NextLevelXP != 80 || v.Storage.Stock["ca_rot"] != 36 {
		t.Fatalf("level view: %+v", v)
	}
	before = e.balance(alice.ID)
	if _, err := e.st.FarmExpand(e.ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	v = e.view(alice.ID, alice.ID)
	if v.PlotCount != 9 || len(v.Plots) != 9 || e.balance(alice.ID) != before-400 || v.NextPlot.Price != 1000 || v.NextPlot.Level != 4 {
		t.Fatalf("after expanding: count %d next %+v balance %d", v.PlotCount, v.NextPlot, e.balance(alice.ID))
	}
	e.plant(alice.ID, 8, "ca_chua") // ô mới dùng được, cà chua đã mở ở cấp 2
	_, err = e.st.FarmExpand(e.ctx, alice.ID)
	wantFarmErr(t, err) // bậc 3 cần cấp 4
	if v := e.view(alice.ID, alice.ID); v.PlotCount != 9 {
		t.Fatalf("expanded without level: %d", v.PlotCount)
	}
}

func TestFarmAutoRepayWhileInDebt(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	e.st.TransferCoins(e.ctx, alice.ID, bob.ID, 950, "")
	if _, err := e.st.TakeLoan(e.ctx, alice.ID); err != nil { // 350 / 360
		t.Fatal(err)
	}
	e.plant(alice.ID, 0, "ca_rot") // −20 → 330
	e.advance(15 * time.Minute)
	e.st.FarmHarvest(e.ctx, alice.ID, 0, false) // 3 cà rốt vào kho, chưa có xu
	if w, _ := e.st.GetWallet(e.ctx, alice.ID); w.Balance != 330 || w.Debt != 360 {
		t.Fatalf("harvest paid coins: %+v", w)
	}
	e.st.FarmSell(e.ctx, alice.ID, "", 0, true) // 3 × 10 = +30, trả nợ 15 → 345 / 345
	w, _ := e.st.GetWallet(e.ctx, alice.ID)
	if w.Balance != 345 || w.Debt != 345 {
		t.Fatalf("auto repay: %+v", w)
	}
}

func TestFarmQuestsAndChest(t *testing.T) {
	e := newFarmEnv(t)
	me := e.user("farmer")
	n := []model.User{e.user("n1"), e.user("n2"), e.user("n3")}

	// đơn hàng sinh lúc xem lần đầu: roll 0 → mỗi đơn 1 rau muống (thưởng 7 xu, 1 XP)
	e.setRoll(0)
	// chọn ngày có bộ nhiệm vụ không gồm plant_rare (cần cấp 3) để làm xong được cả 4
	var quests []store.Quest
	for range 60 {
		quests = e.view(me.ID, me.ID).Quests
		rare := false
		ids := map[string]bool{}
		for _, q := range quests {
			rare = rare || q.ID == "plant_rare"
			ids[q.ID] = true
		}
		if len(quests) != 4 || len(ids) != 4 {
			t.Fatalf("quests: %+v", quests)
		}
		if !rare {
			break
		}
		e.advance(24 * time.Hour)
	}
	e.setRoll(0.9)
	if again := e.view(me.ID, me.ID).Quests; fmt.Sprint(again) != fmt.Sprint(quests) {
		t.Fatalf("quests not stable: %v vs %v", again, quests)
	}
	_, err := e.st.FarmClaimQuest(e.ctx, me.ID, quests[0].ID)
	wantErr(t, err, store.ErrFarmQuestNotDone)
	_, err = e.st.FarmClaimQuest(e.ctx, me.ID, "chest")
	wantErr(t, err, store.ErrFarmQuestNotDone)
	_, err = e.st.FarmClaimQuest(e.ctx, me.ID, "khong_co")
	wantErr(t, err, store.ErrFarmNoQuest)

	// làm đủ mọi loại việc (trừ plant_rare)
	for range 11 { // harvest ≥ 5, plant ≥ 6; 33 rau muống vào kho
		e.plant(me.ID, 0, "rau_muong")
		e.advance(5 * time.Minute)
		if _, err := e.st.FarmHarvest(e.ctx, me.ID, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	e.setRoll(0.1) // hàng xóm trồng cây có sâu
	for _, u := range n {
		e.plant(u.ID, 0, "rau_muong")
		e.plant(u.ID, 1, "rau_muong")
	}
	e.setRoll(0.9)
	for _, u := range n {
		if _, err := e.st.FarmWater(e.ctx, me.ID, u.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	e.advance(3 * time.Minute)
	for _, u := range n[:2] {
		if _, err := e.st.FarmBug(e.ctx, me.ID, u.ID, 1); err != nil {
			t.Fatal(err)
		}
	}
	e.advance(3 * time.Minute)
	for _, u := range n[:2] {
		if _, err := e.st.FarmSteal(e.ctx, me.ID, u.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range n {
		if _, err := e.st.FarmVisit(e.ctx, me.ID, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	e.st.FarmVisit(e.ctx, me.ID, n[0].ID) // ghé lại không tính thêm
	// giao 2 đơn (order), rồi bán hết kho: 33 + 2 trộm − 2 giao = 33 × 5 = 165 (+ 14 tiền đơn) ≥ 150 (earn)
	for _, o := range e.view(me.ID, me.ID).Orders[:2] {
		if _, err := e.st.FarmDeliverOrder(e.ctx, me.ID, o.ID); err != nil {
			t.Fatalf("deliver %+v: %v", o, err)
		}
	}
	if res, err := e.st.FarmSell(e.ctx, me.ID, "", 0, true); err != nil || res.Gained.Xu != 165 {
		t.Fatalf("sell all: %v %+v", err, res.Gained)
	}

	v := e.view(me.ID, me.ID)
	for _, q := range v.Quests {
		if !q.Done || q.Progress != q.Goal || q.Claimed {
			t.Fatalf("quest not done: %+v", q)
		}
	}
	if !v.Chest.Ready || v.Chest.Claimed || v.Chest.Xu != 80 || v.Chest.XP != 20 {
		t.Fatalf("chest: %+v", v.Chest)
	}
	xp := v.XP
	bal := e.balance(me.ID)
	var sumXu, sumXP int64
	for _, q := range v.Quests {
		res, err := e.st.FarmClaimQuest(e.ctx, me.ID, q.ID)
		if err != nil || res.Gained.Xu != q.Xu || res.Gained.XP != q.XP {
			t.Fatalf("claim %s: %v %+v", q.ID, err, res.Gained)
		}
		sumXu += q.Xu
		sumXP += q.XP
		_, err = e.st.FarmClaimQuest(e.ctx, me.ID, q.ID)
		wantErr(t, err, store.ErrFarmQuestClaimed)
	}
	if res, err := e.st.FarmClaimQuest(e.ctx, me.ID, "chest"); err != nil || res.Gained.Xu != 80 || res.Gained.XP != 20 {
		t.Fatalf("claim chest: %v %+v", err, res)
	}
	_, err = e.st.FarmClaimQuest(e.ctx, me.ID, "chest")
	wantErr(t, err, store.ErrFarmQuestClaimed)
	v = e.view(me.ID, me.ID)
	if !v.Chest.Claimed || v.XP != xp+sumXP+20 {
		t.Fatalf("after claims: chest %+v xp %d want %d", v.Chest, v.XP, xp+sumXP+20)
	}
	for _, q := range v.Quests {
		if !q.Claimed {
			t.Fatalf("quest not marked claimed: %+v", q)
		}
	}
	// thưởng lên cấp (nếu có) cộng thêm ngoài thưởng nhiệm vụ
	levelBonus := int64(0)
	for lv := levelOf(xp) + 1; lv <= v.Level; lv++ {
		levelBonus += int64(20 * lv)
	}
	if got := e.balance(me.ID); got != bal+sumXu+80+levelBonus {
		t.Fatalf("balance after claims: %d, want %d", got, bal+sumXu+80+levelBonus)
	}

	// ngày mới: nhiệm vụ mới, tiến độ về 0
	e.advance(24 * time.Hour)
	for _, q := range e.view(me.ID, me.ID).Quests {
		if q.Progress != 0 || q.Claimed {
			t.Fatalf("new day quest: %+v", q)
		}
	}
}

func levelOf(xp int64) int {
	lv := 1
	for i, need := range store.LevelXP {
		if xp >= need {
			lv = i + 1
		}
	}
	return lv
}

func TestFarmNeighborsAndLeaderboard(t *testing.T) {
	e := newFarmEnv(t)
	alice, bob := e.user("alice"), e.user("bob")
	bot := e.user("tin_bot")
	ai := e.user("ai")
	e.plant(bob.ID, 0, "rau_muong")
	e.plant(bob.ID, 1, "rau_muong")
	e.advance(5 * time.Minute)
	e.plant(bob.ID, 2, "rau_muong")
	e.st.FarmHarvest(e.ctx, bob.ID, 1, false)
	e.plant(bob.ID, 1, "ca_rot")

	list, err := e.st.FarmNeighbors(e.ctx, alice.ID)
	if err != nil || len(list) != 1 || list[0].User.ID != bob.ID {
		t.Fatalf("neighbors: %v %+v", err, list)
	}
	if nb := list[0]; nb.Ripe != 1 || nb.Stealable != 1 || nb.NeedsHelp != 2 || nb.XP != 2 || nb.Level != 1 || nb.Dog {
		t.Fatalf("neighbor bob: %+v", nb)
	}
	if _, err := e.st.FarmVisit(e.ctx, alice.ID, bot.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("visit bot farm: %v", err)
	}
	if _, err := e.st.FarmVisit(e.ctx, alice.ID, ai.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("visit ai farm: %v", err)
	}
	_, err = e.st.FarmWater(e.ctx, alice.ID, bot.ID, 0)
	wantErr(t, err, store.ErrNotFound)
	v, err := e.st.FarmVisit(e.ctx, alice.ID, bob.ID)
	if err != nil || v.Me || v.Wallet != nil || v.Quests != nil || v.Log != nil || v.Chest != nil || v.StealsLeft != 5 {
		t.Fatalf("visit bob: %v %+v", err, v)
	}

	board, err := e.st.FarmLeaderboard(e.ctx)
	if err != nil || len(board) != 2 || board[0].User.ID != bob.ID || board[0].XP != 2 {
		t.Fatalf("leaderboard: %v %+v", err, board)
	}
}
