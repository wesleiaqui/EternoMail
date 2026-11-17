package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type visibilityOp struct {
	called          bool
	account         string
	ids             []string
	hidden, refresh bool
}

func (o *visibilityOp) SetFolderVisibility(_ context.Context, a string, ids []string, h, r bool) error {
	o.called = true
	o.account = a
	o.ids = ids
	o.hidden = h
	o.refresh = r
	return nil
}
func TestFolderVisibilityRouteValidationAndCapability(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allow      bool
		want       int
	}{
		{"deny", `{"accountId":"a","folderIds":["f"],"hidden":true}`, false, 403},
		{"missing-hidden", `{"accountId":"a","folderIds":["f"]}`, true, 400},
		{"empty-ids", `{"accountId":"a","folderIds":[],"hidden":true}`, true, 400},
		{"refresh-hidden", `{"accountId":"a","folderIds":["f"],"hidden":true,"refresh":true}`, true, 400},
		{"hide", `{"accountId":"a","folderIds":["f"],"hidden":true}`, true, 200},
		{"show-refresh", `{"accountId":"a","folderIds":["f"],"hidden":false,"refresh":true}`, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := &visibilityOp{}
			mux := http.NewServeMux()
			MountFolderVisibilityRoutes(mux, op, func(w http.ResponseWriter, r *http.Request, c Capability) bool {
				if c != CapabilityMailWrite {
					t.Fatal(c)
				}
				if !tc.allow {
					http.Error(w, "forbidden", 403)
				}
				return tc.allow
			})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/folders/visibility", strings.NewReader(tc.body)))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if op.called != (tc.want == 200) {
				t.Fatalf("called=%v", op.called)
			}
			if op.called && (op.account != "a" || len(op.ids) != 1 || op.ids[0] != "f") {
				t.Fatalf("bad arguments %#v", op)
			}
		})
	}
}
