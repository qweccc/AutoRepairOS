package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
)

//go:embed migration/*.up.sql
var migrationFS embed.FS

// Migrate 依檔名排序執行所有尚未套用的 migration，並記錄在 schema_migrations。
// 已套用的版本不會重複執行。
func Migrate() error {
	MustExec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	applied, err := appliedVersions()
	if err != nil {
		return err
	}

	files, err := fs.Glob(migrationFS, "migration/*.up.sql")
	if err != nil {
		return fmt.Errorf("列出 migration 檔失敗: %v", err)
	}
	sort.Strings(files)

	for _, name := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migration/"), ".up.sql")
		if applied[version] {
			log.Printf("migration %s 已套用，略過", version)
			continue
		}

		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("讀取 migration %s 失敗: %v", name, err)
		}

		tx, err := DB.Begin()
		if err != nil {
			return fmt.Errorf("開始 transaction 失敗: %v", err)
		}

		if err := execScript(tx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("套用 migration %s 失敗: %v", version, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			tx.Rollback()
			return fmt.Errorf("記錄 migration %s 失敗: %v", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交 migration %s 失敗: %v", version, err)
		}

		log.Printf("已套用 migration: %s", version)
	}

	return nil
}

func appliedVersions() (map[string]bool, error) {
	rows, err := DB.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("查詢已套用 migration 失敗: %v", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// execScript 逐句執行 SQL script。modernc.org/sqlite 的 Exec 不接受多個語句，
// 所以自行以分號切分並跳過空行與註解。
func execScript(tx *sql.Tx, script string) error {
	for _, stmt := range splitStatements(script) {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("語句執行失敗 (%v): %s", err, truncate(stmt, 80))
		}
	}
	return nil
}

func splitStatements(script string) []string {
	var out []string
	var buf strings.Builder

	flush := func() {
		s := strings.TrimSpace(buf.String())
		if s != "" {
			out = append(out, s)
		}
		buf.Reset()
	}

	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			flush()
		}
	}
	flush()

	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
