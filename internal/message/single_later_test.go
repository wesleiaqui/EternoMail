package message

import (
	"github.com/hkdb/aerion/internal/database"
	"path/filepath"
	"testing"
)

func TestSingleLaterUnion(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "later.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO accounts(id,name,email,imap_host,smtp_host,username) VALUES('a','A','a@example.test','imap','smtp','a'),('b','B','b@example.test','imap','smtp','b')`,
		`INSERT INTO folders(id,account_id,name,path,folder_type) VALUES('fa','a','Inbox','INBOX','inbox'),('fb','b','Inbox','INBOX','inbox')`,
		`INSERT INTO messages(id,account_id,folder_id,uid,message_id,thread_id,subject,from_email,date,is_read) VALUES('m1','a','fa',1,'m1','both','both','x@example.test',1,1),('m2','a','fa',2,'m2','reply','reply','x@example.test',2,0),('m3','a','fa',3,'m3','read','read','x@example.test',3,1),('m4','b','fb',1,'m4','both','other account','x@example.test',4,1),('m5','a','fa',5,'m5','neither','neither','x@example.test',5,0)`,
		`INSERT INTO read_later_queue(account_id,conversation_id) VALUES('a','both'),('a','read')`,
		`INSERT INTO reply_later_queue(account_id,conversation_id) VALUES('a','both'),('a','reply')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db)
	for _, tc := range []struct {
		filter string
		want   int
	}{{"", 3}, {"unread", 1}, {"readLater", 3}, {"replyLater", 3}, {"readLater+unread", 1}} {
		t.Run(tc.filter, func(t *testing.T) {
			items, err := s.ListReadLaterConversations(0, 50, "newest", tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != tc.want {
				t.Fatalf("items=%d want %d", len(items), tc.want)
			}
			count, err := s.CountReadLaterConversations(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if count != tc.want {
				t.Fatalf("count=%d want %d", count, tc.want)
			}
			for _, item := range items {
				if item.AccountID != "a" || item.MessageCount != 1 {
					t.Fatalf("duplicate/account leak: %+v", item)
				}
			}
		})
	}
	items, err := s.ListConversationsByFolderIDs([]string{"fa", "fb"}, 0, 50, "newest", "readLater")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("folder union=%d", len(items))
	}
	if _, err = db.Exec(`UPDATE messages SET is_read=1`); err != nil {
		t.Fatal(err)
	}
	count, err := s.CountReadLaterConversations("")
	if err != nil || count != 3 {
		t.Fatalf("reading dropped membership: %d %v", count, err)
	}
}
