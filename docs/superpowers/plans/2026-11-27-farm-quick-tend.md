# Farm Quick Tend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One button per animal building (coop / pasture) that collects every ready product, feeds every hungry animal in it, and auto-buys missing feed at market price, in one server transaction.

**Architecture:** New store method `FarmTend` reuses the collect/feed internals of `FarmCollect`/`FarmFeed` (extracted into shared `farmOp` helpers), filtered by `AnimalKind.Building`. Auto-bought feed is paid with `op.spend` and given straight to the animal (never added to storage). New route `POST /api/farm/buildings/{id}/tend`. Desktop: a pure `tendPlan()` preview in `lib/farm.ts` drives a sign over each building in `FarmWorld`.

**Tech Stack:** Go (net/http, SQL via `farmTx`), React 19 + TypeScript (desktop, Vite). No frontend test runner: frontend is verified with `tsc`.

**Spec:** `docs/superpowers/specs/2026-11-27-farm-quick-tend-design.md`

## Global Constraints

- Everything in one `s.farmTx` call: never charge xu without feeding the animal it paid for.
- Auto-bought feed bypasses storage: a full storage must not block buying/feeding.
- Buy only if `it.BuyPrice > 0`, `levelFor(f.XP) >= it.Level`, and wallet can afford it. Check affordability **before** calling `op.spend` (read how `spend` in `store/farm.go`/wallet code treats balance/debt). Never rely on `spend` failing mid-transaction.
- Wallet note per bought item: `fmt.Sprintf("Mua %d %s cho vật nuôi", qty, it.Name)`, one `op.spend` per item id.
- Per-animal click behaviour (`onAnimal` in `Farm.tsx`) stays unchanged.
- User-facing copy is Vietnamese, in the codebase's tone.

## Review Focus

1. Storage nearly full: ready products capped to room (like `FarmCollect` all); feeding still works and buys feed even with 0 room. → Task 1 test `TestFarmTendStorageFull`.
2. Partial affordability: wallet covers feed for only some animals → feed those, charge exactly that, report `hungry` > 0, no error. → Task 1 test `TestFarmTendAutoBuy`.
3. Stock partially covers an animal's `FeedQty` (cừu needs 2 cà rốt, stock has 1) → take 1 from stock, buy 1. → Task 1 test `TestFarmTendAutoBuy`.
4. Tending coop must not touch pasture animals and vice versa. → Task 1 test `TestFarmTendCollectAndFeed`.
5. Preview label disagreeing with server (e.g. animal `producing` with `ready_at` already passed counts as ready) → Task 3 `tendPlan` uses the same readiness rule as `onAnimal`.

---

### Task 1: Store — `FarmTend`

**Files:**
- Modify: `store/farm_v2.go` (extract helpers from `FarmFeed` ~L646 and `FarmCollect` ~L711; add `FarmTend`; add errors next to L167-180)
- Modify: `store/farm.go:230` (`Gained` fields)
- Test: `store/farm_tend_test.go` (new, package `store_test`, reuse `newFarmEnv`, `e.user`, `e.setXP`, `e.setStock`, `e.advance`, `e.view`, `e.storage`, `wantErr`, `wantMsg` from `farm_v2_test.go`)

**Interfaces:**
- Produces:
  - `func (s *Store) FarmTend(ctx context.Context, userID int64, building string) (FarmResult, error)`
  - `Gained` new fields: `Fed int \`json:"fed,omitempty"\``, `Bought map[string]int64 \`json:"bought,omitempty"\``, `Hungry int \`json:"hungry,omitempty"\`` (still hungry after tending). Xu spent is reported as negative `Gained.Xu` (= −total spent).
  - `ErrFarmNothingTend = &FarmError{"Chuồng này không có gì để thu hay cho ăn."}`
  - `ErrFarmNoXuFeed = &FarmError{"Không đủ xu mua thức ăn cho vật nuôi."}`

**Behaviour (in order, inside one `farmTx`):**
1. `building` must be `coop` or `pasture` (`buildingByID` + not kitchen), else `ErrFarmUnknownBuild`. Load animals, keep those with `a.Kind.Building == building`.
2. Collect: ready animals, capped by `op.storageRoom`, same effects as `FarmCollect` (stock, XP, level-up, any counters/quests it bumps). Extract a shared helper and make `FarmCollect` use it.
3. Feed every hungry animal (including the ones just collected), in animal order. For each: take `min(stock, FeedQty)` from stock, buy the rest if allowed and affordable (running balance tracked locally), then set `ready_at` as `FarmFeed` does. If the rest can't be bought, that animal stays hungry and its stock is not taken (decide buy-ability before taking stock). Extract the "feed one animal" update so `FarmFeed` shares it.
4. Charge: one `op.spend` per bought item with the note above.
5. Result: `Gained{Items: collected, XP, LevelUp, Fed, Bought, Hungry, Xu: -spent}`.
6. Errors only when nothing happened: no ready and no hungry → `ErrFarmNothingTend`; ready but no room and no hungry → `ErrFarmStorageFull`; hungry but none fed → the level message `"Cần đạt cấp %d mới mua được %s."` if level-blocked, else `ErrFarmNoXuFeed`.

- [ ] **Step 1: Write failing tests** in `store/farm_tend_test.go`:
  - `TestFarmTendCollectAndFeed`: alice XP for level 4+, coins via `TransferCoins` from bob, build coop + pasture, 2 gà + 1 bò, stock enough feed. Tend `coop` → `Fed == 2`, bò still hungry, `Xu == 0`, no `Bought`. Advance 20m; tend `coop` → `Items["trung"] == 2`, `Fed == 2` (re-fed right after collecting), both hens `producing`. Tend `coop` again immediately → `ErrFarmNothingTend`. `FarmTend(..., "kitchen")` and `"xyz"` → `ErrFarmUnknownBuild`.
  - `TestFarmTendAutoBuy`: 3 gà hungry, stock `rau_muong` 1 → `Fed == 3`, `Bought["rau_muong"] == 2`, `Xu == -2*BuyPrice(rau_muong)` (read the price from `e.view(...).Items`), wallet balance dropped by exactly that, stock `rau_muong == 0`, storage not increased. Cừu with stock `ca_rot` 1 → bought 1. Partial: set wallet so it affords one unit only (drain with `TransferCoins` to bob) with 3 hungry hens and no stock → `Fed == 1`, `Hungry == 2`, no error; then with 0 affordable → `ErrFarmNoXuFeed` and balance unchanged.
  - `TestFarmTendStorageFull`: hens ready, storage filled to cap via `setStock` → tend still succeeds: collects 0, `Fed` counts the hungry ones by buying feed. When all animals are ready (none hungry) and storage full → `ErrFarmStorageFull`.
  - `TestFarmTendLevelGate`: buy an animal whose feed item has `Level > 1` (pick from catalog; e.g. heo → dâu tây), then `setXP` below that item's level, empty stock → error message `"Cần đạt cấp %d mới mua được %s."` with that item. If no feed item has level > 1, drop this test and note it in the commit message.
  - Existing `TestFarmFeedAndCollect`, `TestFarmFeedQty` must keep passing (refactor guard).

- [ ] **Step 2: Run, expect FAIL** — `go test ./store -run 'TestFarmTend' -count=1` → compile error `FarmTend undefined`.
- [ ] **Step 3: Implement** helpers + `FarmTend` + `Gained` fields + errors as specified.
- [ ] **Step 4: Run, expect PASS** — `go test ./store -count=1` → `ok`.
- [ ] **Step 5: Commit** — `feat(farm): tend a whole coop/pasture with auto-bought feed (store)`.

### Task 2: HTTP route + desktop API client

**Files:**
- Modify: `controller/farm.go` (route doc comment block L16-44; handler `farmTend` near `farmCollect`)
- Modify: `controller/api.go:135-137` (register `POST /api/farm/buildings/{id}/tend` with `requireAuth`)
- Modify: `desktop/src/lib/api.ts:~620` and `desktop/src/lib/farm.ts:83` (`gained` type)
- Test: `controller/farm_tend_test.go` (new, package `controller_test`, follow `TestFarmV2API` setup: `newEnv`, `e.clock`, `e.register`, `e.do`)

**Interfaces:**
- Consumes: `store.FarmTend` (Task 1).
- Produces:
  - `func (a *API) farmTend(w, r)` → `a.farmRespond(w, r, res, err)` with `r.PathValue("id")`.
  - TS: `api.farmTend(building: "coop" | "pasture"): Promise<FarmView>`; `gained` gains `fed?: number; bought?: Record<string, number>; hungry?: number`.

- [ ] **Step 1: Failing test** `TestFarmTendAPI`: `POST /api/farm/buildings/xyz/tend` → 400 with the `ErrFarmUnknownBuild` message; with a built coop + 1 hungry hen (get there via the API as `TestFarmV2API` does, giving XP/coins the same way it does) → 200, response `gained.fed == 1` and `gained.bought` present when stock is empty.
- [ ] **Step 2: Run, expect FAIL** — `go test ./controller -run TestFarmTendAPI -count=1` → 404/405 status.
- [ ] **Step 3: Implement** handler, route, TS client + types.
- [ ] **Step 4: Verify** — `go test ./... -count=1` → all `ok`; `cd desktop && pnpm exec tsc --noEmit` → no errors.
- [ ] **Step 5: Commit** — `feat(farm): POST /api/farm/buildings/{id}/tend`.

### Task 3: Desktop UI — tend sign per building

**Files:**
- Modify: `desktop/src/lib/farm.ts` (add `tendPlan`)
- Modify: `desktop/src/components/FarmWorld.tsx` (`WorldHandlers.onTend`, new `TendSign` next to `BuildSign` ~L1835, render near the BuildSigns ~L1573)
- Modify: `desktop/src/components/Farm.tsx` (`onTend` wiring at `<FarmWorld` ~L647; toast; `act` okText)
- Modify: `desktop/src/styles.css` (`.iso-sign.is-tend` next to existing `.iso-sign` rules)

**Interfaces:**
- Consumes: `api.farmTend`, `gained.fed/bought/hungry` (Task 2).
- Produces:
  - `export type TendPlan = { collect: number; feed: number; buy: Record<string, number>; cost: number; hungryLeft: number }`
  - `export function tendPlan(view: FarmView, building: "coop" | "pasture", now: number): TendPlan | null` — mirrors server: ready = `state === "ready"` or `producing` with `ready_at <= now` (same rule as `onAnimal`); collect capped by `storage.cap - storage.used`; hungry = hungry + collected; walk hungry animals consuming a local copy of `storage.stock`, buying the shortfall at `item.buy_price` while `view.level >= item.level` and running cost ≤ `view.wallet?.balance` (if no wallet info, assume affordable); unfed ones → `hungryLeft`. Returns `null` when `collect === 0 && feed === 0`.
  - `WorldHandlers.onTend: (id: "coop" | "pasture") => void`.

- [ ] **Step 1: Implement `tendPlan`** in `lib/farm.ts`.
- [ ] **Step 2: `TendSign`** in `FarmWorld`: shown when `me && !placing && building built && plan`, positioned above the building rect (offset up so it doesn't cover animals), class `iso-click iso-sign is-tend`, greyed + no click when `busy`. Label parts joined by ` · `, omitted when zero: `🥚 Thu {collect}` (coop) / `🧺 Thu {collect}` (pasture), `🍽️ Cho ăn {feed}`, `mua {qty}{emoji}… {cost}🪙`. Widen the pill to fit the text (BuildSign is fixed 156px — compute width from label length or measure).
- [ ] **Step 3: Wire in `Farm.tsx`**: `onTend={(id) => void act(-1, () => api.farmTend(id), (f) => tendToast(f))}`; allow `act`'s `okText` to be `string | ((f: FarmView) => string | undefined)`. Toast copy: `Đã thu {n} sản phẩm, cho {fed} con ăn` + ` (mua thêm {qty}{emoji}…)` when bought; if `hungry > 0` show with `"bad"`: `Còn {hungry} con đói: không đủ xu hoặc chưa đủ cấp mua thức ăn.` Existing gained pops (items, negative xu, XP) already render via `act`.
- [ ] **Step 4: CSS** for `.is-tend` (distinct from build sign: e.g. green/harvest colour), check `make verify-css` passes.
- [ ] **Step 5: Verify** — `cd desktop && pnpm exec tsc --noEmit` → no errors; `make verify-css` → passes; `go test ./... -count=1` still `ok`.
- [ ] **Step 6: Commit** — `feat(farm-ui): one-tap tend sign on coop and pasture`.
