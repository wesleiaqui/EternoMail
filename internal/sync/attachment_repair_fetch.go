package sync

import "fmt"

func validateRepairFetch(wantUID, gotUID uint32, reported int64, received int, commandError error) error {
	if commandError != nil {
		return fmt.Errorf("repair FETCH did not complete: %w", commandError)
	}
	if wantUID == 0 || gotUID != wantUID {
		return fmt.Errorf("repair FETCH UID mismatch")
	}
	if reported <= 0 || reported > maxMessageSize || int64(received) != reported {
		return fmt.Errorf("repair FETCH size mismatch or over limit: received %d, reported %d", received, reported)
	}
	return nil
}
