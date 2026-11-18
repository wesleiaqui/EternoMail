package mailfilter

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/hkdb/aerion/internal/message"
	"github.com/rs/zerolog"
)

// ruleColumns is the select list shared by every rule read; keep scanRule in
// the same order.
const ruleColumns = `id, name, enabled, order_index, account_id, account_ids, folder_ids, from_contains, to_contains, subject_contains, list_id_contains, dest_folder_id, action_mark_read, action_archive, action_trash, created_at, updated_at`

// Store provides persistence operations for local filter rules.
type Store struct {
	db  *database.DB
	log zerolog.Logger
}

// NewStore creates a new filter rule store.
func NewStore(db *database.DB) *Store {
	s := &Store{
		db:  db,
		log: logging.WithComponent("mailfilter-store"),
	}
	if err := s.ensureTables(); err != nil {
		// Table creation is idempotent; a failure here is fatal because every
		// filter operation depends on the schema. Surface it at startup rather
		// than failing with a confusing "no such table" on first use.
		panic(fmt.Errorf("mailfilter store: ensure tables: %w", err))
	}
	return s
}

// ensureTables confirms the migration-created filter schema is present.
func (s *Store) ensureTables() error {
	var tableCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mail_filter_rules'`).Scan(&tableCount); err != nil {
		return err
	}
	if tableCount == 0 {
		return fmt.Errorf("mail filter schema is missing; run database migrations")
	}
	return nil
}

// Create inserts a new filter rule, assigning its ID and timestamps.
func (s *Store) Create(rule *Rule) error {
	if rule == nil {
		return fmt.Errorf("filter rule cannot be nil")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		return fmt.Errorf("filter rule name cannot be empty")
	}
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now

	_, err := s.db.Exec(`
		INSERT INTO mail_filter_rules (id, name, enabled, order_index, account_id, account_ids, folder_ids, from_contains, to_contains, subject_contains, list_id_contains, dest_folder_id, action_mark_read, action_archive, action_trash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID,
		rule.Name,
		boolToInt(rule.Enabled),
		rule.OrderIndex,
		rule.AccountID,
		marshalStrings(rule.AccountIDs),
		marshalStrings(rule.FolderIDs),
		marshalStrings(rule.FromContains),
		marshalStrings(rule.ToContains),
		marshalStrings(rule.SubjectContains),
		marshalStrings(rule.ListIDContains),
		rule.DestFolderID,
		boolToInt(rule.ActionMarkRead),
		boolToInt(rule.ActionArchive),
		boolToInt(rule.ActionTrash),
		rule.CreatedAt,
		rule.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert filter rule: %w", err)
	}
	return nil
}

// Get returns a single filter rule by its ID, or nil when not found.
func (s *Store) Get(id string) (*Rule, error) {
	row := s.db.QueryRow(`SELECT `+ruleColumns+` FROM mail_filter_rules WHERE id = ?`, id)
	return scanRule(row)
}

// List returns all filter rules ordered by priority (order_index, then name).
func (s *Store) List() ([]*Rule, error) {
	rows, err := s.db.Query(`SELECT ` + ruleColumns + ` FROM mail_filter_rules ORDER BY order_index ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list filter rules: %w", err)
	}
	return collectRules(rows)
}

// ListEnabled returns only the enabled filter rules in priority order. This is
// the hot path used for every newly synced message, so it filters in SQL.
func (s *Store) ListEnabled() ([]*Rule, error) {
	rows, err := s.db.Query(`SELECT ` + ruleColumns + ` FROM mail_filter_rules WHERE enabled = 1 ORDER BY order_index ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled filter rules: %w", err)
	}
	return collectRules(rows)
}

// Update modifies an existing filter rule and refreshes its UpdatedAt.
func (s *Store) Update(rule *Rule) error {
	if rule == nil {
		return fmt.Errorf("filter rule cannot be nil")
	}
	if rule.ID == "" {
		return fmt.Errorf("filter rule ID cannot be empty")
	}
	rule.Name = strings.TrimSpace(rule.Name)
	if rule.Name == "" {
		return fmt.Errorf("filter rule name cannot be empty")
	}
	rule.UpdatedAt = time.Now().UTC()

	result, err := s.db.Exec(`
		UPDATE mail_filter_rules
		SET name = ?, enabled = ?, order_index = ?, account_id = ?, account_ids = ?, folder_ids = ?, from_contains = ?, to_contains = ?, subject_contains = ?, list_id_contains = ?, dest_folder_id = ?, action_mark_read = ?, action_archive = ?, action_trash = ?, updated_at = ?
		WHERE id = ?
	`,
		rule.Name,
		boolToInt(rule.Enabled),
		rule.OrderIndex,
		rule.AccountID,
		marshalStrings(rule.AccountIDs),
		marshalStrings(rule.FolderIDs),
		marshalStrings(rule.FromContains),
		marshalStrings(rule.ToContains),
		marshalStrings(rule.SubjectContains),
		marshalStrings(rule.ListIDContains),
		rule.DestFolderID,
		boolToInt(rule.ActionMarkRead),
		boolToInt(rule.ActionArchive),
		boolToInt(rule.ActionTrash),
		rule.UpdatedAt,
		rule.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update filter rule: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to confirm filter rule update: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("filter rule not found: %s", rule.ID)
	}
	return nil
}

// Delete removes a filter rule by ID.
func (s *Store) Delete(id string) error {
	if id == "" {
		return fmt.Errorf("filter rule ID cannot be empty")
	}
	_, err := s.db.Exec(`DELETE FROM mail_filter_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete filter rule: %w", err)
	}
	return nil
}

// Reorder assigns each filter rule its position in the supplied ID list.
func (s *Store) Reorder(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin filter rule reorder: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			s.log.Warn().Err(err).Msg("Failed to rollback filter rule reorder transaction")
		}
	}()
	for index, id := range ids {
		result, err := tx.Exec(`UPDATE mail_filter_rules SET order_index = ?, updated_at = ? WHERE id = ?`, index, time.Now().UTC(), id)
		if err != nil {
			return fmt.Errorf("failed to reorder filter rule %s: %w", id, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to confirm filter rule reorder: %w", err)
		}
		if rows == 0 {
			return fmt.Errorf("filter rule not found: %s", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit filter rule reorder: %w", err)
	}
	return nil
}

// MatchingMessageIDs uses the same matcher as incoming mail, including Unicode
// and decoded recipient semantics. Iterate headers rather than duplicating the
// matcher in SQLite, whose built-in LOWER only folds ASCII.
func (s *Store) MatchingMessageIDs(rule *Rule, limit int) ([]string, error) {
	if rule == nil {
		return nil, fmt.Errorf("filter rule cannot be nil")
	}
	rows, err := s.db.Query(`SELECT m.id, m.account_id, m.folder_id, COALESCE(m.from_name,''), COALESCE(m.from_email,''), COALESCE(m.to_list,''), COALESCE(m.cc_list,''), COALESCE(m.subject,''), COALESCE(li.list_id,'') FROM messages m LEFT JOIN message_list_ids li ON li.message_id=m.id WHERE m.is_deleted=0 AND m.is_draft=0 ORDER BY m.date DESC, m.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	effective := *rule
	effective.Enabled = true
	var ids []string
	for rows.Next() {
		var m message.Message
		if err := rows.Scan(&m.ID, &m.AccountID, &m.FolderID, &m.FromName, &m.FromEmail, &m.ToList, &m.CcList, &m.Subject, &m.ListID); err != nil {
			return nil, err
		}
		if Match(&effective, FromMessage(&m)) {
			ids = append(ids, m.ID)
			if limit > 0 && len(ids) >= limit {
				break
			}
		}
	}
	return ids, rows.Err()
}

// CountMatching counts stored messages matching the same criteria as incoming mail.
func (s *Store) CountMatching(rule *Rule) (int, error) {
	ids, err := s.MatchingMessageIDs(rule, 0)
	return len(ids), err
}

// scanRule reads one rule from a row-scan source.
func scanRule(row interface {
	Scan(dest ...any) error
}) (*Rule, error) {
	rule := &Rule{}
	var enabled, markRead, archive, trash int
	var accountIDs, folderIDs, fromContains, toContains, subjectContains, listIDContains string
	err := row.Scan(
		&rule.ID,
		&rule.Name,
		&enabled,
		&rule.OrderIndex,
		&rule.AccountID,
		&accountIDs,
		&folderIDs,
		&fromContains,
		&toContains,
		&subjectContains,
		&listIDContains,
		&rule.DestFolderID,
		&markRead,
		&archive,
		&trash,
		&rule.CreatedAt,
		&rule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan filter rule: %w", err)
	}
	rule.Enabled = enabled != 0
	rule.AccountIDs = unmarshalStrings(accountIDs)
	rule.FolderIDs = unmarshalStrings(folderIDs)
	rule.FromContains = unmarshalStrings(fromContains)
	rule.ToContains = unmarshalStrings(toContains)
	rule.SubjectContains = unmarshalStrings(subjectContains)
	rule.ListIDContains = unmarshalStrings(listIDContains)
	rule.ActionMarkRead = markRead != 0
	rule.ActionArchive = archive != 0
	rule.ActionTrash = trash != 0
	return rule, nil
}

// collectRules drains a result set into a rule slice.
func collectRules(rows *sql.Rows) ([]*Rule, error) {
	defer rows.Close()
	var rules []*Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return rules, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func marshalStrings(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func unmarshalStrings(jsonValue string) []string {
	if strings.TrimSpace(jsonValue) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(jsonValue), &values); err != nil {
		return []string{}
	}
	return values
}
