package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hkdb/aerion/internal/account"
	"github.com/hkdb/aerion/internal/credentials"
	"github.com/hkdb/aerion/internal/database"
	gokeyring "github.com/zalando/go-keyring"
)

func accountUpdateFixture(t *testing.T, keyringFails bool) (*App, *database.DB, string, account.AccountConfig) {
	t.Helper()
	if keyringFails {
		gokeyring.MockInitWithError(errors.New("keyring unavailable"))
	} else {
		gokeyring.MockInit()
	}

	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "account-update.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.NewStore(db.DB, dir)
	if err != nil {
		t.Fatal(err)
	}
	store := account.NewStore(db)
	config := account.AccountConfig{
		Name:              "Old account",
		DisplayName:       "Old sender",
		Email:             "account@example.test",
		IMAPHost:          "imap.old.test",
		IMAPPort:          993,
		IMAPSecurity:      account.SecurityTLS,
		IMAPAuthMechanism: account.AuthMechAuto,
		SMTPHost:          "smtp.old.test",
		SMTPPort:          587,
		SMTPSecurity:      account.SecurityStartTLS,
		SMTPAuthMechanism: account.AuthMechAuto,
		SMTPUsername:      "smtp-user",
		AuthType:          account.AuthPassword,
		Username:          "imap-user",
		Color:             "#123456",
		SyncPeriodDays:    30,
		SyncInterval:      30,
	}
	created, err := store.Create(&config)
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.SetPassword(created.ID, "old-imap-password"); err != nil {
		t.Fatal(err)
	}
	if err := creds.SetSMTPPassword(created.ID, "old-smtp-password"); err != nil {
		t.Fatal(err)
	}
	return &App{accountStore: store, credStore: creds}, db, created.ID, config
}

func TestUpdateAccountUpdatesConfigurationAndCredentials(t *testing.T) {
	a, _, id, config := accountUpdateFixture(t, false)
	config.Name = "New account"
	config.IMAPHost = "imap.new.test"
	config.SMTPHost = "smtp.new.test"
	config.Username = "new-imap-user"
	config.SMTPUsername = "new-smtp-user"
	config.Password = "new-imap-password"
	config.SMTPPassword = "new-smtp-password"

	updated, err := a.UpdateAccount(id, config)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != config.Name || updated.IMAPHost != config.IMAPHost || updated.SMTPHost != config.SMTPHost || updated.Username != config.Username || updated.SMTPUsername != config.SMTPUsername {
		t.Fatalf("updated account does not contain new configuration: %+v", updated)
	}
	assertAccountCredentials(t, a, id, "new-imap-password", "new-smtp-password")
}

func TestUpdateAccountKeepsCredentialsWhenPasswordsAreBlank(t *testing.T) {
	a, _, id, config := accountUpdateFixture(t, false)
	config.Name = "Renamed account"

	if _, err := a.UpdateAccount(id, config); err != nil {
		t.Fatal(err)
	}
	updated, err := a.accountStore.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed account" {
		t.Fatalf("name = %q, want renamed account", updated.Name)
	}
	assertAccountCredentials(t, a, id, "old-imap-password", "old-smtp-password")
}

func TestUpdateAccountPasswordFailureLeavesConfigurationUnchanged(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, true)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_imap_password
		BEFORE UPDATE OF encrypted_password ON accounts
		BEGIN SELECT RAISE(ABORT, 'injected IMAP password failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	config.IMAPHost = "imap.new.test"
	config.Password = "new-imap-password"

	if _, err := a.UpdateAccount(id, config); err == nil {
		t.Fatal("UpdateAccount succeeded despite password failure")
	}
	assertAccountConfiguration(t, a, id, "Old account", "imap.old.test")
	assertAccountCredentials(t, a, id, "old-imap-password", "old-smtp-password")
}

func TestUpdateAccountRestoresIMAPWhenSMTPUpdateFails(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, true)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_smtp_password
		BEFORE UPDATE OF encrypted_smtp_password ON accounts
		BEGIN SELECT RAISE(ABORT, 'injected SMTP password failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	config.IMAPHost = "imap.new.test"
	config.Password = "new-imap-password"
	config.SMTPPassword = "new-smtp-password"

	if _, err := a.UpdateAccount(id, config); err == nil {
		t.Fatal("UpdateAccount succeeded despite SMTP password failure")
	}
	assertAccountConfiguration(t, a, id, "Old account", "imap.old.test")
	assertAccountCredentials(t, a, id, "old-imap-password", "old-smtp-password")
}

func TestUpdateAccountUsesFallbackWhenKeyringFails(t *testing.T) {
	a, _, id, config := accountUpdateFixture(t, true)
	config.IMAPHost = "imap.new.test"
	config.Password = "new-imap-password"
	config.SMTPPassword = "new-smtp-password"

	if _, err := a.UpdateAccount(id, config); err != nil {
		t.Fatal(err)
	}
	assertAccountConfiguration(t, a, id, "Old account", "imap.new.test")
	assertAccountCredentials(t, a, id, "new-imap-password", "new-smtp-password")
}

func TestUpdateAccountRestoresAbsentSMTPPasswordAfterConfigurationFailure(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, true)
	if _, err := db.Exec("UPDATE accounts SET encrypted_smtp_password = NULL, smtp_password_storage = '' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER fail_identity_update
		BEFORE UPDATE OF name ON identities
		BEGIN SELECT RAISE(ABORT, 'injected identity update failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	config.SMTPPassword = "new-smtp-password"
	if _, err := a.UpdateAccount(id, config); err == nil {
		t.Fatal("UpdateAccount succeeded despite configuration failure")
	}
	var storage string
	if err := db.QueryRow("SELECT smtp_password_storage FROM accounts WHERE id = ?", id).Scan(&storage); err != nil || storage != "" {
		t.Fatalf("SMTP legacy state = %q, %v", storage, err)
	}
	if _, err := a.credStore.GetSMTPPassword(id); !errors.Is(err, credentials.ErrCredentialNotFound) {
		t.Fatalf("SMTP password after rollback = %v", err)
	}
}

func TestUpdateAccountDoesNotRemoveSMTPPasswordWithoutCredentialChange(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, true)
	if _, err := db.Exec("UPDATE accounts SET smtp_username = '' WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	config.SMTPUsername = ""
	config.Name = "Renamed account"
	if _, err := a.UpdateAccount(id, config); err != nil {
		t.Fatal(err)
	}
	var storage string
	if err := db.QueryRow("SELECT smtp_password_storage FROM accounts WHERE id = ?", id).Scan(&storage); err != nil || storage != "fallback" {
		t.Fatalf("SMTP storage changed during config-only update: %q, %v", storage, err)
	}
	if got, err := a.credStore.GetSMTPPassword(id); err != nil || got != "old-smtp-password" {
		t.Fatalf("SMTP password changed during config-only update: %q, %v", got, err)
	}
}

func TestUpdateAccountRestoresCredentialsWhenConfigurationWriteFails(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, true)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_identity_update
		BEFORE UPDATE OF name ON identities
		BEGIN SELECT RAISE(ABORT, 'injected identity update failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	config.Name = "New account"
	config.IMAPHost = "imap.new.test"
	config.Password = "new-imap-password"
	config.SMTPUsername = ""

	if _, err := a.UpdateAccount(id, config); err == nil {
		t.Fatal("UpdateAccount succeeded despite configuration failure")
	}
	assertAccountConfiguration(t, a, id, "Old account", "imap.old.test")
	assertAccountCredentials(t, a, id, "old-imap-password", "old-smtp-password")
}

func TestUpdateAccountReportsCredentialCompensationFailureWithoutSecrets(t *testing.T) {
	a, db, id, config := accountUpdateFixture(t, false)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_password_marker
		BEFORE UPDATE OF password_storage ON accounts
		BEGIN SELECT RAISE(ABORT, 'injected password marker failure'); END
	`); err != nil {
		t.Fatal(err)
	}
	config.IMAPHost = "imap.new.test"
	config.Password = "new-imap-password"

	_, err := a.UpdateAccount(id, config)
	if err == nil {
		t.Fatal("UpdateAccount succeeded despite keyring marker failure")
	}
	if !strings.Contains(err.Error(), "failed to restore previous credentials") {
		t.Fatalf("error does not report compensation failure: %v", err)
	}
	if strings.Contains(err.Error(), "old-imap-password") || strings.Contains(err.Error(), "new-imap-password") {
		t.Fatalf("error exposed password: %v", err)
	}
	assertAccountConfiguration(t, a, id, "Old account", "imap.old.test")
	assertAccountCredentials(t, a, id, "old-imap-password", "old-smtp-password")
}

func assertAccountConfiguration(t *testing.T, a *App, id, wantName, wantIMAPHost string) {
	t.Helper()
	acc, err := a.accountStore.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Name != wantName || acc.IMAPHost != wantIMAPHost {
		t.Fatalf("configuration = name:%q imap:%q, want name:%q imap:%q", acc.Name, acc.IMAPHost, wantName, wantIMAPHost)
	}
}

func assertAccountCredentials(t *testing.T, a *App, id, wantIMAP, wantSMTP string) {
	t.Helper()
	imapPassword, err := a.credStore.GetPassword(id)
	if err != nil || imapPassword != wantIMAP {
		t.Fatalf("IMAP password changed unexpectedly: %v", err)
	}
	smtpPassword, err := a.credStore.GetSMTPPassword(id)
	if err != nil || smtpPassword != wantSMTP {
		t.Fatalf("SMTP password changed unexpectedly: %v", err)
	}
}
