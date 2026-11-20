---
name: feature-brainstorm
description: Brainstorm new features, analyze system architecture, evaluate technical trade-offs, and produce comprehensive, actionable technical specifications (SPEC.md) before implementation. Use this skill when the user asks to brainstorm, plan, design, or write a SPEC for a new feature, game, or module.
---

# Feature Brainstorm & Specification Skill

This skill guides Antigravity through brainstorming, architectural analysis, edge-case discovery, and producing production-grade technical specifications (`SPEC.md`) before writing any code.

---

## Workflow Overview

```text
[User Idea / Request]
         ↓
1. Requirement & Scope Analysis (Goals, Non-Goals, Personas)
         ↓
2. Architecture & Event-Flow Design (Sequence diagrams, State machines)
         ↓
3. Domain Logic & Edge-Case Modeling (Rules, Validations, Error handling)
         ↓
4. Data Models & API Contracts (DB schemas, REST endpoints, WebSocket events)
         ↓
5. UI / UX & Interaction Design (Components, states, visual indicators, audio)
         ↓
6. Generate Comprehensive SPEC.md (Saved at repo root)
         ↓
7. Present Key Decisions to User (Awaiting confirmation before coding)
```

---

## Step-by-Step Instructions

### Step 1: Discover & Analyze Existing Codebase Architecture
Before proposing new architecture or libraries:
1. Search the repository for existing patterns, conventions, and reusable modules:
   - How are similar features implemented? (e.g., existing games, controllers, state stores).
   - What libraries and tools are already in use? (avoid introducing redundant external dependencies).
   - What protocols and formats are used? (WebSocket frames, JSON structures, DB queries).
2. Clearly distinguish between **In-Scope (v1)** and **Out-of-Scope (v2+)** to prevent scope creep.

### Step 2: Model Domain Logic & Rule Matrix
1. Break down the feature into atomic domain entities and operations.
2. Build an **Edge Case Matrix**:
   - Boundary values (empty inputs, maximum limits, simultaneous actions).
   - Terminal conditions (win, loss, draw, timeout, stalemate, disconnection).
   - Permission / validation checks (whose turn is it, can this piece move here, is user authorized).
   - Anti-cheat & Security considerations (server-authoritative vs client-rendered).

### Step 3: Design Architecture & Data Contracts
1. **Flow / Sequence Diagram**: Use Mermaid sequence diagrams (`sequenceDiagram`) to document the message flow between client, server, database, and background workers.
2. **Data Models**:
   - Backend Go/Python/Rust structs.
   - Database tables or SQLite migrations.
   - JSON request/response payloads.
   - WebSocket broadcast events.
3. **State Machine**:
   - Define all valid states (`lobby` → `playing` → `paused` → `ended`).
   - Define valid transitions and guard conditions.

### Step 4: Detail UI & Interaction (Frontend)
1. Component hierarchy and layout structure (SVG, Canvas, or DOM).
2. User interaction states:
   - Hover, selection, legal move indicators, drag-and-drop.
   - Visual alerts (e.g., check alert, timers, error toasts).
   - Audio feedback (synthesized Web Audio or sound assets).
   - Perspective and accessibility (e.g., flip board, high-contrast markers).

### Step 5: Write the Technical Specification into `specs/`
All technical specifications **MUST** be created inside the **`specs/`** directory (e.g., `specs/<FEATURE_NAME>_SPEC.md`). Do not place SPEC files in the project root.

#### Quick Generation with the Included Script:
You can use the bundled script to scaffold a new specification file with the standard 7-section structure:
```bash
./scripts/create-spec.sh <feature_slug> ["Tiêu đề tính năng tiếng Việt"]
# Example:
./scripts/create-spec.sh live-topbar "Ghim Game Đang Live Ở Topbar Kênh Chat"
```
The script normalizes the filename, prevents accidental overwrites, and populates the standard template.

#### Required SPEC Sections:
1. **Mục tiêu & Phạm vi (Goals & Scope)**: Goals, non-goals, user benefits.
2. **Kiến trúc & Luồng hoạt động (Architecture & Flow)**: Mermaid sequence diagram, state machine.
3. **Mô hình dữ liệu & API Contracts**: Structs, REST APIs, WebSocket schemas.
4. **Quy tắc nghiệp vụ & Xử lý trường hợp biên (Domain Rules & Edge Cases)**: Full rules matrix, boundary handling.
5. **Thiết kế Giao diện Người dùng (UI / UX)**: Component architecture, visual/audio feedback, responsive layout.
6. **Tích hợp Bảng xếp hạng / Thống kê (Leaderboard & Stats)**: If applicable.
7. **Kế hoạch triển khai từng bước (Step-by-Step Implementation Plan)**: Checkboxes for tracking progress.

After creating a SPEC, update the index in `specs/README.md` if necessary.

### Step 6: User Alignment & Decision Points
After creating the SPEC:
- Do not paste the entire SPEC into the chat.
- Create a clickable markdown link to the SPEC file: `[specs/<FEATURE_NAME>_SPEC.md](file:///Users/hoang.nguyen3/tidebird/chat/specs/<FEATURE_NAME>_SPEC.md)`.
- Highlight **2-3 key architectural decisions or design choices** that need user confirmation (e.g., UI styles, rule interpretations, default settings) using direct questions or the `ask_question` tool.
