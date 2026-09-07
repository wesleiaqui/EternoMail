package pgp

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Store manages PGP keys in the database
type Store struct {
	db  *sql.DB
	log zerolog.Logger
}

// NewStore creates a new PGP key store
func NewStore(db *sql.DB, log zerolog.Logger) *Store {
	return &Store{
		db:  db,
		log: log,
	}
}

// SaveKey stores a user's PGP key
func (s *Store) SaveKey(key *Key, publicKeyArmored string) error {
	if key.ID == "" {
		key.ID = uuid.New().String()
	}

	_, err := s.db.Exec(`
		INSERT INTO pgp_keys (id, account_id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key, public_key_armored,
			is_default, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			account_id = excluded.account_id,
			is_default = excluded.is_default`,
		key.ID, key.AccountID, key.Email, key.KeyID, key.Fingerprint, key.UserID,
		key.Algorithm, key.KeySize, key.CreatedAtKey, key.ExpiresAtKey,
		publicKeyArmored, key.IsDefault, time.Now(),
	)
	return err
}

// GetKey retrieves a key by ID
func (s *Store) GetKey(id string) (*Key, string, error) {
	key := &Key{}
	var publicKeyArmored string
	var createdAtKey, expiresAtKey sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, account_id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key, public_key_armored,
			is_default, created_at
		FROM pgp_keys WHERE id = ?`, id,
	).Scan(
		&key.ID, &key.AccountID, &key.Email, &key.KeyID, &key.Fingerprint, &key.UserID,
		&key.Algorithm, &key.KeySize, &createdAtKey, &expiresAtKey,
		&publicKeyArmored, &key.IsDefault, &key.CreatedAt,
	)
	if err != nil {
		return nil, "", err
	}

	if createdAtKey.Valid {
		key.CreatedAtKey = &createdAtKey.Time
	}
	if expiresAtKey.Valid {
		key.ExpiresAtKey = &expiresAtKey.Time
		key.IsExpired = time.Now().After(expiresAtKey.Time)
	}

	return key, publicKeyArmored, nil
}

// ListKeys returns all PGP keys for an account
func (s *Store) ListKeys(accountID string) ([]*Key, error) {
	rows, err := s.db.Query(`
		SELECT id, account_id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key,
			is_default, created_at
		FROM pgp_keys WHERE account_id = ?
		ORDER BY is_default DESC, created_at DESC`, accountID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*Key
	for rows.Next() {
		key := &Key{}
		var createdAtKey, expiresAtKey sql.NullTime

		if err := rows.Scan(
			&key.ID, &key.AccountID, &key.Email, &key.KeyID, &key.Fingerprint, &key.UserID,
			&key.Algorithm, &key.KeySize, &createdAtKey, &expiresAtKey,
			&key.IsDefault, &key.CreatedAt,
		); err != nil {
			return nil, err
		}

		if createdAtKey.Valid {
			key.CreatedAtKey = &createdAtKey.Time
		}
		if expiresAtKey.Valid {
			key.ExpiresAtKey = &expiresAtKey.Time
			key.IsExpired = time.Now().After(expiresAtKey.Time)
		}

		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// DeleteKey removes a PGP key by ID
func (s *Store) DeleteKey(id string) error {
	_, err := s.db.Exec("DELETE FROM pgp_keys WHERE id = ?", id)
	return err
}

// SetDefaultKey sets a key as the default for its account
func (s *Store) SetDefaultKey(accountID, keyID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Validate ownership inside the transaction before changing either default.
	var owner string
	if err := tx.QueryRow("SELECT account_id FROM pgp_keys WHERE id = ?", keyID).Scan(&owner); err != nil {
		return fmt.Errorf("find default credential: %w", err)
	}
	if owner != accountID {
		return fmt.Errorf("default credential belongs to another account")
	}

	// Clear existing defaults for this account
	if _, err := tx.Exec(
		"UPDATE pgp_keys SET is_default = 0 WHERE account_id = ?", accountID,
	); err != nil {
		return err
	}

	// Set the new default
	if _, err := tx.Exec(
		"UPDATE pgp_keys SET is_default = 1 WHERE id = ? AND account_id = ?", keyID, accountID,
	); err != nil {
		return err
	}

	// Update account's default key ID
	if _, err := tx.Exec(
		"UPDATE accounts SET pgp_default_key_id = ? WHERE id = ?", keyID, accountID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// GetKeyByEmail returns the key matching a specific email for an account.
// Returns nil (not an error) if no matching key exists.
func (s *Store) GetKeyByEmail(accountID, email string) (*Key, string, error) {
	key := &Key{}
	var publicKeyArmored string
	var createdAtKey, expiresAtKey sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, account_id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key, public_key_armored,
			is_default, created_at
		FROM pgp_keys
		WHERE account_id = ? AND LOWER(email) = LOWER(?)`, accountID, email,
	).Scan(
		&key.ID, &key.AccountID, &key.Email, &key.KeyID, &key.Fingerprint, &key.UserID,
		&key.Algorithm, &key.KeySize, &createdAtKey, &expiresAtKey,
		&publicKeyArmored, &key.IsDefault, &key.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}

	if createdAtKey.Valid {
		key.CreatedAtKey = &createdAtKey.Time
	}
	if expiresAtKey.Valid {
		key.ExpiresAtKey = &expiresAtKey.Time
		key.IsExpired = time.Now().After(expiresAtKey.Time)
	}

	return key, publicKeyArmored, nil
}

// GetDefaultKey returns the default signing key for an account
func (s *Store) GetDefaultKey(accountID string) (*Key, string, error) {
	key := &Key{}
	var publicKeyArmored string
	var createdAtKey, expiresAtKey sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, account_id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key, public_key_armored,
			is_default, created_at
		FROM pgp_keys
		WHERE account_id = ? AND is_default = 1`, accountID,
	).Scan(
		&key.ID, &key.AccountID, &key.Email, &key.KeyID, &key.Fingerprint, &key.UserID,
		&key.Algorithm, &key.KeySize, &createdAtKey, &expiresAtKey,
		&publicKeyArmored, &key.IsDefault, &key.CreatedAt,
	)
	if err != nil {
		return nil, "", err
	}

	if createdAtKey.Valid {
		key.CreatedAtKey = &createdAtKey.Time
	}
	if expiresAtKey.Valid {
		key.ExpiresAtKey = &expiresAtKey.Time
		key.IsExpired = time.Now().After(expiresAtKey.Time)
	}

	return key, publicKeyArmored, nil
}

// CacheSenderKey stores or updates a sender's public key from a signed message
func (s *Store) CacheSenderKey(email, armoredPublicKey, source string) error {
	entities, err := ParseArmoredKey(armoredPublicKey)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	entity := entities[0]
	meta := ExtractKeyMetadata(entity)

	id := uuid.New().String()
	now := time.Now()

	_, err = s.db.Exec(`
		INSERT INTO pgp_sender_keys (id, email, key_id, fingerprint, user_id,
			algorithm, key_size, created_at_key, expires_at_key, public_key_armored,
			source, collected_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			last_seen_at = excluded.last_seen_at`,
		id, email, meta.KeyID, meta.Fingerprint, meta.UserID,
		meta.Algorithm, meta.KeySize, meta.CreatedAtKey, meta.ExpiresAtKey,
		armoredPublicKey, source, now, now,
	)
	return err
}

// GetSenderKeys returns cached public keys for an email address
func (s *Store) GetSenderKeys(email string) ([]*SenderKey, error) {
	rows, err := s.db.Query(`
		SELECT id, email, key_id, fingerprint, user_id, algorithm, key_size,
			created_at_key, expires_at_key, source, collected_at, last_seen_at
		FROM pgp_sender_keys WHERE email = ?
		ORDER BY last_seen_at DESC`, email,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanSenderKeys(rows)
}

// ListAllSenderKeys returns all cached sender keys
func (s *Store) ListAllSenderKeys() ([]*SenderKey, error) {
	rows, err := s.db.Query(`
		SELECT id, email, key_id, fingerprint, user_id, algorithm, key_size,
			created_at_key, expires_at_key, source, collected_at, last_seen_at
		FROM pgp_sender_keys
		ORDER BY last_seen_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanSenderKeys(rows)
}

func (s *Store) scanSenderKeys(rows *sql.Rows) ([]*SenderKey, error) {
	var keys []*SenderKey
	for rows.Next() {
		sk := &SenderKey{}
		var createdAtKey, expiresAtKey sql.NullTime

		if err := rows.Scan(
			&sk.ID, &sk.Email, &sk.KeyID, &sk.Fingerprint, &sk.UserID,
			&sk.Algorithm, &sk.KeySize, &createdAtKey, &expiresAtKey,
			&sk.Source, &sk.CollectedAt, &sk.LastSeenAt,
		); err != nil {
			return nil, err
		}

		if createdAtKey.Valid {
			sk.CreatedAtKey = &createdAtKey.Time
		}
		if expiresAtKey.Valid {
			sk.ExpiresAtKey = &expiresAtKey.Time
		}

		keys = append(keys, sk)
	}
	return keys, rows.Err()
}

// DeleteSenderKey removes a cached sender key
func (s *Store) DeleteSenderKey(id string) error {
	_, err := s.db.Exec("DELETE FROM pgp_sender_keys WHERE id = ?", id)
	return err
}

// GetSenderKeyArmored returns the armored public key for a sender key
func (s *Store) GetSenderKeyArmored(id string) (string, error) {
	var armored string
	err := s.db.QueryRow("SELECT public_key_armored FROM pgp_sender_keys WHERE id = ?", id).Scan(&armored)
	return armored, err
}

// GetSenderKeyArmoreds returns armored public keys for multiple email addresses (batch lookup for encryption).
// Returns a map of email -> armoredPublicKey for emails that have a valid (non-expired) key.
func (s *Store) GetSenderKeyArmoreds(emails []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(emails) == 0 {
		return result, nil
	}

	now := time.Now()
	for _, email := range emails {
		var armored string
		var expiresAtKey sql.NullTime
		err := s.db.QueryRow(`
			SELECT public_key_armored, expires_at_key FROM pgp_sender_keys
			WHERE email = ? ORDER BY last_seen_at DESC LIMIT 1`, email,
		).Scan(&armored, &expiresAtKey)
		if err != nil {
			continue
		}
		if expiresAtKey.Valid && now.After(expiresAtKey.Time) {
			continue // Skip expired keys
		}
		result[email] = armored
	}

	return result, nil
}

// ImportSenderKeyFromFile imports a recipient's public key from file content
func (s *Store) ImportSenderKeyFromFile(email string, keyData []byte) error {
	armoredPublicKey, _, err := ImportPublicKey(keyData)
	if err != nil {
		return fmt.Errorf("failed to import key: %w", err)
	}

	return s.CacheSenderKey(email, armoredPublicKey, "manual")
}

// ListKeyServers returns all configured key servers ordered by order_index
func (s *Store) ListKeyServers() ([]KeyServer, error) {
	rows, err := s.db.Query(`
		SELECT id, url, order_index FROM pgp_keyservers
		ORDER BY order_index, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []KeyServer
	for rows.Next() {
		var ks KeyServer
		if err := rows.Scan(&ks.ID, &ks.URL, &ks.OrderIndex); err != nil {
			return nil, err
		}
		servers = append(servers, ks)
	}
	return servers, rows.Err()
}

// AddKeyServer inserts a new key server with the next order_index
func (s *Store) AddKeyServer(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("key server URL must start with https://")
	}

	// Get the next order_index
	var maxIdx int
	err := s.db.QueryRow("SELECT COALESCE(MAX(order_index), -1) FROM pgp_keyservers").Scan(&maxIdx)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		"INSERT INTO pgp_keyservers (url, order_index) VALUES (?, ?)",
		url, maxIdx+1,
	)
	return err
}

// RemoveKeyServer deletes a key server by ID
func (s *Store) RemoveKeyServer(id int) error {
	_, err := s.db.Exec("DELETE FROM pgp_keyservers WHERE id = ?", id)
	return err
}

// GetSignPolicy returns the PGP signing policy for an account
func (s *Store) GetSignPolicy(accountID string) (string, error) {
	var policy string
	err := s.db.QueryRow(
		"SELECT pgp_sign_policy FROM accounts WHERE id = ?", accountID,
	).Scan(&policy)
	return policy, err
}

// SetSignPolicy updates the PGP signing policy for an account
func (s *Store) SetSignPolicy(accountID, policy string) error {
	_, err := s.db.Exec(
		"UPDATE accounts SET pgp_sign_policy = ? WHERE id = ?", policy, accountID,
	)
	return err
}

// GetEncryptPolicy returns the PGP encryption policy for an account
func (s *Store) GetEncryptPolicy(accountID string) (string, error) {
	var policy string
	err := s.db.QueryRow(
		"SELECT pgp_encrypt_policy FROM accounts WHERE id = ?", accountID,
	).Scan(&policy)
	return policy, err
}

// SetEncryptPolicy updates the PGP encryption policy for an account
func (s *Store) SetEncryptPolicy(accountID, policy string) error {
	_, err := s.db.Exec(
		"UPDATE accounts SET pgp_encrypt_policy = ? WHERE id = ?", policy, accountID,
	)
	return err
}
