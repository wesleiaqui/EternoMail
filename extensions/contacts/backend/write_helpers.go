package backend

import (
	"fmt"
	"time"
)

// contactsWriteTimeout bounds interactive write requests for providers that
// support them. Google Contacts deliberately has no write path.
const contactsWriteTimeout = 45 * time.Second

// firstAddressbookForSource resolves the local addressbook created by the
// read-side synchronizer for an OAuth contact source.
func (a *API) firstAddressbookForSource(sourceID string) (string, error) {
	abs, err := a.carddavStore.ListAddressbooks(sourceID)
	if err != nil {
		return "", err
	}
	for _, ab := range abs {
		if ab != nil {
			return ab.ID, nil
		}
	}
	return "", fmt.Errorf("source %s has no addressbook; run a sync first", sourceID)
}
