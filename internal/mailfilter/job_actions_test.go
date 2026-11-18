package mailfilter

import (
	"github.com/hkdb/aerion/internal/message"
	"testing"
	"time"
)

func TestExplicitJobsAreAtomicAndSnapshotActions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	rule := &Rule{ID: "r", Name: "Read list", AccountIDs: []string{"account1", "account2"}, ListIDContains: []string{"news"}, ActionMarkRead: true, ActionTrash: true}
	if err := NewStore(db).Create(rule); err != nil {
		t.Fatal(err)
	}
	restored, err := NewStore(db).Get(rule.ID)
	if err != nil || len(restored.AccountIDs) != 2 || len(restored.ListIDContains) != 1 || !restored.ActionTrash || !restored.ActionMarkRead {
		t.Fatalf("actions/scope lost: %+v %v", restored, err)
	}
	good := &message.Message{ID: "m", AccountID: "account1", FolderID: "folder1", UID: 1, Date: time.Now()}
	bad := &message.Message{ID: "unstable", AccountID: "account1", FolderID: "folder1"}
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{good, bad}, true); err == nil {
		t.Fatal("unstable existing selection accepted")
	}
	if job, err := jobs.ClaimNext(); job != nil || err != nil {
		t.Fatalf("rejected batch queued partial mail: %+v %v", job, err)
	}
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{good}, true); err != nil {
		t.Fatal(err)
	}
	// A duplicate HTTP submission cannot duplicate pending provider work.
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{good}, true); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext()
	if err != nil || job == nil || !job.Actions.ApplyExisting || !job.Actions.MarkRead || !job.Actions.Trash || job.DestinationFolderID != "" {
		t.Fatalf("action-only snapshot lost: %+v %v", job, err)
	}
	if next, err := jobs.ClaimNext(); next != nil || err != nil {
		t.Fatalf("duplicate work: %+v %v", next, err)
	}
}

func TestPausedJobKeepsItsSnapshotWithoutBusyReclaim(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	rule := &Rule{ID: "paused", ActionMarkRead: true, DestFolderID: "folder2"}
	m := &message.Message{ID: "source", AccountID: "account1", FolderID: "folder1", UID: 10}
	if err := jobs.Enqueue(rule, rule.DestFolderID, m); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext()
	if err != nil || job == nil {
		t.Fatalf("claim: %+v %v", job, err)
	}
	if err := jobs.DeferUnstarted(job.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := jobs.ClaimNext(); next != nil || err != nil {
		t.Fatalf("paused job reclaimed in hot loop: %+v %v", next, err)
	}
	if unknown, err := jobs.ListOutcomeUnknown(0, 10); err != nil || len(unknown) != 0 {
		t.Fatalf("unattempted pause reported uncertain: %v %v", unknown, err)
	}
	if _, err := db.Exec(`UPDATE automatic_filter_jobs SET updated_at=datetime('now','-1 second') WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := jobs.ClaimNext()
	if err != nil || resumed == nil || resumed.ID != job.ID || resumed.SourceUID != 10 || !resumed.Actions.MarkRead {
		t.Fatalf("resume lost source/actions: %+v %v", resumed, err)
	}
}

func TestExplicitSubmissionUpgradesPendingAutomaticJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	rule := &Rule{ID: "rule", ActionMarkRead: true}
	m := &message.Message{ID: "m", AccountID: "account1", FolderID: "folder1", UID: 10}
	if err := jobs.Enqueue(rule, "", m); err != nil {
		t.Fatal(err)
	}
	rule.ActionTrash = true
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{m}, true); err != nil {
		t.Fatal(err)
	}
	// A late automatic callback must never downgrade a user's explicit choice.
	if err := jobs.Enqueue(rule, "", m); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext()
	if err != nil || job == nil || !job.Actions.ApplyExisting || !job.Actions.Trash {
		t.Fatalf("explicit application lost to pending job: %+v %v", job, err)
	}
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{m}, true); err == nil {
		t.Fatal("reported a second explicit submission while provider work was in progress")
	}
}

func TestUncertainConflictRejectsEntireExplicitBatch(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	jobs := NewJobStore(db)
	rule := &Rule{ID: "rule", ActionMarkRead: true}
	m := &message.Message{ID: "uncertain", AccountID: "account1", FolderID: "folder1", UID: 10}
	if err := jobs.Enqueue(rule, "", m); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.ClaimNext()
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err := jobs.MarkOutcomeUnknown(job.ID, "connection lost"); err != nil {
		t.Fatal(err)
	}
	other := &message.Message{ID: "other", AccountID: "account1", FolderID: "folder1", UID: 11}
	if err := jobs.EnqueueBatch(rule, "", []*message.Message{other, m}, true); err == nil {
		t.Fatal("uncertain conflict reported success")
	}
	if next, err := jobs.ClaimNext(); next != nil || err != nil {
		t.Fatalf("rejected batch queued partial work: %+v %v", next, err)
	}
}
