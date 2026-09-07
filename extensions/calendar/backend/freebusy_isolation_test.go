package backend

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestFreeBusyUsesCurrentStoreAndExactInterval(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()
	start := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC).Unix()
	err := store.WithTx(func(tx *sql.Tx) error {
		if err := store.CreateSourceTx(tx, Source{ID: "s", Type: SourceTypeLocal, Name: "local", Enabled: true}); err != nil {
			return err
		}
		if err := store.CreateCalendarTx(tx, Calendar{ID: "c", SourceID: "s", URL: "local", DisplayName: "calendar", Visible: true}); err != nil {
			return err
		}
		return store.UpsertEventTx(tx, Event{ID: "e", CalendarID: "c", UID: "one", Summary: "private event", DTStartUnix: start, DTEndUnix: start + 3600, ICSBlob: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:one\r\nDTSTART:20260907T100000Z\r\nDTEND:20260907T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"})
	})
	if err != nil {
		t.Fatal(err)
	}
	api := NewAPI(store, nil, nil, nil)
	emails := []string{"user@example.test"}
	first, err := api.QueryAggregatedFreeBusy(context.Background(), emails, emails, start, start+3600)
	if err != nil || len(first) != 1 || len(first[0].Blocks) != 1 {
		t.Fatal("missing busy interval", first, err)
	}
	later, err := api.QueryAggregatedFreeBusy(context.Background(), emails, emails, start+7200, start+10800)
	if err != nil || len(later) != 1 || len(later[0].Blocks) != 0 {
		t.Fatal("reused another interval", later, err)
	}
	other := newTestStore(t)
	defer other.Close()
	second, err := NewAPI(other, nil, nil, nil).QueryAggregatedFreeBusy(context.Background(), emails, emails, start, start+3600)
	if err != nil || len(second) != 1 || len(second[0].Blocks) != 0 {
		t.Fatal("leaked another store's availability", second, err)
	}
	if err := store.WithTx(func(tx *sql.Tx) error { _, err := tx.Exec("UPDATE calendar_sources SET enabled=0"); return err }); err != nil {
		t.Fatal(err)
	}
	disabled, err := api.QueryAggregatedFreeBusy(context.Background(), emails, emails, start, start+3600)
	if err != nil || len(disabled) != 1 || len(disabled[0].Blocks) != 0 {
		t.Fatal("disabled source still visible", disabled, err)
	}
}
