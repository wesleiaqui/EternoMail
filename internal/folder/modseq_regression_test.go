package folder

import "testing"

func TestNonPositiveModSeqIsAbsent(t *testing.T) {
	db := openTestDB(t)
	createTestAccount(t, db, "account")
	s := NewStore(db)
	f := &Folder{AccountID: "account", Name: "Inbox", Path: "INBOX", Type: TypeInbox, Subscribed: true}
	if err := s.Create(f); err != nil {
		t.Fatal(err)
	}
	for _, value := range []int64{-1, 0, 17} {
		if _, err := db.Exec("UPDATE folders SET highest_mod_seq = ?, flags_sync_modseq = ? WHERE id = ?", value, value, f.ID); err != nil {
			t.Fatal(err)
		}
		want := uint64(0)
		if value > 0 {
			want = uint64(value)
		}
		got, err := s.Get(f.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.HighestModSeq != want || got.FlagsSyncModSeq != want {
			t.Fatalf("invalid baseline: %+v", got)
		}
		list, err := s.List("account")
		if err != nil {
			t.Fatal(err)
		}
		if list[0].HighestModSeq != want || list[0].FlagsSyncModSeq != want {
			t.Fatal("list accepted invalid baseline")
		}
	}
}
