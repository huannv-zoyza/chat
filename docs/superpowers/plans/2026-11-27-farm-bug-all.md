# Farm Bug-All Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Owner catches every visible bug in one tap; "⚡ Làm hết" / "🤖 Tự động" catch bugs first.

**Architecture:** Mirror the water-all feature (PR #14): op-level `catchBug` extracted from `FarmBug`, `FarmBugAll`, a first `/auto` step, desktop button + auto wiring + spec doc.

**Tech Stack:** Go store/controller tests; React/TS desktop.

**Spec:** `docs/superpowers/specs/2026-11-27-farm-bug-all-design.md`

## Global Constraints

- Only `p.bugVisible(now)` plots; idx < plot_count; own farm only (`{all:true, user_id: other}` → 400 `ErrFarmBugAllOwn` "Chỉ bắt sâu tất cả được ở vườn của mình.").
- `ErrFarmNothingBug = &FarmError{"Không có sâu nào để bắt."}`; `Gained.Bugs` `json:"bugs,omitempty"`; `AutoReport.Bugs` `json:"bugs"`.
- Owner self-catch: `bump(owner, "bug", 1)` per plot (same as manual), no xu/XP/love/log.
- `/auto` order: bug → harvest → plant → water → tend → trees → collect → fill. Step label "Bắt sâu", silent on ErrFarmNothingBug, Bugs in nothing-done gate.
- Branch stacked on `feat/farm-auto-water` (PR #14).

## Review Focus
1. Ripe plot with a bug in `/auto` yields full harvest (bug caught first).
2. Second bug-all → ErrFarmNothingBug.
3. Hidden bug (progress < 50%) not caught, not counted.
4. Neighbour `FarmBug` reward path unchanged.
5. Client button never on neighbour farm.

---

### Task 1: Bug-all end to end

**Files:** `store/farm.go` (catchBug, FarmBugAll, errors, Gained.Bugs), `store/farm_auto.go` (first step, AutoReport.Bugs, gate), `controller/farm.go` (bug handler `{all:true}`), tests `store/farm_bug_all_test.go` + controller test next to `TestFarmWaterAllAPI`; `desktop/src/lib/{api,farm,farmAuto}.ts` (`api.farmBugAll()`, `gained.bugs?`, `auto.bugs?`, `autoPending` bug condition, `autoSummary` "bắt N sâu" first, `buggyPlots(view)`), `desktop/src/components/Farm.tsx` (banner "🐛 Bắt sâu tất cả", own farm, via `act()`, toast "Đã bắt N con sâu 🐛"), `specs/FARM_SPEC.md` (§14 order + labels + API list).

- [ ] **Step 1: Failing tests** — `TestFarmBugAll` (2 plots with visible bug, 1 with hidden bug <50%, 1 clean → Bugs==2; quest "bug" progress +2; second call ErrFarmNothingBug), `TestFarmAutoCatchesBugBeforeHarvest` (ripe plot with bug → /auto harvest yields full units, `auto.bugs==1`), controller `TestFarmBugAllAPI` (own ok, neighbour 400, old `{user_id, plot}` unchanged).
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** server then client then spec doc.
- [ ] **Step 4: Verify** `go test ./... -count=1`, `cd desktop && pnpm exec tsc --noEmit`, `make verify-css`, `make check-spec`.
- [ ] **Step 5: Commit** `feat(farm): bug-all and bug step in auto`.
