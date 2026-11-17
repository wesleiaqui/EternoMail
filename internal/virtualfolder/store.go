package virtualfolder

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/rs/zerolog"
)

// Store provides persistence operations for virtual folders
type Store struct {
	db  *database.DB
	log zerolog.Logger
}

// NewStore creates a new virtual folder store
func NewStore(db *database.DB) *Store {
	s := &Store{
		db:  db,
		log: logging.WithComponent("virtualfolder-store"),
	}
	if err := s.ensureTables(); err != nil {
		// Table creation is idempotent; a failure here is fatal because every
		// virtual folder operation depends on the schema. Surface it at startup
		// rather than failing with a confusing "no such table" on first use.
		panic(fmt.Errorf("virtualfolder store: ensure tables: %w", err))
	}
	return s
}

// ensureTables confirms the migration-created virtual-folder schema is present.
func (s *Store) ensureTables() error {
	var tableCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='virtual_folder_members'`).Scan(&tableCount); err != nil {
		return err
	}
	if tableCount == 0 {
		return fmt.Errorf("virtual folder schema is missing; run database migrations")
	}
	return nil
}

// create inserts a new virtual folder and its folder members (retried on SQLITE_BUSY by the public wrapper)
func (s *Store) create(vf *VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	vf.Name = strings.TrimSpace(vf.Name)
	if vf.Name == "" {
		return fmt.Errorf("virtual folder name cannot be empty")
	}
	if vf.ID == "" {
		vf.ID = uuid.New().String()
	}
	if err := vf.normalizeCriteria(); err != nil {
		return err
	}

	now := time.Now().UTC()
	if vf.CreatedAt.IsZero() {
		vf.CreatedAt = now
	}
	vf.UpdatedAt = now

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	// Insert virtual folder
	_, err = tx.Exec(`
		INSERT INTO virtual_folders (id, name, order_index, folder_mode, read_status, reply_status, star_status, date_window, search_query, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, vf.ID, vf.Name, vf.OrderIndex, vf.FolderMode, vf.ReadStatus, vf.ReplyStatus, vf.StarStatus, vf.DateWindow, vf.SearchQuery, vf.CreatedAt, vf.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert virtual folder: %w", err)
	}

	// Insert members
	for _, folderID := range vf.FolderIDs {
		_, err = tx.Exec(`
			INSERT INTO virtual_folder_members (virtual_folder_id, folder_id)
			VALUES (?, ?)
		`, vf.ID, folderID)
		if err != nil {
			return fmt.Errorf("failed to insert virtual folder member %s: %w", folderID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Busy-retry policy for virtual-folder writes. Sync churn (per-message
// upserts, FTS batches) can starve one waiter past the DSN busy_timeout, which
// used to surface in the UI as "failed to begin transaction: database is
// locked (5) (SQLITE_BUSY)". A few short retries absorb that transient
// contention; reads need no retry because WAL readers never block.
const (
	busyRetryAttempts  = 5
	busyRetryBaseDelay = 200 * time.Millisecond
)

// Create inserts a new virtual folder and its folder members
func (s *Store) Create(vf *VirtualFolder) error {
	return database.RetryOnBusy(func() error { return s.create(vf) }, busyRetryAttempts, busyRetryBaseDelay)
}

// Update modifies an existing virtual folder and its folder members
func (s *Store) Update(vf *VirtualFolder) error {
	return database.RetryOnBusy(func() error { return s.update(vf) }, busyRetryAttempts, busyRetryBaseDelay)
}

// Delete removes a virtual folder and its members from the database
func (s *Store) Delete(id string) error {
	return database.RetryOnBusy(func() error { return s.delete(id) }, busyRetryAttempts, busyRetryBaseDelay)
}

// Reorder updates the display order of virtual folders
func (s *Store) Reorder(ids []string) error {
	return database.RetryOnBusy(func() error { return s.reorder(ids) }, busyRetryAttempts, busyRetryBaseDelay)
}

// SaveCache atomically replaces a virtual folder's cached conversation index
// and stamps the refresh time.
func (s *Store) SaveCache(virtualFolderID string, entries []CachedConversation) error {
	return database.RetryOnBusy(func() error { return s.saveCache(virtualFolderID, entries) }, busyRetryAttempts, busyRetryBaseDelay)
}

// SaveSearchCachePage retains provider-resolved membership, never a local FTS
// approximation. Pages replace only their account on the first page and append
// thereafter. The definition revision is checked in the write transaction so
// an edited/deleted view cannot be repopulated by an old in-flight search.
func (s *Store) SaveSearchCachePage(vf *VirtualFolder, accountID string, entries []CachedConversation, first bool) error {
	return database.RetryOnBusy(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		var revision time.Time
		err = tx.QueryRow(`SELECT updated_at FROM virtual_folders WHERE id=?`, vf.ID).Scan(&revision)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if !revision.Equal(vf.UpdatedAt) {
			return nil
		}
		if first {
			if _, err = tx.Exec(`DELETE FROM virtual_folder_cache WHERE virtual_folder_id=? AND account_id=?`, vf.ID, accountID); err != nil {
				return err
			}
		}
		var position, count int
		if err = tx.QueryRow(`SELECT COALESCE(MAX(position)+1,0), COUNT(*) FROM virtual_folder_cache WHERE virtual_folder_id=?`, vf.ID).Scan(&position, &count); err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.AccountID != accountID {
				return fmt.Errorf("search cache account mismatch")
			}
			var exists int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM virtual_folder_cache WHERE virtual_folder_id=? AND account_id=? AND conversation_id=?`, vf.ID, accountID, entry.ConversationID).Scan(&exists); err != nil {
				return err
			}
			if exists > 0 || count >= 200 {
				continue
			}
			if _, err = tx.Exec(`INSERT INTO virtual_folder_cache(virtual_folder_id,position,account_id,conversation_id) VALUES(?,?,?,?)`, vf.ID, position, accountID, entry.ConversationID); err != nil {
				return err
			}
			position++
			count++
		}
		if _, err = tx.Exec(`INSERT INTO virtual_folder_cache_meta(virtual_folder_id,refreshed_at) VALUES(?,?) ON CONFLICT(virtual_folder_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, vf.ID, time.Now().UTC().Unix()); err != nil {
			return err
		}
		return tx.Commit()
	}, busyRetryAttempts, busyRetryBaseDelay)
}

// detachedSaveBusyTimeoutMS bounds how long a detached, best-effort cache
// write waits on the write lock. The modernc driver does not interrupt a
// BEGIN IMMEDIATE busy wait when the context expires, so the bound comes
// from lowering busy_timeout on a pinned connection instead; the connection
// is restored to the database's configured timeout before returning to the
// pool.
const detachedSaveBusyTimeoutMS = 2000

// SaveCacheContext is SaveCache for detached, best-effort cache maintenance:
// no retry loop, and the lock wait is bounded by the lowered busy_timeout
// above - cancellation safety rests on that override, not on the context,
// which the driver ignores during the busy wait. An abandoned refresh
// therefore frees its pool connection within detachedSaveBusyTimeoutMS.
func (s *Store) SaveCacheContext(ctx context.Context, virtualFolderID string, entries []CachedConversation) error {
	if virtualFolderID == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", detachedSaveBusyTimeoutMS)); err != nil {
		return fmt.Errorf("failed to bound busy timeout: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout = %d", s.db.BusyTimeoutMS()))
	}()
	return s.saveCacheTx(ctx, conn, virtualFolderID, entries)
}

// Get returns a single virtual folder by its ID
func (s *Store) Get(id string) (*VirtualFolder, error) {
	vf := &VirtualFolder{}

	err := s.db.QueryRow(`
		SELECT id, name, order_index, folder_mode, read_status, reply_status, star_status, date_window, search_query, created_at, updated_at
		FROM virtual_folders
		WHERE id = ?
	`, id).Scan(&vf.ID, &vf.Name, &vf.OrderIndex, &vf.FolderMode, &vf.ReadStatus, &vf.ReplyStatus, &vf.StarStatus, &vf.DateWindow, &vf.SearchQuery, &vf.CreatedAt, &vf.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder: %w", err)
	}

	// Get members
	rows, err := s.db.Query(`
		SELECT folder_id
		FROM virtual_folder_members
		WHERE virtual_folder_id = ?
		ORDER BY folder_id
	`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtual folder members: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var folderID string
		if err := rows.Scan(&folderID); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		vf.FolderIDs = append(vf.FolderIDs, folderID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if vf.FolderIDs == nil {
		vf.FolderIDs = []string{}
	}
	if err := vf.normalizeCriteria(); err != nil {
		return nil, fmt.Errorf("invalid virtual folder criteria: %w", err)
	}

	return vf, nil
}

// List returns all virtual folders ordered by order_index
func (s *Store) List() ([]*VirtualFolder, error) {
	rows, err := s.db.Query(`
		SELECT v.id, v.name, v.order_index, v.folder_mode, v.read_status, v.reply_status, v.star_status, v.date_window, v.search_query, v.created_at, v.updated_at, m.folder_id
		FROM virtual_folders v
		LEFT JOIN virtual_folder_members m ON v.id = m.virtual_folder_id
		ORDER BY v.order_index ASC, v.name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query virtual folders: %w", err)
	}
	defer rows.Close()

	vfMap := make(map[string]*VirtualFolder)
	var orderedList []*VirtualFolder

	for rows.Next() {
		var id, name string
		var orderIndex int
		var folderMode, readStatus, replyStatus, starStatus, dateWindow, searchQuery string
		var createdAt, updatedAt time.Time
		var folderID sql.NullString

		if err := rows.Scan(&id, &name, &orderIndex, &folderMode, &readStatus, &replyStatus, &starStatus, &dateWindow, &searchQuery, &createdAt, &updatedAt, &folderID); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		vf, exists := vfMap[id]
		if !exists {
			vf = &VirtualFolder{
				ID:          id,
				Name:        name,
				OrderIndex:  orderIndex,
				FolderMode:  folderMode,
				ReadStatus:  readStatus,
				ReplyStatus: replyStatus,
				StarStatus:  starStatus,
				DateWindow:  dateWindow,
				SearchQuery: searchQuery,
				CreatedAt:   createdAt,
				UpdatedAt:   updatedAt,
				FolderIDs:   []string{},
			}
			vfMap[id] = vf
			orderedList = append(orderedList, vf)
		}

		if folderID.Valid {
			vf.FolderIDs = append(vf.FolderIDs, folderID.String)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	for _, vf := range orderedList {
		if err := vf.normalizeCriteria(); err != nil {
			return nil, fmt.Errorf("invalid virtual folder criteria for %s: %w", vf.ID, err)
		}
	}

	return orderedList, nil
}

// QueryFolderIDs returns the selected real folders or their complement across enabled accounts.
func (s *Store) QueryFolderIDs(vf *VirtualFolder) ([]string, error) {
	if vf == nil {
		return nil, fmt.Errorf("virtual folder cannot be nil")
	}
	if err := vf.normalizeCriteria(); err != nil {
		return nil, err
	}
	if vf.FolderMode == FolderModeInclude {
		return append([]string(nil), vf.FolderIDs...), nil
	}

	query := `SELECT f.id FROM folders f JOIN accounts a ON a.id = f.account_id WHERE a.enabled = 1`
	args := make([]any, 0, len(vf.FolderIDs))
	if len(vf.FolderIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(vf.FolderIDs)), ",")
		query += ` AND f.id NOT IN (` + placeholders + `)`
		for _, folderID := range vf.FolderIDs {
			args = append(args, folderID)
		}
	}
	query += ` ORDER BY f.id`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve excluded virtual-folder members: %w", err)
	}
	defer rows.Close()

	folderIDs := make([]string, 0)
	for rows.Next() {
		var folderID string
		if err := rows.Scan(&folderID); err != nil {
			return nil, fmt.Errorf("failed to scan virtual-folder member: %w", err)
		}
		folderIDs = append(folderIDs, folderID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read virtual-folder members: %w", err)
	}
	return folderIDs, nil
}

// UnreadCounts returns unread conversation counts for every virtual folder in one query.
func (s *Store) UnreadCounts() (map[string]int, error) {
	cutoff7d := time.Now().UTC().Add(-7 * 24 * time.Hour).Unix()
	cutoff30d := time.Now().UTC().Add(-30 * 24 * time.Hour).Unix()
	cutoff90d := time.Now().UTC().Add(-90 * 24 * time.Hour).Unix()
	cutoff1y := time.Now().UTC().AddDate(-1, 0, 0).Unix()

	rows, err := s.db.Query(fmt.Sprintf(`
		WITH scoped_messages AS (
			SELECT v.id AS virtual_folder_id, v.read_status, v.reply_status, v.star_status, v.date_window,
				m.account_id, COALESCE(m.thread_id, m.id) AS conversation_id,
				m.is_read, m.is_answered, m.is_starred, %s AS date_epoch
			FROM virtual_folders v
			CROSS JOIN folders f
			INNER JOIN accounts a ON a.id = f.account_id AND a.enabled = 1
			INNER JOIN messages m ON m.folder_id = f.id
			WHERE (v.folder_mode = 'include' AND EXISTS (
				SELECT 1 FROM virtual_folder_members selected
				WHERE selected.virtual_folder_id = v.id AND selected.folder_id = f.id
			)) OR (v.folder_mode = 'exclude' AND NOT EXISTS (
				SELECT 1 FROM virtual_folder_members excluded
				WHERE excluded.virtual_folder_id = v.id AND excluded.folder_id = f.id
			))
		), virtual_folder_conversations AS (
			SELECT virtual_folder_id, read_status, reply_status, star_status, date_window, account_id, conversation_id,
				SUM(CASE WHEN is_read = 0 THEN 1 ELSE 0 END) AS unread_count,
				MAX(CASE WHEN is_answered = 1 THEN 1 ELSE 0 END) AS has_answered,
				MAX(CASE WHEN is_starred = 1 THEN 1 ELSE 0 END) AS is_starred,
				MAX(date_epoch) AS latest_date_epoch
			FROM scoped_messages
			GROUP BY virtual_folder_id, account_id, conversation_id
		)
		SELECT v.id, COUNT(CASE
			WHEN c.unread_count > 0
				AND c.read_status != 'read'
				AND (c.reply_status = 'all'
					OR (c.reply_status = 'replied' AND c.has_answered = 1)
					OR (c.reply_status = 'unreplied' AND c.has_answered = 0))
				AND (c.star_status = 'all'
					OR (c.star_status = 'starred' AND c.is_starred = 1)
					OR (c.star_status = 'unstarred' AND c.is_starred = 0))
				AND (c.date_window = 'all'
					OR (c.date_window = '7d' AND c.latest_date_epoch >= ?)
					OR (c.date_window = '30d' AND c.latest_date_epoch >= ?)
					OR (c.date_window = '90d' AND c.latest_date_epoch >= ?)
					OR (c.date_window = '1y' AND c.latest_date_epoch >= ?))
			THEN 1 END)
		FROM virtual_folders v
		LEFT JOIN virtual_folder_conversations c ON c.virtual_folder_id = v.id
		GROUP BY v.id
	`, database.UnixEpochSQL("m.date")), cutoff7d, cutoff30d, cutoff90d, cutoff1y)
	if err != nil {
		return nil, fmt.Errorf("failed to query virtual folder unread counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, fmt.Errorf("failed to scan virtual folder unread count: %w", err)
		}
		counts[id] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read virtual folder unread counts: %w", err)
	}
	return counts, nil
}

// Reorder assigns each virtual folder its position in the supplied ID list.
func (s *Store) reorder(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin virtual folder reorder: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit
	for index, id := range ids {
		result, err := tx.Exec(`UPDATE virtual_folders SET order_index = ?, updated_at = ? WHERE id = ?`, index, time.Now().UTC(), id)
		if err != nil {
			return fmt.Errorf("failed to reorder virtual folder %s: %w", id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to confirm virtual folder reorder: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("virtual folder not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit virtual folder reorder: %w", err)
	}
	return nil
}

// update modifies an existing virtual folder and its folder members (retried on SQLITE_BUSY by the public wrapper)
func (s *Store) update(vf *VirtualFolder) error {
	if vf == nil {
		return fmt.Errorf("virtual folder cannot be nil")
	}
	vf.Name = strings.TrimSpace(vf.Name)
	if vf.ID == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	if vf.Name == "" {
		return fmt.Errorf("virtual folder name cannot be empty")
	}
	if err := vf.normalizeCriteria(); err != nil {
		return err
	}
	vf.UpdatedAt = time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	// Cached provider-resolved membership belongs to one exact definition.
	var oldMode, oldRead, oldReply, oldStar, oldDate, oldQuery string
	if err := tx.QueryRow(`SELECT folder_mode,read_status,reply_status,star_status,date_window,search_query FROM virtual_folders WHERE id=?`, vf.ID).Scan(&oldMode, &oldRead, &oldReply, &oldStar, &oldDate, &oldQuery); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT folder_id FROM virtual_folder_members WHERE virtual_folder_id=? ORDER BY folder_id`, vf.ID)
	if err != nil {
		return err
	}
	oldIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		oldIDs = append(oldIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	newIDs := append([]string{}, vf.FolderIDs...)
	sort.Strings(newIDs)
	changed := oldMode != vf.FolderMode || oldRead != vf.ReadStatus || oldReply != vf.ReplyStatus || oldStar != vf.StarStatus || oldDate != vf.DateWindow || oldQuery != vf.SearchQuery || !slices.Equal(oldIDs, newIDs)
	if changed {
		if _, err := tx.Exec(`DELETE FROM virtual_folder_cache WHERE virtual_folder_id=?`, vf.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM virtual_folder_cache_meta WHERE virtual_folder_id=?`, vf.ID); err != nil {
			return err
		}
	}

	result, err := tx.Exec(`
		UPDATE virtual_folders
		SET name = ?, order_index = ?, folder_mode = ?, read_status = ?, reply_status = ?, star_status = ?, date_window = ?, search_query = ?, updated_at = ?
		WHERE id = ?
	`, vf.Name, vf.OrderIndex, vf.FolderMode, vf.ReadStatus, vf.ReplyStatus, vf.StarStatus, vf.DateWindow, vf.SearchQuery, vf.UpdatedAt, vf.ID)
	if err != nil {
		return fmt.Errorf("failed to update virtual folder: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("virtual folder not found: %s", vf.ID)
	}

	// Delete existing members
	_, err = tx.Exec(`DELETE FROM virtual_folder_members WHERE virtual_folder_id = ?`, vf.ID)
	if err != nil {
		return fmt.Errorf("failed to delete existing members: %w", err)
	}

	// Insert updated members
	for _, folderID := range vf.FolderIDs {
		_, err = tx.Exec(`
			INSERT INTO virtual_folder_members (virtual_folder_id, folder_id)
			VALUES (?, ?)
		`, vf.ID, folderID)
		if err != nil {
			return fmt.Errorf("failed to insert virtual folder member %s: %w", folderID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// delete removes a virtual folder and its members from the database (retried on SQLITE_BUSY by the public wrapper)
func (s *Store) delete(id string) error {
	if id == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	// Members are deleted automatically by cascade or manually
	_, err = tx.Exec(`DELETE FROM virtual_folder_members WHERE virtual_folder_id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete virtual folder members: %w", err)
	}

	// FK cascades depend on the foreign_keys pragma, so clear the cached index
	// and its metadata explicitly.
	if _, err = tx.Exec(`DELETE FROM virtual_folder_cache WHERE virtual_folder_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete virtual folder cache: %w", err)
	}
	if _, err = tx.Exec(`DELETE FROM virtual_folder_cache_meta WHERE virtual_folder_id = ?`, id); err != nil {
		return fmt.Errorf("failed to delete virtual folder cache meta: %w", err)
	}

	result, err := tx.Exec(`DELETE FROM virtual_folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete virtual folder: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("virtual folder not found: %s", id)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// CachedConversation is one entry of a virtual folder's cached conversation index.
type CachedConversation struct {
	AccountID      string
	ConversationID string
}

// SaveCache atomically replaces a virtual folder's cached conversation index
// and stamps the refresh time.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (s *Store) saveCache(virtualFolderID string, entries []CachedConversation) error {
	if virtualFolderID == "" {
		return fmt.Errorf("virtual folder ID cannot be empty")
	}
	return s.saveCacheTx(context.Background(), s.db.DB, virtualFolderID, entries)
}

// saveCacheTx runs the cache replacement in one transaction. The context is
// threaded through Begin and every statement, but note the modernc driver
// does not interrupt a BEGIN IMMEDIATE busy wait on context expiry: callers
// that need a bounded lock wait must bound it with busy_timeout, as
// SaveCacheContext does.
func (s *Store) saveCacheTx(ctx context.Context, beginner txBeginner, virtualFolderID string, entries []CachedConversation) error {
	tx, err := beginner.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	// A local maintenance pass may have started before the definition changed
	// into a provider search. Recheck inside the write transaction.
	var query string
	if err = tx.QueryRowContext(ctx, `SELECT search_query FROM virtual_folders WHERE id=?`, virtualFolderID).Scan(&query); err != nil {
		return err
	}
	if query != "" {
		return nil
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM virtual_folder_cache WHERE virtual_folder_id = ?`, virtualFolderID); err != nil {
		return fmt.Errorf("failed to clear virtual folder cache: %w", err)
	}
	for position, entry := range entries {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO virtual_folder_cache (virtual_folder_id, position, account_id, conversation_id)
			VALUES (?, ?, ?, ?)
		`, virtualFolderID, position, entry.AccountID, entry.ConversationID); err != nil {
			return fmt.Errorf("failed to insert virtual folder cache entry %d: %w", position, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO virtual_folder_cache_meta (virtual_folder_id, refreshed_at)
		VALUES (?, ?)
		ON CONFLICT(virtual_folder_id) DO UPDATE SET refreshed_at = excluded.refreshed_at
	`, virtualFolderID, time.Now().UTC().Unix()); err != nil {
		return fmt.Errorf("failed to stamp virtual folder cache meta: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit virtual folder cache: %w", err)
	}
	return nil
}

// LoadCache returns a virtual folder's cached conversation index in display
// order plus the last refresh time. found is false when nothing was cached yet.
func (s *Store) LoadCache(virtualFolderID string) ([]CachedConversation, time.Time, bool, error) {
	if virtualFolderID == "" {
		return nil, time.Time{}, false, fmt.Errorf("virtual folder ID cannot be empty")
	}
	var refreshedAt int64
	err := s.db.QueryRow(`SELECT refreshed_at FROM virtual_folder_cache_meta WHERE virtual_folder_id = ?`, virtualFolderID).Scan(&refreshedAt)
	if err == sql.ErrNoRows {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("failed to read virtual folder cache meta: %w", err)
	}
	rows, err := s.db.Query(`
		SELECT account_id, conversation_id
		FROM virtual_folder_cache
		WHERE virtual_folder_id = ?
		ORDER BY position ASC
	`, virtualFolderID)
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("failed to read virtual folder cache: %w", err)
	}
	defer rows.Close()
	entries := make([]CachedConversation, 0)
	for rows.Next() {
		var entry CachedConversation
		if err := rows.Scan(&entry.AccountID, &entry.ConversationID); err != nil {
			return nil, time.Time{}, false, fmt.Errorf("failed to scan virtual folder cache entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, false, fmt.Errorf("rows error: %w", err)
	}
	return entries, time.Unix(refreshedAt, 0).UTC(), true, nil
}
