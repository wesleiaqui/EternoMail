package virtualfolder

import (
	"database/sql"
	"fmt"
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

// Create inserts a new virtual folder and its folder members
func (s *Store) Create(vf *VirtualFolder) error {
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
		INSERT INTO virtual_folders (id, name, order_index, folder_mode, read_status, reply_status, star_status, date_window, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, vf.ID, vf.Name, vf.OrderIndex, vf.FolderMode, vf.ReadStatus, vf.ReplyStatus, vf.StarStatus, vf.DateWindow, vf.CreatedAt, vf.UpdatedAt)
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

// Get returns a single virtual folder by its ID
func (s *Store) Get(id string) (*VirtualFolder, error) {
	vf := &VirtualFolder{}

	err := s.db.QueryRow(`
		SELECT id, name, order_index, folder_mode, read_status, reply_status, star_status, date_window, created_at, updated_at
		FROM virtual_folders
		WHERE id = ?
	`, id).Scan(&vf.ID, &vf.Name, &vf.OrderIndex, &vf.FolderMode, &vf.ReadStatus, &vf.ReplyStatus, &vf.StarStatus, &vf.DateWindow, &vf.CreatedAt, &vf.UpdatedAt)

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
		SELECT v.id, v.name, v.order_index, v.folder_mode, v.read_status, v.reply_status, v.star_status, v.date_window, v.created_at, v.updated_at, m.folder_id
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
		var folderMode, readStatus, replyStatus, starStatus, dateWindow string
		var createdAt, updatedAt time.Time
		var folderID sql.NullString

		if err := rows.Scan(&id, &name, &orderIndex, &folderMode, &readStatus, &replyStatus, &starStatus, &dateWindow, &createdAt, &updatedAt, &folderID); err != nil {
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
func (s *Store) Reorder(ids []string) error {
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
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to confirm virtual folder reorder: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("virtual folder not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit virtual folder reorder: %w", err)
	}
	return nil
}

// Update modifies an existing virtual folder and its folder members
func (s *Store) Update(vf *VirtualFolder) error {
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

	result, err := tx.Exec(`
		UPDATE virtual_folders
		SET name = ?, order_index = ?, folder_mode = ?, read_status = ?, reply_status = ?, star_status = ?, date_window = ?, updated_at = ?
		WHERE id = ?
	`, vf.Name, vf.OrderIndex, vf.FolderMode, vf.ReadStatus, vf.ReplyStatus, vf.StarStatus, vf.DateWindow, vf.UpdatedAt, vf.ID)
	if err != nil {
		return fmt.Errorf("failed to update virtual folder: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
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

// Delete removes a virtual folder and its members from the database
func (s *Store) Delete(id string) error {
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

	result, err := tx.Exec(`DELETE FROM virtual_folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete virtual folder: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("virtual folder not found: %s", id)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
