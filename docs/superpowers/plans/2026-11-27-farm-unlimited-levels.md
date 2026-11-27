# Farm Unlimited Levels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the level-10 cap with a growing XP curve and give level-scaled, capped perks (faster crops, faster animals, bigger harvests, better sell price).

**Architecture:** Pure functions `levelXP(n)`, `levelFor(xp)`, `farmPerks(level)` in a new `store/farm_level.go`; perks applied at the four action points in the store; `FarmView` exposes `perks`/`next_perks`; desktop shows them.

**Tech Stack:** Go store + tests; React/TS desktop (verified with `tsc`, `make verify-css`).

**Spec:** `docs/superpowers/specs/2026-11-27-farm-unlimited-levels-design.md`

## Global Constraints

- Levels 1..10 keep exactly `LevelXP = {0, 30, 80, 160, 280, 450, 680, 1000, 1400, 1900}`.
- n ≥ 11: `gap(n) = round10(gap(n−1) × 115 / 100)` with integer math `round10(x) = (x + 5) / 10 * 10`; `gap(10) = 500`, so `gap(11) = 580`, `XP(11) = 2480`.
- No overflow: `levelFor` must terminate and be correct for `xp = math.MaxInt64`.
- Perks: `grow_pct = min(2*(lv-1), 50)`, `animal_pct = min(lv-1, 30)`, `harvest_bonus = min(lv/5, 5)`, `sell_pct = min(2*(lv-1), 40)`.
- Perks use the actor's level at action time; already-planted crops / already-producing animals unchanged.
- Harvest bonus only for the owner's harvest, never for steals. Sell bonus only in `FarmSell` (not orders, not gifts, not craft).
- Unlock levels, level-up xu reward (`20*lv`), market buy prices unchanged.
- This branch is stacked on `feat/farm-quick-tend`: animal feeding goes through `op.startProducing` (store/farm_v2.go) — apply `animal_pct` there so feed, feed-all and tend all get it.

## Review Focus

1. Buy-then-sell arbitrage at any level: for every crop item, `sellUnit × (100+40)/100 < BuyPrice`. → Task 1 `TestFarmPerksNoArbitrage`.
2. Steal after an owner with harvest bonus: thief still gets exactly 1 unit and `harvestUnits` floor/steal math stays intact. → Task 1 `TestFarmHarvestBonus`.
3. Legacy rows: farms with XP ≥ 1900 jump from "max" to the right level >10 on read; `next_level_xp` non-null. → Task 1 `TestFarmLevelCurve`.
4. Water-speedup (`p.Ready - GrowSec/10`) still behaves on shortened crops (ready never before now). → existing water tests must pass.
5. UI with old server (no `perks` field) must not crash. → Task 2 optional chaining.

---

### Task 1: Store — curve, perks, application, view

**Files:**
- Create: `store/farm_level.go` (move `LevelXP`, `levelFor` here from `store/farm.go:53-85`; add `levelXP`, `Perks`, `farmPerks`)
- Modify: `store/farm.go` (FarmPlant ~L676 ready time; `harvestUnits`/FarmHarvest ~L696-760 owner bonus; FarmView ~L244 fields; GetFarm view build ~L1237-1247 uses `levelXP`)
- Modify: `store/farm_v2.go` (`startProducing` animal time; `FarmSell` price incl. sell-all)
- Test: `store/farm_level_test.go` (new; external `store_test` package; reuse helpers from `farm_v2_test.go` such as `newFarmEnv`, `setXP`, `setStock`, `advance`, `view`, `storage`; if a pure func must be tested, use `store/farm_internal_test.go`'s internal package style)

**Interfaces — Produces:**
- `func levelXP(n int) int64` (total XP to reach level n; n ≤ 1 → 0)
- `func levelFor(xp int64) int`
- `type Perks struct { GrowPct int \`json:"grow_pct"\`; AnimalPct int \`json:"animal_pct"\`; HarvestBonus int64 \`json:"harvest_bonus"\`; SellPct int \`json:"sell_pct"\` }`
- `func farmPerks(level int) Perks`
- `FarmView` new fields `Perks Perks \`json:"perks"\``, `NextPerks Perks \`json:"next_perks"\``; `NextLevelXP` always set.
- Keep exported `LevelXP` var (tests/other code may read it).

- [ ] **Step 1: Failing tests** in `store/farm_level_test.go` (internal tests where needed):
  - `TestFarmLevelCurve`: `levelXP(1..10)` equals `LevelXP`; `levelXP(11) == 2480`; `levelXP(12) == 2480 + round10(580*115/100) == 2480+670 == 3150`; strictly increasing gaps up to 200; `levelFor(levelXP(n)) == n` and `levelFor(levelXP(n)-1) == n-1` for n in 2..200; `levelFor(math.MaxInt64)` returns (no hang/panic) a level > 200. Via store: `setXP(5000)` → view `Level` matches `levelFor(5000)`, `NextLevelXP != nil`.
  - `TestFarmPerks`: table — lv1 `{0,0,0,0}`, lv5 `{8,4,1,8}`, lv10 `{18,9,2,18}`, lv21 `{40,20,4,40}`, lv26 `{50,25,5,40}`, lv100 `{50,30,5,40}`; view `perks` / `next_perks` match `farmPerks(lv)` / `farmPerks(lv+1)`.
  - `TestFarmPerkGrowAndAnimal`: at level 11 (`grow 20%`, `animal 10%`) planting rau_muong (300s) → ready after 240s, not ready at 239s; feeding a hen (1200s) → ready after 1080s. Also through `FarmTend` (stacked branch) the hen gets 1080s.
  - `TestFarmHarvestBonus`: level 10 owner harvest of a clean plot gives 3+2 = 5 units; a plot with a bug gives 4; a neighbour stealing gets 1 unit and the owner then gets `5 - stolen` units.
  - `TestFarmSellBonus`: level 11 (`sell 20%`) selling 5 trung (price 22) earns `22*5*120/100 = 132`; sell-all applies the same per item; level 1 unchanged.
  - `TestFarmPerksNoArbitrage`: for every item with `BuyPrice > 0`, `Price*(100+40)/100 < BuyPrice`.
- [ ] **Step 2: Run, expect FAIL** — `go test ./store -run 'TestFarm(LevelCurve|Perks|PerkGrow|HarvestBonus|SellBonus|PerksNoArbitrage)' -count=1` → compile errors.
- [ ] **Step 3: Implement** per Interfaces and Global Constraints. Sell rounding: per item `price*qty*(100+pct)/100` (integer floor).
- [ ] **Step 4: Run** — `go test ./... -count=1` → all ok (existing farm tests must still pass; if an existing test asserted cap-10 behaviour like `next_level_xp == null`, update it to the new rule and say so in the report).
- [ ] **Step 5: Commit** — `feat(farm): unlimited levels with growing XP curve and level perks`.

### Task 2: Desktop — perks display

**Files:**
- Modify: `desktop/src/lib/farm.ts` (`FarmView.perks?`, `next_perks?`, `type Perks`; `next_level_xp` stays `number | null` for old servers)
- Modify: `desktop/src/components/Farm.tsx` (level bar ~L1094-1112; seed shop time ~L1179 and crop table ~L2075; storage price ~L2466 and total ~L2422; level-up toast in `act`)
- Modify: `desktop/src/styles.css` (perk panel)

**Interfaces — Consumes:** `perks`, `next_perks` from Task 1 (`grow_pct`, `animal_pct`, `harvest_bonus`, `sell_pct`).

- [ ] **Step 1:** Add helpers in `lib/farm.ts`: `effGrowSec(sec: number, p?: Perks)`, `effAnimalSec(sec, p?)`, `effSellPrice(price: number, p?: Perks)` — same integer math as server (`Math.floor`). Absent perks → unchanged.
- [ ] **Step 2:** Level bar: remove "Tối đa" (when `next_level_xp` null on an old server keep current fallback). Clicking the level badge toggles a perk panel listing 4 rows, Vietnamese copy:
  `🌱 Cây lớn nhanh hơn {grow}%`, `🐓 Vật nuôi làm nhanh hơn {animal}%`, `🧺 Thu hoạch thêm +{bonus}/vụ`, `🪙 Bán giá cao hơn {sell}%`; each with `→ {next}` when next > current, else `(tối đa)`.
- [ ] **Step 3:** Seed shop / crop table show `formatGrow(effGrowSec(c.grow_sec, view.perks))`; animal shop time ~L2015 uses `effAnimalSec`; storage unit price and total use `effSellPrice`.
- [ ] **Step 4:** Level-up toast (`g.level_up`) appends the perks that improved, e.g. `Cây lớn nhanh hơn 20%, bán giá +20%` (compare `perks` vs previous view's perks; if unavailable, skip).
- [ ] **Step 5: Verify** — `cd desktop && pnpm exec tsc --noEmit` clean; `make verify-css` passes.
- [ ] **Step 6: Commit** — `feat(farm-ui): show level perks and perk-adjusted times/prices`.
