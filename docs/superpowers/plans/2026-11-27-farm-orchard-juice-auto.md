# Farm Part 5 — Orchard, Juicer, Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add fruit trees (plant once, harvest repeatedly), a juice press building with juice recipes, and one-tap / auto-mode automation.

**Architecture:** New `farm_trees` table + `store/farm_orchard.go`; crafting generalized per building (`Recipe.Building`, jobs keyed by building) with a new `juicer` building; `store/farm_auto.go` composes existing op-level helpers into `/api/farm/auto`. Desktop: orchard rendering in FarmWorld, a new "Vườn & Ép" tab, automation buttons + client auto-loop.

**Tech Stack:** Go + SQLite migrations (`database/migrations/NNNNNN_*.sql`), React 19 + TS desktop.

**Spec:** `docs/superpowers/specs/2026-11-27-farm-orchard-juice-auto-design.md` — all catalog values (trees table, juice recipes table, spot unlock levels, spot coordinates, juicer price/level/slots) are defined there; copy them verbatim.

## Global Constraints

- Branch `feat/farm-orchard-juice`, stacked on `feat/farm-unlimited-levels` (perks: `farmPerks`, `levelFor`, `sellValue` exist).
- New migration number: `000032` (latest is 000031). Every migration has a working `.down.sql`; existing data (kitchen jobs, buildings) survives up/down/up.
- Item kinds: `crop | product | fruit | goods`. `sellValue` bonus applies to crop, product, fruit — never goods. Fruits have `BuyPrice = 0`.
- Never spend xu without the thing it paid for (tx per action). Automation spends only: seed for the chosen crop, feed only when `buy_feed` is true, nothing else.
- Error copy Vietnamese, via `farmErr`/`FarmError`. New sentinel: `ErrFarmNothingAuto = &FarmError{"Chưa có việc gì để làm."}`.
- All new endpoints act only on the caller's own farm, registered with `requireAuth` in `controller/api.go`, documented in the route comment block in `controller/farm.go` and in `specs/FARM_SPEC.md` "Phần 5".
- Commit trailer: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never bypass commit signing; if it fails, leave staged and report.

## Review Focus

1. Storage nearly full on fruit harvest-all: harvest trees in spot order while the whole yield fits; partial room skips that tree; nothing fits → `ErrFarmStorageFull`. → Task 1 test.
2. Existing kitchen jobs after migration still collectable at the same slot/time. → Task 2 migration test.
3. `/auto` with `buy_feed=false` and empty feed stock: animals stay hungry, balance unchanged by feed. → Task 3 test.
4. `plant all` with xu for only 2 of 5 empty plots: plants 2, charges 2 seeds, no error. → Task 3 test.
5. Client auto-loop after an error (e.g. no xu for seeds): pauses 5 minutes, one toast, no request storm; never fires while `busy` or when viewing a neighbour. → Task 5 (manual reasoning + code review).

---

### Task 1: Store + API — Orchard

**Files:** Create `database/migrations/000032_farm_orchard.up.sql` / `.down.sql` (table `farm_trees(id INTEGER PK, user_id, spot INTEGER, tree TEXT, planted_at, ready_at, UNIQUE(user_id, spot))`; the juicer jobs change goes in Task 2's own migration 000033). Create `store/farm_orchard.go`, `store/farm_orchard_test.go`. Modify `store/farm_v2.go` (Items: fruit items after products, before goods; `sellValue` includes fruit; order pool gating), `store/farm_place.go` (blocked rect `{15.5, 2.5, 6.5, 4.8}` replacing the decorative-orchard rect), `store/farm.go` (FarmView fields), `controller/farm.go`, `controller/api.go`, `controller/farm_orchard_test.go`, `desktop/src/lib/api.ts` + `lib/farm.ts` (types + client calls only; UI is Task 4).

**Interfaces — Produces:**
- `type FruitTree struct { ID, Name, Emoji, Fruit, FruitName, FruitEmoji string; Price int64; Level int; MatureSec, CycleSec, FruitPrice, XP int64 }` + `var FruitTrees []FruitTree` (spec table). Fruit items get `Item{Kind: "fruit", Level: tree.Level, Price: FruitPrice, BuyPrice: 0}`.
- `func orchardSpots(level int) int` (0 below 5; 3/6/9/12 at 5/8/12/16).
- `const OrchardMaxSpots = 12`.
- `func (s *Store) FarmPlantTree(ctx, userID int64, spot int, treeID string) (FarmResult, error)`
- `func (s *Store) FarmHarvestTree(ctx, userID int64, spot int, all bool) (FarmResult, error)`
- `func (s *Store) FarmRemoveTree(ctx, userID int64, spot int) (FarmResult, error)`
- op-level helper used later by `/auto`: `func (op *farmOp) harvestTrees(userID int64, now int64) (items map[string]int64, xp int64, err error)` (all ready trees that fit).
- FarmView: `Trees []TreeView \`json:"trees"\`` (always, also for visitors), `TreeKinds []FruitTree \`json:"tree_kinds,omitempty"\`` (me only), `OrchardSpots int \`json:"orchard_spots"\``. `TreeView{Spot int; Tree string; State string /* growing|fruiting|ready */; ReadyAt string /* RFC3339 */ }`.
- Routes: `POST /api/farm/orchard/plant {spot, tree}`, `POST /api/farm/orchard/harvest {spot} | {all:true}`, `DELETE /api/farm/orchard/{spot}`.
- Errors: `ErrFarmSpotLocked = &FarmError{"Chỗ này chưa mở, lên cấp để mở thêm chỗ trồng cây."}`, `ErrFarmSpotUsed = &FarmError{"Chỗ này đã có cây."}`, `ErrFarmNoTree = &FarmError{"Chỗ này chưa có cây."}`, `ErrFarmTreeNotReady = &FarmError{"Cây chưa có quả."}`, `ErrFarmNoFruit = &FarmError{"Chưa có cây nào có quả để hái."}`, unknown tree → `farmErr("Không có loại cây này.")`, level → `farmErr("Cần đạt cấp %d mới trồng được %s.", ...)`.
- TS: `api.farmPlantTree(spot, tree)`, `api.farmHarvestTree(spot | "all")`, `api.farmRemoveTree(spot)`; types `FruitTree`, `TreeView`, FarmView `trees`, `tree_kinds?`, `orchard_spots`.

- [ ] **Step 1: Failing tests** `store/farm_orchard_test.go`:
  - `TestOrchardSpotsByLevel`: levels 4,5,7,8,12,16,40 → 0,3,3,6,9,12,12; planting at spot 3 at level 5 → `ErrFarmSpotLocked`; spot −1/12 → locked/invalid error.
  - `TestOrchardPlantHarvest`: level 5, plant `cam` at spot 0 → balance −300, view tree `growing`; at +2h·(100−8)/100 it's `ready`; harvest → `Items["cam"] == 3 + farmPerks(5).HarvestBonus` (=4), XP +4, state `fruiting`, next ready after `CycleSec·(100−8)/100`; harvest again early → `ErrFarmTreeNotReady`; plant on used spot → `ErrFarmSpotUsed`; tree level gate (`tao` at level 5) → level message.
  - `TestOrchardHarvestAllStorage`: 3 ready trees, storage room for 5 at yield 4 → harvests exactly 1 tree, others stay ready; room 0 → `ErrFarmStorageFull`; none ready → `ErrFarmNoFruit`.
  - `TestOrchardRemove`: remove → spot empty, no refund; remove empty → `ErrFarmNoTree`.
  - `TestOrchardFruitSellAndOrders`: fruit sells with sell bonus at level 11 (`sellValue`); fruit items have BuyPrice 0; with `SetRand` forcing picks, order pool contains `cam` only when a `cam` tree exists (assert via a helper or by generating many orders).
  - Controller `TestFarmOrchardAPI`: plant/harvest/remove happy path + 400 on locked spot; view JSON has `trees`, `orchard_spots`, `tree_kinds` (me) and no `tree_kinds` for visitor.
  - Migration: extend or mirror `TestFarmV2MigrationKeepsPartOneData` to step 000032 down and up.
- [ ] **Step 2: Run, expect FAIL** — `go test ./store ./controller -run 'Orchard' -count=1`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Verify** — `go test ./... -count=1`; `cd desktop && pnpm exec tsc --noEmit`.
- [ ] **Step 5: Commit** — `feat(farm): orchard with fruit trees (store + API)`.

### Task 2: Store + API — Juicer and per-building crafting

**Files:** Create `database/migrations/000033_farm_juicer.up.sql`/`.down.sql` (farm_jobs gets `building TEXT NOT NULL DEFAULT 'kitchen'`, key `(user_id, building, slot)`; SQLite needs a table rebuild — copy rows; down drops non-kitchen jobs and restores the old shape). Modify `store/farm_craft.go`, `store/farm_v2.go` (buildingDefs: juicer per spec, movable `Size{2,2}`, default spot that passes `checkSpot` and avoids the orchard rect — add a test asserting it), `store/farm.go` (view), `controller/*`, `desktop/src/lib/{api,farm}.ts` (types only), tests `store/farm_juicer_test.go`, `controller/farm_juicer_test.go`.

**Interfaces — Produces:**
- `Recipe.Building string \`json:"building"\`` (existing recipes `kitchen`; juice recipes per spec table, `juicer`). `const juicerID = "juicer"`.
- `FarmCraft(ctx, userID, recipeID string)` unchanged signature, uses the recipe's building (`ErrFarmNoJuicer = &FarmError{"Cần xây Máy ép nước trước."}`, busy → `farmErr("Các lò của %s đều đang bận.", buildingName)` for juicer; keep the kitchen's existing message text).
- `FarmCraftCollect(ctx, userID, building string, slot int, all bool)` — `all` with `building == ""` collects every building. Controller: `POST /api/farm/craft/collect {building?, slot} | {all:true, building?}` — `building` defaults to `kitchen` when `slot` is given (backward compatible).
- `func (s *Store) FarmCraftFill(ctx, userID int64, recipeID string) (FarmResult, error)` — fills every free slot of the recipe's building while ingredients last; 0 started → missing-ingredient error as FarmCraft gives. Route: `POST /api/farm/craft {recipe, fill: true}`.
- op-level helpers for `/auto`: `func (op *farmOp) collectJobs(userID int64, building string) (items map[string]int64, xp int64, err error)` and `func (op *farmOp) fillSlots(userID int64, recipeID string) (started int, err error)`.
- View: `Job.Building string \`json:"building"\``; recipes include `building`.
- Orders: goods pool includes kitchen recipes only if kitchen built, juicer recipes only if juicer built (still ≤1 goods per order).
- TS: `Job.building`, `Recipe.building`, `api.farmCraftFill(recipe)`, `api.farmCraftCollect(slot | "all", building?)`.

- [ ] **Step 1: Failing tests:**
  - `TestFarmJuicerMigrationKeepsKitchenJobs`: create a kitchen job at slot 1 on the pre-000033 schema (migrate to 000032, insert raw row), migrate up → job visible as `building:"kitchen", slot:1`, collectable; down then up works.
  - `TestFarmJuicer`: build juicer (level 6, price 800), craft `nuoc_cam` with 3 cam → job on juicer slot 0, kitchen slots unaffected; craft juice without juicer → `ErrFarmNoJuicer`; kitchen recipe still needs kitchen; collect all juicer → `nuoc_cam` +1, XP 8; upgrade → 4 slots.
  - `TestFarmCraftFill`: kitchen 2 free slots, stock for 3 salads → starts 2; stock for 1 → starts 1; stock 0 → error.
  - `TestFarmJuiceNoArbitrage`: every juice recipe whose ingredients all have `BuyPrice > 0` sells (`sellValue` at max perks) ≤ ingredient buy cost (expected vacuous except none — assert the loop runs over all recipes).
  - `TestFarmJuicerDefaultSpot`: default spot passes `checkSpot` on an empty farm and does not intersect the orchard rect.
  - `TestFarmOrdersJuiceGating`: juice items only in orders with juicer built.
  - Controller `TestFarmJuicerAPI`: build, craft fill, collect with/without `building`.
- [ ] **Step 2: Run, expect FAIL.** - [ ] **Step 3: Implement.** - [ ] **Step 4: Verify** `go test ./... -count=1`, tsc. - [ ] **Step 5: Commit** `feat(farm): juice press building and per-building crafting`.

### Task 3: Store + API — Plant all and /auto

**Files:** Create `store/farm_auto.go`, `store/farm_auto_test.go`, `controller/farm_auto_test.go`. Modify `store/farm.go` (plant-all; extract op-level `plantPlots`/`harvestPlots`/`clearWithered` helpers from FarmPlant/FarmHarvest/FarmClear so existing endpoints share them), `store/farm_v2.go` (FarmTend: extract op-level `tendBuilding(userID, building string, buyFeed bool)`; existing FarmTend = `buyFeed: true`), controller routes, `desktop/src/lib/{api,farm}.ts`.

**Interfaces — Produces:**
- `func (s *Store) FarmPlantAll(ctx, userID int64, cropID string) (FarmResult, error)` — route `POST /api/farm/plant {all:true, crop}` (existing `{plot, crop}` unchanged). Clears withered plots first, plants every empty plot index `< PlotCount` in order while xu last; `Gained.Planted int \`json:"planted,omitempty"\``; 0 planted → the existing not-enough-xu / level error.
- `type AutoOpts struct { Crop, KitchenRecipe, JuicerRecipe string; BuyFeed bool }`
- `type AutoReport struct { Harvested, Planted, Fed, Fruits, Crafted, Collected int; Spent int64; Skipped []string }` with JSON snake_case tags; `Gained.Auto *AutoReport \`json:"auto,omitempty"\``.
- `func (s *Store) FarmAuto(ctx, userID int64, o AutoOpts) (FarmResult, error)` — steps exactly as spec §C, in that order; business errors become `Skipped` entries (their Vietnamese message); DB errors abort. Nothing done → `ErrFarmNothingAuto`. Decision: one `farmTx` for the whole run (atomic; simplest given op-level helpers).
- Route `POST /api/farm/auto` body `{crop?, kitchen_recipe?, juicer_recipe?, buy_feed}`.
- TS: `api.farmPlantAll(crop)`, `api.farmAuto(opts)`, `gained.planted?`, `gained.auto?`.

- [ ] **Step 1: Failing tests:**
  - `TestFarmPlantAll`: 5 empty plots, xu for 2 rau_muong seeds → planted 2, balance −20, no error; 1 withered plot gets cleared and replanted; level-gated crop → level error; 0 xu → error.
  - `TestFarmAutoAllSteps`: ripe plots, hungry hen with feed in stock, ready tree, ready kitchen job, settings with crop + kitchen recipe → one call: `Harvested>0, Planted>0, Fed>0, Fruits>0, Collected>0, Crafted>0`, `Skipped` lists juicer step reason if no juicer.
  - `TestFarmAutoNoBuyFeed`: `BuyFeed=false`, hungry hen, no feed stock, nothing else to do → `ErrFarmNothingAuto`, balance unchanged; with `BuyFeed=true` → fed, `Spent > 0`.
  - `TestFarmAutoNothing` → `ErrFarmNothingAuto`.
  - Existing `TestFarmTend*`, harvest, plant, clear tests keep passing (refactor guard).
  - Controller `TestFarmAutoAPI` (+ plant all).
- [ ] **Step 2: Run, expect FAIL.** - [ ] **Step 3: Implement.** - [ ] **Step 4: Verify** `go test ./... -count=1`, tsc. - [ ] **Step 5: Commit** `feat(farm): plant-all and one-tap auto endpoint`.

### Task 4: Desktop — Orchard world + "Vườn & Ép" tab

**Files:** Modify `desktop/src/components/FarmWorld.tsx` (replace decorative orchard L1126-1143 with 12 real spots at spec coordinates; `Juicer` drawing like `Workshop` with a different look, placeable/movable like kitchen; orchard sign "🧺 Hái N quả"), `desktop/src/components/Farm.tsx` (new tab `orchard` "Vườn & Ép" 🍊 with tree shop + juicer panel; generalize `KitchenPanel` to take `building: "kitchen" | "juicer"` instead of duplicating; "Làm đầy lò" button per recipe → `api.farmCraftFill`; tree click handler: ready → harvest, empty → open tab shop with that spot selected, growing → toast time left, remove via confirm), `desktop/src/lib/farm.ts` (helpers `treeLive(t, now)` readiness mirroring server; perk-adjusted times reuse `effGrowSec`), `desktop/src/styles.css`.

**Interfaces — Consumes:** Task 1/2 TS API and types.

- [ ] Implement per spec §D (copy text Vietnamese: locked spot "Mở ở cấp N", empty-spot tooltip "Trồng cây ăn quả", toasts "Đã hái N {emoji}", "{Tên} còn {thời gian} nữa mới có quả.", confirm "Chặt cây {tên}? Không hoàn lại tiền cây giống.").
- [ ] Visitors see trees (read-only, no actions).
- [ ] Verify: `cd desktop && pnpm exec tsc --noEmit`; `make verify-css`; if feasible run `pnpm dev` and screenshot the farm; otherwise say so.
- [ ] Commit `feat(farm-ui): orchard, tree shop and juice press panel`.

### Task 5: Desktop — Automation buttons and auto mode

**Files:** Create `desktop/src/lib/farmAuto.ts` (settings type, load/save via `lib/storage.ts` key `farm-auto:${userId}`, `autoPending(view, now, settings): boolean`), modify `desktop/src/components/Farm.tsx` (banner buttons; auto toggle + settings popover; interval), `desktop/src/styles.css`.

**Interfaces — Produces:**
- `type FarmAutoSettings = { on: boolean; crop?: string; kitchen_recipe?: string; juicer_recipe?: string; buy_feed: boolean }` (default `{on:false, buy_feed:false}`).
- `autoPending` true if any: ripe plot; empty plot (index < plot_count, no crop) and `crop` set; withered plot and `crop` set; ready or hungry animal (hungry counts only if feed in stock or `buy_feed`); ready tree; ready job; free slot in a built kitchen/juicer and the matching recipe set with enough stock.

- [ ] Banner: "⚡ Làm hết" (when `autoPending`), "🌱 Gieo tất cả" (when empty plots; uses `settings.crop`, else opens shop tab and toast "Chọn loại cây để gieo tất cả").
- [ ] Toggle "🤖 Tự động" + ⚙️ popover (crop select from unlocked crops, kitchen recipe select, juicer recipe select, checkbox "Tự mua thức ăn khi thiếu"). Interval 30 s only while own farm view is open, settings.on, not `busy`, and not paused; on error: toast once, pause 5 min. Toast summary from `gained.auto` (e.g. "🤖 Thu 4 ô, gieo 4, cho 3 con ăn, hái 8 quả, nấu 2 món").
- [ ] Verify tsc + verify-css; commit `feat(farm-ui): one-tap "Làm hết" and auto mode`.

### Task 6: Docs

- [ ] `specs/FARM_SPEC.md` "Phần 5" (§12 Vườn cây ăn quả, §13 Máy ép nước, §14 Tự động) with the catalogs, endpoints and view fields actually shipped; `make check-spec` passes. Commit `docs(farm): spec part 5`.
