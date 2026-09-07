package ipc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestMessageReaderLimitAndPipeline(t *testing.T) {
	r := &messageReader{reader: strings.NewReader(`{"type":"auth"}{"type":"ping"}`)}
	var m Message
	if err := r.Decode(&m); err != nil || m.Type != TypeAuth {
		t.Fatal(err, m.Type)
	}
	if err := r.Decode(&m); err != nil || m.Type != TypePing {
		t.Fatal(err, m.Type)
	}
	r = &messageReader{reader: strings.NewReader(`{"type":"` + strings.Repeat("x", maxIPCMessageSize) + `"}`)}
	if err := r.Decode(&m); err == nil {
		t.Fatal("oversized JSON accepted")
	}
}
func TestServerRejectsPreAuthAndPreservesPipelinedPing(t *testing.T) {
	for _, authenticate := range []bool{false, true} {
		tm, err := NewTokenManager()
		if err != nil {
			t.Fatal(err)
		}
		s := NewBaseServer(tm)
		s.ctx = context.Background()
		server, client := net.Pipe()
		client.SetDeadline(time.Now().Add(2 * time.Second))
		done := make(chan struct{})
		go func() { s.handleConnection(server); close(done) }()
		payload := `{"type":"ping"}`
		if authenticate {
			payload = `{"type":"auth","payload":{"token":"` + tm.GetToken() + `"}}` + payload
		}
		go func() { client.Write([]byte(payload)) }()
		dec := json.NewDecoder(client)
		var response Message
		if err := dec.Decode(&response); err != nil {
			t.Fatal(err)
		}
		var auth AuthResponsePayload
		if err := response.ParsePayload(&auth); err != nil {
			t.Fatal(err)
		}
		if auth.Success != authenticate {
			t.Fatal("incorrect auth outcome")
		}
		if authenticate {
			if err := dec.Decode(&response); err != nil || response.Type != TypePong {
				t.Fatal("pipelined ping lost", err)
			}
		}
		client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("connection goroutine leaked")
		}
	}
}
func TestEmptyTokenManagerRejectsEmptyToken(t *testing.T) {
	if new(TokenManager).Validate("") {
		t.Fatal("uninitialized manager authenticated")
	}
}
