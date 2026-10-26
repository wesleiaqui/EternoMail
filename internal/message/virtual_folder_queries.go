package message

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/database"
)

// VirtualFolderCriteria holds the message-state and date criteria for one saved view.
type VirtualFolderCriteria struct {
	ReadStatus  string
	ReplyStatus string
	StarStatus  string
	DateWindow  string
}

// ListVirtualFolderConversations returns conversations matching the saved view criteria.
func (s *Store) ListVirtualFolderConversations(folderIDs []string, offset, limit int, sortOrder string, criteria VirtualFolderCriteria) ([]*Conversation, error) {
	results, err := s.queryVirtualFolderConversations(folderIDs, "", offset, limit, sortOrder, criteria)
	if err != nil {
		return nil, err
	}
	conversations := make([]*Conversation, 0, len(results))
	for _, result := range results {
		conversations = append(conversations, &result.Conversation)
	}
	return conversations, nil
}

// CountVirtualFolderConversations counts conversations matching the saved view criteria.
func (s *Store) CountVirtualFolderConversations(folderIDs []string, criteria VirtualFolderCriteria) (int, error) {
	if len(folderIDs) == 0 {
		return 0, nil
	}
	cte, args, err := virtualFolderConversationCTE(folderIDs)
	if err != nil {
		return 0, err
	}
	where, criteriaArgs, err := virtualFolderCriteriaWhere(criteria)
	if err != nil {
		return 0, err
	}
	args = append(args, criteriaArgs...)
	query := cte + ` SELECT COUNT(*) FROM virtual_folder_conversations c WHERE 1 = 1` + where
	var count int
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count virtual-folder conversations: %w", err)
	}
	return count, nil
}

// CountVirtualFolderUnreadConversations counts unread conversations after applying other saved criteria.
func (s *Store) CountVirtualFolderUnreadConversations(folderIDs []string, criteria VirtualFolderCriteria) (int, error) {
	criteria.ReadStatus = "unread"
	return s.CountVirtualFolderConversations(folderIDs, criteria)
}

// SearchVirtualFolderConversations searches for matching text while applying saved criteria to the whole conversation.
func (s *Store) SearchVirtualFolderConversations(folderIDs []string, query string, offset, limit int, criteria VirtualFolderCriteria) ([]*ConversationSearchResult, int, error) {
	if len(folderIDs) == 0 || strings.TrimSpace(query) == "" {
		return []*ConversationSearchResult{}, 0, nil
	}
	cte, args, err := virtualFolderConversationCTE(folderIDs)
	if err != nil {
		return nil, 0, err
	}
	matchCTE, matchArgs := virtualFolderMatchesCTE(prepareFTSQuery(query))
	criteriaWhere, criteriaArgs, err := virtualFolderCriteriaWhere(criteria)
	if err != nil {
		return nil, 0, err
	}
	args = append(args, matchArgs...)
	args = append(args, criteriaArgs...)
	countQuery := cte + matchCTE + `
		SELECT COUNT(*)
		FROM virtual_folder_conversations c
		INNER JOIN virtual_folder_matches matches
			ON matches.account_id = c.account_id AND matches.conversation_id = c.conversation_id
		WHERE 1 = 1` + criteriaWhere
	var totalCount int
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count virtual-folder search results: %w", err)
	}
	if totalCount == 0 {
		return []*ConversationSearchResult{}, 0, nil
	}

	searchQuery := cte + matchCTE + `
		SELECT c.conversation_id, matches.subject, matches.snippet, matches.from_name,
			c.message_count, c.unread_count, c.has_attachments, c.is_starred,
			c.latest_date_epoch, c.message_ids, c.is_encrypted, c.inbox_category,
			c.account_id, c.account_name, c.account_color, c.folder_id,
			c.folder_name, c.folder_type, c.participants_json
		FROM virtual_folder_conversations c
		INNER JOIN virtual_folder_matches matches
			ON matches.account_id = c.account_id AND matches.conversation_id = c.conversation_id
		WHERE 1 = 1` + criteriaWhere + `
		ORDER BY c.latest_date_epoch DESC
		LIMIT ? OFFSET ?`
	searchArgs := append([]any(nil), args...)
	searchArgs = append(searchArgs, limit, offset)
	rows, err := s.db.QueryContext(ctx, searchQuery, searchArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search virtual-folder conversations: %w", err)
	}
	defer rows.Close()

	results, err := scanVirtualFolderSearchResults(rows, query)
	if err != nil {
		return nil, 0, err
	}
	return results, totalCount, nil
}

func (s *Store) queryVirtualFolderConversations(folderIDs []string, query string, offset, limit int, sortOrder string, criteria VirtualFolderCriteria) ([]*ConversationSearchResult, error) {
	if len(folderIDs) == 0 {
		return []*ConversationSearchResult{}, nil
	}
	cte, args, err := virtualFolderConversationCTE(folderIDs)
	if err != nil {
		return nil, err
	}
	criteriaWhere, criteriaArgs, err := virtualFolderCriteriaWhere(criteria)
	if err != nil {
		return nil, err
	}
	args = append(args, criteriaArgs...)
	orderClause := "ORDER BY c.latest_date_epoch DESC"
	if sortOrder == "oldest" {
		orderClause = "ORDER BY c.latest_date_epoch ASC"
	}
	queryText := cte + `
		SELECT c.conversation_id, c.subject, c.snippet, c.from_name,
			c.message_count, c.unread_count, c.has_attachments, c.is_starred,
			c.latest_date_epoch, c.message_ids, c.is_encrypted, c.inbox_category,
			c.account_id, c.account_name, c.account_color, c.folder_id,
			c.folder_name, c.folder_type, c.participants_json
		FROM virtual_folder_conversations c
		WHERE 1 = 1` + criteriaWhere + `
		` + orderClause + `
		LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, queryText, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query virtual-folder conversations: %w", err)
	}
	defer rows.Close()
	return scanVirtualFolderSearchResults(rows, query)
}

func virtualFolderConversationCTE(folderIDs []string) (string, []any, error) {
	if len(folderIDs) == 0 {
		return "", nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(folderIDs)), ",")
	query := fmt.Sprintf(`
		WITH scoped_messages AS (
			SELECT m.rowid AS row_id, m.id, m.account_id, m.folder_id,
				COALESCE(m.thread_id, m.id) AS conversation_id,
				m.subject, m.snippet, m.from_name, m.from_email, m.date,
				m.is_read, m.is_answered, m.is_starred, m.has_attachments,
				m.smime_encrypted, m.pgp_encrypted, m.inbox_category,
				a.name AS account_name, a.color AS account_color,
				f.name AS folder_name, f.folder_type
			FROM messages m
			INNER JOIN folders f ON f.id = m.folder_id
			INNER JOIN accounts a ON a.id = m.account_id AND a.enabled = 1
			WHERE m.folder_id IN (%s)
		), virtual_folder_conversations AS (
			SELECT account_id, conversation_id,
				COALESCE(MIN(subject), '') AS subject,
				MAX(snippet) AS snippet,
				MIN(from_name) AS from_name,
				COUNT(*) AS message_count,
				SUM(CASE WHEN is_read = 0 THEN 1 ELSE 0 END) AS unread_count,
				MAX(CASE WHEN has_attachments = 1 THEN 1 ELSE 0 END) AS has_attachments,
				MAX(CASE WHEN is_starred = 1 THEN 1 ELSE 0 END) AS is_starred,
				MAX(%s) AS latest_date_epoch,
				GROUP_CONCAT(id) AS message_ids,
				MAX(CASE WHEN smime_encrypted = 1 OR pgp_encrypted = 1 THEN 1 ELSE 0 END) AS is_encrypted,
				MAX(inbox_category) AS inbox_category,
				MIN(account_name) AS account_name,
				MIN(account_color) AS account_color,
				MIN(folder_id) AS folder_id,
				MIN(folder_name) AS folder_name,
				MIN(folder_type) AS folder_type,
				MAX(CASE WHEN is_answered = 1 THEN 1 ELSE 0 END) AS has_answered,
				json_group_array(DISTINCT json_object('name', from_name, 'email', from_email)) AS participants_json
			FROM scoped_messages
			GROUP BY account_id, conversation_id
		)
	`, placeholders, database.UnixEpochSQL("date"))
	args := make([]any, len(folderIDs))
	for index, folderID := range folderIDs {
		args[index] = folderID
	}
	return query, args, nil
}

func virtualFolderMatchesCTE(ftsQuery string) (string, []any) {
	return `, virtual_folder_matches AS (
		SELECT sm.account_id, sm.conversation_id, COALESCE(MIN(sm.subject), '') AS subject,
			MAX(sm.snippet) AS snippet, MIN(sm.from_name) AS from_name
		FROM scoped_messages sm
		INNER JOIN messages_fts ON messages_fts.rowid = sm.row_id
		WHERE messages_fts MATCH ?
		GROUP BY sm.account_id, sm.conversation_id
	)
	`, []any{ftsQuery}
}

func virtualFolderCriteriaWhere(criteria VirtualFolderCriteria) (string, []any, error) {
	readStatus := criteria.ReadStatus
	if readStatus == "" {
		readStatus = "all"
	}
	replyStatus := criteria.ReplyStatus
	if replyStatus == "" {
		replyStatus = "all"
	}
	starStatus := criteria.StarStatus
	if starStatus == "" {
		starStatus = "all"
	}
	dateWindow := criteria.DateWindow
	if dateWindow == "" {
		dateWindow = "all"
	}
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 1)
	switch readStatus {
	case "all":
	case "unread":
		conditions = append(conditions, "c.unread_count > 0")
	case "read":
		conditions = append(conditions, "c.unread_count = 0")
	default:
		return "", nil, fmt.Errorf("invalid virtual-folder read status: %s", readStatus)
	}
	switch replyStatus {
	case "all":
	case "replied":
		conditions = append(conditions, "c.has_answered = 1")
	case "unreplied":
		conditions = append(conditions, "c.has_answered = 0")
	default:
		return "", nil, fmt.Errorf("invalid virtual-folder reply status: %s", replyStatus)
	}
	switch starStatus {
	case "all":
	case "starred":
		conditions = append(conditions, "c.is_starred = 1")
	case "unstarred":
		conditions = append(conditions, "c.is_starred = 0")
	default:
		return "", nil, fmt.Errorf("invalid virtual-folder star status: %s", starStatus)
	}
	switch dateWindow {
	case "all":
	case "7d":
		conditions = append(conditions, "c.latest_date_epoch >= ?")
		args = append(args, time.Now().UTC().Add(-7*24*time.Hour).Unix())
	case "30d":
		conditions = append(conditions, "c.latest_date_epoch >= ?")
		args = append(args, time.Now().UTC().Add(-30*24*time.Hour).Unix())
	case "90d":
		conditions = append(conditions, "c.latest_date_epoch >= ?")
		args = append(args, time.Now().UTC().Add(-90*24*time.Hour).Unix())
	case "1y":
		conditions = append(conditions, "c.latest_date_epoch >= ?")
		args = append(args, time.Now().UTC().AddDate(-1, 0, 0).Unix())
	default:
		return "", nil, fmt.Errorf("invalid virtual-folder date window: %s", dateWindow)
	}
	if len(conditions) == 0 {
		return "", args, nil
	}
	return " AND " + strings.Join(conditions, " AND "), args, nil
}

func scanVirtualFolderSearchResults(rows *sql.Rows, query string) ([]*ConversationSearchResult, error) {
	var results []*ConversationSearchResult
	for rows.Next() {
		result := &ConversationSearchResult{}
		var latestDateEpoch sql.NullInt64
		var snippet, fromName, messageIDs, participantsJSON sql.NullString
		err := rows.Scan(
			&result.ThreadID,
			&result.Subject,
			&snippet,
			&fromName,
			&result.MessageCount,
			&result.UnreadCount,
			&result.HasAttachments,
			&result.IsStarred,
			&latestDateEpoch,
			&messageIDs,
			&result.IsEncrypted,
			&result.InboxCategory,
			&result.AccountID,
			&result.AccountName,
			&result.AccountColor,
			&result.FolderID,
			&result.FolderName,
			&result.FolderType,
			&participantsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan virtual-folder conversation: %w", err)
		}
		if snippet.Valid {
			result.Snippet = snippet.String
		}
		if fromName.Valid {
			result.HighlightedFromName = highlightMatches(fromName.String, query)
		}
		if latestDateEpoch.Valid {
			result.LatestDate = time.Unix(latestDateEpoch.Int64, 0).UTC()
		}
		if messageIDs.Valid && messageIDs.String != "" {
			result.MessageIDs = strings.Split(messageIDs.String, ",")
		}
		if participantsJSON.Valid {
			result.Participants = parseParticipantsJSON(participantsJSON.String)
		}
		if query != "" {
			result.HighlightedSubject = highlightMatches(result.Subject, query)
			result.HighlightedSnippet = highlightMatches(result.Snippet, query)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate virtual-folder conversations: %w", err)
	}
	return results, nil
}
