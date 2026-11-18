package v1

import (
	"github.com/hkdb/aerion/internal/mailfilter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type filterEvaluationStub struct {
	applied int
	id      string
	preview *mailfilter.Rule
}

func (*filterEvaluationStub) GetFilterRules() ([]*mailfilter.Rule, error) { return nil, nil }
func (*filterEvaluationStub) CreateFilterRule(*mailfilter.Rule) error     { return nil }
func (*filterEvaluationStub) UpdateFilterRule(*mailfilter.Rule) error     { return nil }
func (*filterEvaluationStub) DeleteFilterRule(string) error               { return nil }
func (*filterEvaluationStub) ReorderFilterRules([]string) error           { return nil }
func (s *filterEvaluationStub) PreviewFilterRule(r *mailfilter.Rule) (int, error) {
	s.preview = r
	return 42, nil
}
func (s *filterEvaluationStub) ApplyFilterRuleToExisting(id string) (int, error) {
	s.applied++
	s.id = id
	return 9, nil
}
func (s *filterEvaluationStub) GetMessageListID(id string) (string, error) {
	s.id = id
	return "news.example.com", nil
}

func TestFilterEvaluationPermissionsAndSideEffects(t *testing.T) {
	ops := &filterEvaluationStub{}
	writeAllowed := false
	mux := http.NewServeMux()
	MountFilterRuleRoutes(mux, ops, func(w http.ResponseWriter, r *http.Request, c Capability) bool {
		if c == CapabilityMailWrite && !writeAllowed {
			w.WriteHeader(http.StatusForbidden)
			return false
		}
		return true
	})
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/filters/evaluate", strings.NewReader(body)))
		return w
	}
	w := call(`{"operation":"preview","rule":{"accountIds":["a","b"],"listIdContains":["news"],"actionTrash":true}}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"data":42`) || ops.applied != 0 || ops.preview == nil || len(ops.preview.AccountIDs) != 2 {
		t.Fatalf("preview changed mail or lost scope: %d %s %+v", w.Code, w.Body.String(), ops)
	}
	w = call(`{"operation":"apply","id":"new-rule"}`)
	if w.Code != 403 || ops.applied != 0 {
		t.Fatal("read-only client applied a filter")
	}
	writeAllowed = true
	w = call(`{"operation":"apply","id":"new-rule"}`)
	if w.Code != 200 || ops.applied != 1 || ops.id != "new-rule" {
		t.Fatalf("explicit application lost the saved ID: %d %+v", w.Code, ops)
	}
	w = call(`{"operation":"list-id","id":"message-id"}`)
	if w.Code != 200 || ops.id != "message-id" || !strings.Contains(w.Body.String(), "news.example.com") {
		t.Fatal("header lookup failed")
	}
	w = call(`{"operation":"unknown"}`)
	if w.Code != 400 || ops.applied != 1 {
		t.Fatal("unknown operation applied mail")
	}
	w = call(strings.Repeat("x", (1<<20)+1))
	if w.Code != 400 {
		t.Fatal("oversize request accepted")
	}
}
