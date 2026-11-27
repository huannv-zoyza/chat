# Farm Water-All Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Owner can water every growing plot in one tap, and "⚡ Làm hết" / "🤖 Tự động" water automatically.

**Architecture:** Extract an op-level `waterPlot` helper from `FarmWater`; `FarmWaterAll` and a new `/auto` step reuse it; desktop adds a banner button and wires `autoPending`/`autoSummary`.

**Tech Stack:** Go store + controller tests; React/TS desktop (`tsc`, `make verify-css`).

**Spec:** `docs/superpowers/specs/2026-11-27-farm-water-all-design.md`

## Global Constraints

- Watering rules unchanged: per crop max `FarmMaxWaters` (3); each actor once per crop (`addAct(p.ID, actor, "water")`); `ready = max(now, p.Ready - p.Crop.GrowSec/10)`; only `stateGrowing`.
- Water-all is own farm only; no xu/XP/love/quest bump/log for owner self-watering (same as today's owner path).
- Only plots with `idx < plot_count`.
- `ErrFarmNothingWater = &FarmError{"Không có cây nào cần tưới."}`.
- `/auto` step order: harvest → plant → **water** → tend → trees → collect jobs → fill.
- Branch stacked on `feat/farm-orchard-juice` (PR #13).

## Review Focus

1. Second water-all call in a row → `ErrFarmNothingWater`, no double shortening. → Task 1 test.
2. Plot already watered 3× by neighbours → skipped, not an error for the others. → Task 1 test.
3. Neighbour-farm `{all:true, user_id: other}` rejected. → Task 1 controller test.
4. `/auto` that only waters (nothing else) is NOT "nothing to do". → Task 1 test.
5. Old single-plot `{user_id, plot}` route unchanged (existing water tests pass).

---

### Task 1: Store + API + /auto step

**Files:** Modify `store/farm.go` (extract `func (op *farmOp) waterPlot(actorID int64, p *plotRow, now int64) error` — use the actual plot type name; `FarmWater` calls it; add `FarmWaterAll`, `ErrFarmNothingWater`, `Gained.Watered`), `store/farm_auto.go` (step "Tưới" after plant, silent on `ErrFarmNothingWater`; `AutoReport.Watered int \`json:"watered"\``; include in nothing-done gate), `controller/farm.go` (water handler accepts `{all:true}`), `desktop/src/lib/{api,farm}.ts` (`api.farmWaterAll()`, `gained.watered?`, `auto.watered`). Tests: `store/farm_water_all_test.go`, controller test next to existing farm controller tests.

**Interfaces — Produces:**
- `func (s *Store) FarmWaterAll(ctx context.Context, userID int64) (FarmResult, error)`
- `POST /api/farm/water {all: true}` → FarmView with `gained.watered`; `user_id` present and ≠ me → 400 with a FarmError message ("Chỉ tưới tất cả được ở vườn của mình." — add as `ErrFarmWaterAllOwn`).
- TS: `api.farmWaterAll()`.

- [ ] **Step 1: Failing tests**
  - `TestFarmWaterAll`: plant 3 plots (rau_muong 300s at level 1 → no perks), one empty, one withered/ripe; water-all → `Watered == 3`, each ready_at moved earlier by `GrowSec/10`; second call → `ErrFarmNothingWater`; balance unchanged.
  - `TestFarmWaterAllSkipsMaxed`: a plot watered by 3 neighbours + a fresh plot → water-all waters only the fresh one (`Watered == 1`).
  - `TestFarmAutoWaters`: settings `Crop` set, empty plots → one `/auto` run plants and waters them (`Planted == Watered`); a run where the only work is watering returns no error.
  - Controller: `{all:true}` OK; `{all:true, user_id: neighbour}` rejected; old `{user_id, plot}` unchanged.
- [ ] **Step 2: Run, expect FAIL** — `go test ./store ./controller -run 'WaterAll|AutoWaters' -count=1`.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Verify** `go test ./... -count=1`; `cd desktop && pnpm exec tsc --noEmit`.
- [ ] **Step 5: Commit** `feat(farm): water-all and watering step in auto`.

### Task 2: Desktop button + auto wiring + spec doc

**Files:** `desktop/src/lib/farmAuto.ts` (`autoPending` true if own plot `growing` with `can_water`; `autoSummary` adds `tưới N ô` after `gieo`; export `waterablePlots(view, now): number`), `desktop/src/components/Farm.tsx` (banner "💧 Tưới tất cả" next to "🌱 Gieo tất cả", own farm only, visible when `waterablePlots > 0`, via `act()`, toast `Đã tưới N ô 💧` from `gained.watered`), `desktop/src/styles.css` if needed, `specs/FARM_SPEC.md` (§14 + API list).

- [ ] Implement; verify `pnpm exec tsc --noEmit`, `make verify-css`, `make check-spec`.
- [ ] Commit `feat(farm-ui): water-all button and auto watering`.
