package database

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func openUnmigratedTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applyMigrationsThrough(t *testing.T, db *DB, version int) {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create migrations table: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version > version {
			break
		}
		if err := db.applyMigration(migration); err != nil {
			t.Fatalf("apply migration %d: %v", migration.Version, err)
		}
	}
}

func querySignature(t *testing.T, db *DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("query signature: %v", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("signature columns: %v", err)
	}
	var result []string
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for index := range values {
			targets[index] = &values[index]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatalf("scan signature: %v", err)
		}
		result = append(result, fmt.Sprint(values...))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("signature rows: %v", err)
	}
	return result
}

func schemaSignature(t *testing.T, db *DB) []string {
	t.Helper()
	result := querySignature(t, db, `
		SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type IN ('table', 'index', 'trigger', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name
	`)
	tables := querySignature(t, db, `
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	for _, table := range tables {
		result = append(result, "table_info:"+table)
		result = append(result, querySignature(t, db, fmt.Sprintf("PRAGMA table_info(%q)", table))...)
		result = append(result, "index_list:"+table)
		result = append(result, querySignature(t, db, fmt.Sprintf("PRAGMA index_list(%q)", table))...)
		result = append(result, "foreign_key_list:"+table)
		result = append(result, querySignature(t, db, fmt.Sprintf("PRAGMA foreign_key_list(%q)", table))...)
	}
	return result
}

func assertDatabaseIntegrity(t *testing.T, db *DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check returned a violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q, want ok", integrity)
	}
}

func TestMigrationUpgradePathsMatchFreshSchema(t *testing.T) {
	fresh := openUnmigratedTestDB(t)
	if err := fresh.Migrate(); err != nil {
		t.Fatalf("fresh Migrate: %v", err)
	}
	freshSchema := schemaSignature(t, fresh)
	assertDatabaseIntegrity(t, fresh)

	for _, startVersion := range []int{31, 42, 81} {
		t.Run(fmt.Sprintf("from_v%d", startVersion), func(t *testing.T) {
			db := openUnmigratedTestDB(t)
			applyMigrationsThrough(t, db, startVersion)
			if err := db.Migrate(); err != nil {
				t.Fatalf("upgrade from v%d: %v", startVersion, err)
			}
			if got := schemaSignature(t, db); !reflect.DeepEqual(got, freshSchema) {
				t.Fatalf("schema after upgrade from v%d differs from fresh install: %s", startVersion, firstSchemaDifference(freshSchema, got))
			}
			assertDatabaseIntegrity(t, db)

			if err := db.Migrate(); err != nil {
				t.Fatalf("repeat Migrate after v%d upgrade: %v", startVersion, err)
			}
			if got := schemaSignature(t, db); !reflect.DeepEqual(got, freshSchema) {
				t.Fatalf("schema changed after repeat Migrate from v%d upgrade: %s", startVersion, firstSchemaDifference(freshSchema, got))
			}
		})
	}
}

func TestFailedMigrationDoesNotRecordVersion(t *testing.T) {
	db := openUnmigratedTestDB(t)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create migrations table: %v", err)
	}

	failed := Migration{Version: 999, SQL: `THIS IS NOT VALID SQL`}
	if err := db.applyMigration(failed); err == nil {
		t.Fatal("applyMigration succeeded, want SQL error")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migrations WHERE version = ?`, failed.Version).Scan(&count); err != nil {
		t.Fatalf("count failed migration marker: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed migration marker count = %d, want 0", count)
	}
}

func TestMigrationV47AddsZeroFlagsSyncWatermark(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 46)
	if _, err := db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, imap_port, smtp_host, smtp_port, auth_type, username)
		VALUES ('account-1', 'Account', 'account-1@example.test', 'imap.example.test', 993, 'smtp.example.test', 587, 'password', 'account-1')
	`); err != nil {
		t.Fatalf("seed account before v47: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO folders (id, account_id, name, path, highest_mod_seq)
		VALUES ('folder-1', 'account-1', 'Inbox', 'INBOX', 150)
	`); err != nil {
		t.Fatalf("seed folder before v47: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("upgrade through v47: %v", err)
	}
	var observed, watermark int
	if err := db.QueryRow(`SELECT highest_mod_seq, flags_sync_modseq FROM folders WHERE id = 'folder-1'`).Scan(&observed, &watermark); err != nil {
		t.Fatalf("read v47 columns: %v", err)
	}
	if observed != 150 || watermark != 0 {
		t.Fatalf("migration values = observed:%d watermark:%d, want observed:150 watermark:0", observed, watermark)
	}
}

func TestMigrationV52_DefaultSyncPeriodAllMessages(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 51)

	if _, err := db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, smtp_host, username, sync_period_days)
		VALUES
			('acc-180', 'OAuth', 'oauth@example.com', 'imap.example.com', 'smtp.example.com', 'user1', 180),
			('acc-30', 'Legacy', 'legacy@example.com', 'imap.example.com', 'smtp.example.com', 'user2', 30),
			('acc-custom', 'Custom', 'custom@example.com', 'imap.example.com', 'smtp.example.com', 'user3', 7),
			('acc-all', 'All', 'all@example.com', 'imap.example.com', 'smtp.example.com', 'user4', 0);
	`); err != nil {
		t.Fatalf("seed accounts before v52: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate through v52: %v", err)
	}

	rows, err := db.Query(`SELECT id, sync_period_days FROM accounts ORDER BY id`)
	if err != nil {
		t.Fatalf("query accounts: %v", err)
	}
	defer rows.Close()

	results := make(map[string]int)
	for rows.Next() {
		var id string
		var period int
		if err := rows.Scan(&id, &period); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		results[id] = period
	}

	if results["acc-180"] != 0 {
		t.Errorf("acc-180 sync_period_days = %d, want 0", results["acc-180"])
	}
	if results["acc-30"] != 0 {
		t.Errorf("acc-30 sync_period_days = %d, want 0", results["acc-30"])
	}
	if results["acc-custom"] != 7 {
		t.Errorf("acc-custom sync_period_days = %d, want 7", results["acc-custom"])
	}
	if results["acc-all"] != 0 {
		t.Errorf("acc-all sync_period_days = %d, want 0", results["acc-all"])
	}

	// Verify virtual_folders table has the new criteria columns with defaults
	if _, err := db.Exec(`
		INSERT INTO virtual_folders (id, name, order_index, created_at, updated_at)
		VALUES ('vf-test', 'All Mail', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
	`); err != nil {
		t.Fatalf("insert virtual folder after v52: %v", err)
	}

	var folderMode, readStatus, replyStatus, starStatus, dateWindow string
	if err := db.QueryRow(`
		SELECT folder_mode, read_status, reply_status, star_status, date_window
		FROM virtual_folders WHERE id = 'vf-test'
	`).Scan(&folderMode, &readStatus, &replyStatus, &starStatus, &dateWindow); err != nil {
		t.Fatalf("query virtual folder criteria columns: %v", err)
	}
	if folderMode != "include" || readStatus != "all" || replyStatus != "all" || starStatus != "all" || dateWindow != "all" {
		t.Errorf("unexpected defaults: mode=%s read=%s reply=%s star=%s date=%s",
			folderMode, readStatus, replyStatus, starStatus, dateWindow)
	}
}

func firstSchemaDifference(want, got []string) string {
	limit := len(want)
	if len(got) < limit {
		limit = len(got)
	}
	for index := 0; index < limit; index++ {
		if want[index] != got[index] {
			return fmt.Sprintf("index %d: fresh=%q upgrade=%q", index, want[index], got[index])
		}
	}
	return fmt.Sprintf("entry count: fresh=%d upgrade=%d", len(want), len(got))
}

func TestMigrationV58AddsDraftAttachmentBlobTables(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 57)

	// Before v58 the PR #45 blob tables must not exist.
	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('draft_attachment_blobs', 'draft_history_node_blobs')`).Scan(&before); err != nil {
		t.Fatalf("count blob tables before v58: %v", err)
	}
	if before != 0 {
		t.Fatalf("blob tables present before v58: %d, want 0", before)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate through v58: %v", err)
	}

	// Round-trip one blob and one membership row the way drafthistory does.
	if _, err := db.Exec(`INSERT INTO draft_history_states (hash, payload, byte_size) VALUES ('s1', '{}', 2)`); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO draft_history_nodes (node_id, draft_key, state_hash) VALUES ('n1', 'acc:d1', 's1')`); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO draft_attachment_blobs (hash, name, mime, size, content) VALUES ('b1', 'a.pdf', 'application/pdf', 3, x'010203')`); err != nil {
		t.Fatalf("insert blob: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO draft_history_node_blobs (node_id, blob_hash) VALUES ('n1', 'b1')`); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT blob_hash FROM draft_history_node_blobs WHERE node_id = 'n1'`).Scan(&got); err != nil {
		t.Fatalf("read membership: %v", err)
	}
	if got != "b1" {
		t.Fatalf("membership = %q, want b1", got)
	}
}

func TestMigrationV59AddsMailboxUndoCommands(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 58)

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'mailbox_undo_commands'`).Scan(&before); err != nil {
		t.Fatalf("count undo table before v59: %v", err)
	}
	if before != 0 {
		t.Fatal("mailbox_undo_commands present before v59")
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate through v59: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO mailbox_undo_commands (operation_id, action, type, payload, created_at) VALUES ('op1', 'archive', 'move', '{}', '2026-10-28 20:00:00.000000000')`); err != nil {
		t.Fatalf("insert undo row: %v", err)
	}
	var op string
	if err := db.QueryRow(`SELECT operation_id FROM mailbox_undo_commands WHERE seq = 1`).Scan(&op); err != nil {
		t.Fatalf("read undo row: %v", err)
	}
	if op != "op1" {
		t.Fatalf("operation_id = %q, want op1", op)
	}
}

func TestFilterMigration82PreservesVersion81RulesJobsAndVisibility(t *testing.T) {
	db := openUnmigratedTestDB(t)
	applyMigrationsThrough(t, db, 81)
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES ('a','A','a@example.com','imap','smtp','a')`,
		`INSERT INTO folders(id,account_id,name,path,folder_type,hidden) VALUES ('destination','a','Archive','Archive','archive',1)`,
		`INSERT INTO mail_filter_rules(id,name,account_id,from_contains,dest_folder_id,created_at,updated_at) VALUES ('legacy','Legacy','a','["sender@example.com"]','destination',datetime('now'),datetime('now'))`,
		`INSERT INTO automatic_filter_jobs(id,rule_id,destination_folder_id,message_id,source_account_id,source_folder_id,source_uid,state) VALUES ('queued','legacy','destination','message','a','source',12,'pending')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	var account, from, dest, accounts, list string
	var read, archive, trash int
	if err := db.QueryRow(`SELECT account_id,from_contains,dest_folder_id,account_ids,list_id_contains,action_mark_read,action_archive,action_trash FROM mail_filter_rules WHERE id='legacy'`).Scan(&account, &from, &dest, &accounts, &list, &read, &archive, &trash); err != nil {
		t.Fatal(err)
	}
	if account != "a" || from != `["sender@example.com"]` || dest != "destination" || accounts != "[]" || list != "[]" || read != 0 || archive != 0 || trash != 0 {
		t.Fatalf("legacy rule changed: %s %s %s %s %s %d %d %d", account, from, dest, accounts, list, read, archive, trash)
	}
	var actions, state string
	var uid, hidden, version int
	if err := db.QueryRow(`SELECT actions,state,source_uid FROM automatic_filter_jobs WHERE id='queued'`).Scan(&actions, &state, &uid); err != nil {
		t.Fatal(err)
	}
	if actions != "{}" || state != "pending" || uid != 12 {
		t.Fatalf("queued move changed: %s %s %d", actions, state, uid)
	}
	if err := db.QueryRow(`SELECT hidden FROM folders WHERE id='destination'`).Scan(&hidden); err != nil || hidden != 1 {
		t.Fatalf("visibility lost: %d %v", hidden, err)
	}
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil || version != 82 {
		t.Fatalf("migration 82 skipped: %d %v", version, err)
	}
	assertDatabaseIntegrity(t, db)
}
