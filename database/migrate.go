package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Mode string

const (
	ModeUp   Mode = "up"
	ModeDown Mode = "down"
)

type MigrationOpt struct {
	DB   *sql.DB
	Mode Mode
	// Steps: số migration cần chạy.
	//   - up:   <= 0 nghĩa là chạy hết các migration chưa chạy.
	//   - down: <= 0 nghĩa là rollback 1 migration gần nhất (tránh lỡ tay xoá hết).
	Steps int
}

type migration struct {
	Version int
	Name    string
	Up      string // đường dẫn file .up.sql trong migrationFS
	Down    string // đường dẫn file .down.sql trong migrationFS
}

// 000001_create_users.up.sql -> version=1, name=create_users, direction=up
var migrationFileRe = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

func Migrate(opt MigrationOpt) error {
	if opt.DB == nil {
		return fmt.Errorf("migrate: DB is nil")
	}

	if err := ensureMigrationTable(opt.DB); err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	applied, err := appliedVersions(opt.DB)
	if err != nil {
		return err
	}

	switch opt.Mode {
	case ModeUp:
		return migrateUp(opt.DB, migrations, applied, opt.Steps)
	case ModeDown:
		return migrateDown(opt.DB, migrations, applied, opt.Steps)
	default:
		return fmt.Errorf("migrate: invalid mode %q (expected %q or %q)", opt.Mode, ModeUp, ModeDown)
	}
}

func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}
	return nil
}

// loadMigrations đọc thư mục migrations, ghép cặp up/down theo version và sắp xếp tăng dần.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migrate: read migrations dir: %w", err)
	}

	byVersion := map[int]*migration{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migrate: invalid migration filename %q (expected 000001_name.up.sql)", e.Name())
		}

		version, _ := strconv.Atoi(m[1])
		name, direction := m[2], m[3]
		path := "migrations/" + e.Name()

		mg, ok := byVersion[version]
		if !ok {
			mg = &migration{Version: version, Name: name}
			byVersion[version] = mg
		} else if mg.Name != name {
			return nil, fmt.Errorf("migrate: version %d has conflicting names %q and %q", version, mg.Name, name)
		}

		if direction == "up" {
			mg.Up = path
		} else {
			mg.Down = path
		}
	}

	result := make([]migration, 0, len(byVersion))
	for _, mg := range byVersion {
		if mg.Up == "" || mg.Down == "" {
			return nil, fmt.Errorf("migrate: version %d (%s) is missing its up or down file", mg.Version, mg.Name)
		}
		result = append(result, *mg)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })

	return result, nil
}

func appliedVersions(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func migrateUp(db *sql.DB, migrations []migration, applied map[int]bool, steps int) error {
	count := 0
	for _, m := range migrations {
		if applied[m.Version] {
			continue
		}
		if steps > 0 && count >= steps {
			break
		}

		if err := runMigration(db, m.Up, func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`, m.Version, m.Name)
			return err
		}); err != nil {
			return fmt.Errorf("migrate up %06d_%s: %w", m.Version, m.Name, err)
		}

		log.Printf("migrate: applied %06d_%s", m.Version, m.Name)
		count++
	}

	if count == 0 {
		log.Println("migrate: no pending migrations")
	}
	return nil
}

func migrateDown(db *sql.DB, migrations []migration, applied map[int]bool, steps int) error {
	if steps <= 0 {
		steps = 1
	}

	count := 0
	for i := len(migrations) - 1; i >= 0 && count < steps; i-- {
		m := migrations[i]
		if !applied[m.Version] {
			continue
		}

		if err := runMigration(db, m.Down, func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM schema_migrations WHERE version = ?`, m.Version)
			return err
		}); err != nil {
			return fmt.Errorf("migrate down %06d_%s: %w", m.Version, m.Name, err)
		}

		log.Printf("migrate: rolled back %06d_%s", m.Version, m.Name)
		count++
	}

	if count == 0 {
		log.Println("migrate: nothing to roll back")
	}
	return nil
}

// runMigration chạy nội dung file SQL và cập nhật schema_migrations trong cùng một transaction,
// lỗi ở bất kỳ bước nào thì rollback toàn bộ.
func runMigration(db *sql.DB, path string, record func(tx *sql.Tx) error) error {
	content, err := migrationFS.ReadFile(path)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op nếu đã Commit

	if _, err := tx.Exec(string(content)); err != nil {
		return err
	}
	if err := record(tx); err != nil {
		return err
	}
	return tx.Commit()
}
