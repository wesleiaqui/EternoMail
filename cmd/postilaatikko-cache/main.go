// Local, offline attachment repair rollback. Quit the mailbox-owning app first.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/message"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("postilaatikko-cache", flag.ContinueOnError)
	dbPath := flags.String("db", "", "exact mailbox aerion.db path (quit the app first)")
	account := flags.String("account", "", "owning account ID")
	msg := flags.String("message", "", "message ID")
	restore := flags.String("restore", "", "current backup ID to restore; omit to show it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" || *account == "" || *msg == "" {
		return fmt.Errorf("--db, --account and --message are required")
	}
	// Open must never create a new database on a typo.
	info, err := os.Stat(*dbPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("existing database file required")
	}
	db, err := database.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil {
		return err
	}
	if version < 79 {
		return fmt.Errorf("attachment repair schema not installed; open upgraded app first")
	}
	var owner string
	if err = db.QueryRow(`SELECT account_id FROM messages WHERE id=?`, *msg).Scan(&owner); err != nil {
		return err
	}
	if owner != *account {
		return fmt.Errorf("message account mismatch")
	}
	store := message.NewAttachmentStore(db)
	if *restore != "" {
		if err = store.RestoreRepair(*account, *msg, *restore); err != nil {
			return err
		}
	}
	backup, err := store.RepairBackupID(*msg)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct{ Message, Backup string }{*msg, backup})
}
