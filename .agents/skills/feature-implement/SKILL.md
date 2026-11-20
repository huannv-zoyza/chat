---
name: feature-implement
description: Execute and implement features strictly according to an approved technical specification (SPEC.md). Use this skill when the user asks to start implementing, coding, developing, or executing tasks defined in a SPEC or design document.
---

# Feature Implementation Skill

This skill guides Antigravity through implementing full-stack features methodically, adhering strictly to an approved technical specification (`SPEC.md`), maintaining high code quality, and validating incrementally at each step.

---

## Workflow Overview

```text
[Approved SPEC.md]
         ↓
1. Preflight Check (Branch, Git status, Dependencies)
         ↓
2. Backend & Core Engine Implementation (Models, Handlers, Game engine, DB)
         ↓
3. Intermediate Validation (Go build, type checks, incremental unit tests)
         ↓
4. Frontend & UI Implementation (Components, Hooks, State, WebSocket integration)
         ↓
5. Full Integration Verification (Local builds, End-to-end event wiring)
         ↓
6. Update SPEC Progress Checklist & Atomic Git Commits
```

---

## Step-by-Step Instructions

### Step 1: Pre-implementation Check
1. **Read the Specification**:
   - Locate and read the relevant `*_SPEC.md` file completely before writing code.
   - Note all data structures, rule constraints, API paths, and WebSocket event names.
2. **Verify Git Hygiene**:
   - Check `git status` to ensure working directory is clean.
   - Confirm you are on the intended feature branch.

### Step 2: Implement Backend Core / Domain Engine
1. **Data Models & Types**:
   - Create or update structs in their respective domain package (e.g., `game/<feature>.go`, `model/`).
   - Ensure JSON tags match the contracts defined in the SPEC.
2. **Core Logic & Rule Engine**:
   - Implement pure domain logic functions (e.g., move validation, board state transitions, check/win detection).
   - Keep core engine code decoupled from HTTP/WebSocket handlers for easy unit testing.
   - Guard against invalid operations, panics, nil pointers, and array index out-of-bounds.
3. **Storage & Controller Integration**:
   - Add database schemas/queries if needed (e.g., `store/`, migrations).
   - Register the new game type or feature in manager registries (e.g., `game.Register(...)`).
   - Wire HTTP endpoints or WebSocket action handlers in controllers.

### Step 3: Incremental Backend Verification
Immediately compile and run unit tests for the backend before touching frontend code:
```bash
go build ./...
go test -v ./game/... # Or the package modified
```
Fix any compilation errors or failing unit tests immediately.

### Step 4: Implement Frontend Components & State
1. **Component Scaffolding**:
   - Create UI components in `desktop/src/components/<ComponentName>.tsx`.
   - Use clean, modular React patterns with TypeScript interfaces matching the backend JSON schema.
2. **Visual & Interaction Implementation**:
   - Implement SVG boards/canvases, interactive piece selection, move indicators, drag-and-drop.
   - Implement turn timers, player badges, status messages, and game result dialogs.
3. **State Management & Realtime Sync**:
   - Connect WebSocket events (e.g., `game.updated`, `game.ended`) in providers or custom hooks.
   - Handle edge cases: component unmounting, reconnection, window resizing.
4. **Integration Points**:
   - Add entry points to dialogs (e.g., Game picker, Leaderboard tabs, Settings).

### Step 5: Incremental Frontend Verification
Compile TypeScript and verify frontend build:
```bash
cd desktop && pnpm build
```
Verify that:
- TypeScript compiler (`tsc`) produces 0 errors.
- Bundler completes with no missing module or syntax issues.

### Step 6: Update SPEC Progress & Git Commits
1. Mark completed tasks `[x]` in the implementation plan of the `*_SPEC.md` file.
2. Make atomic, descriptive Git commits following Conventional Commits format:
   - `feat(<scope>): implement ...`
   - `docs(spec): update implementation progress for ...`
