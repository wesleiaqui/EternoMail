package sync

import (
	"errors"
	"testing"
)

func TestRepairFetchRequiresExactUIDSizeAndSuccess(t *testing.T) {
	if err := validateRepairFetch(1, 1, 10, 10, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		uid   uint32
		size  int64
		bytes int
		err   error
	}{{2, 10, 10, nil}, {1, 0, 10, nil}, {1, 10, 9, nil}, {1, 9, 10, nil}, {1, maxMessageSize + 1, int(maxMessageSize + 1), nil}, {1, 10, 10, errors.New("FETCH failed")}} {
		if err := validateRepairFetch(1, tc.uid, tc.size, tc.bytes, tc.err); err == nil {
			t.Fatalf("unsafe fetch accepted: %+v", tc)
		}
	}
}
