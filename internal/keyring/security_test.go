package keyring

import (
	"errors"
	"strings"
	"testing"
)

func TestKeyringUnavailableRedactsBackendDetails(t *testing.T) {
	err := keyringFailure("failed to store password", errors.New("D-Bus internal-secret-payload"))
	if strings.Contains(err.Error(), "internal-secret") || !strings.Contains(err.Error(), "desktop session") {
		t.Fatal(err)
	}
	if serviceName != "aerion" {
		t.Fatal("generic service")
	}
}
