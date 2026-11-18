package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/hkdb/aerion/internal/mailfilter"
)

// FilterRuleMutationRequest describes a persisted rule operation.
type FilterRuleMutationRequest struct {
	Operation string           `json:"operation"`
	Rule      *mailfilter.Rule `json:"rule,omitempty"`
	ID        string           `json:"id,omitempty"`
	IDs       []string         `json:"ids,omitempty"`
}

// FilterRuleOperations owns persisted filter-rule configuration.
type FilterRuleOperations interface {
	GetFilterRules() ([]*mailfilter.Rule, error)
	CreateFilterRule(*mailfilter.Rule) error
	UpdateFilterRule(*mailfilter.Rule) error
	DeleteFilterRule(string) error
	ReorderFilterRules([]string) error
}

// MountFilterRuleRoutes mounts read and mutation routes for automatic filters.
func MountFilterRuleRoutes(mux *http.ServeMux, ops FilterRuleOperations, requireCap func(http.ResponseWriter, *http.Request, Capability) bool) {
	mux.HandleFunc("/v1/filters/evaluate", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodPost {
			writeFilterRuleError(w, r, 405, CodeInvalidRequest, "Method not allowed")
			return
		}
		var req FilterRuleMutationRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeFilterRuleError(w, r, 400, CodeInvalidRequest, "Malformed request")
			return
		}
		extended, ok := ops.(interface {
			PreviewFilterRule(*mailfilter.Rule) (int, error)
			ApplyFilterRuleToExisting(string) (int, error)
			GetMessageListID(string) (string, error)
		})
		if !ok {
			writeFilterRuleError(w, r, 501, CodeInternal, "Filter evaluation unavailable")
			return
		}
		var result any
		var err error
		switch req.Operation {
		case "preview":
			result, err = extended.PreviewFilterRule(req.Rule)
		case "apply":
			if !requireCap(w, r, CapabilityMailWrite) {
				return
			}
			result, err = extended.ApplyFilterRuleToExisting(req.ID)
		case "list-id":
			result, err = extended.GetMessageListID(req.ID)
		default:
			err = fmt.Errorf("unknown evaluation operation")
		}
		if err != nil {
			writeFilterRuleError(w, r, 400, CodeInvalidRequest, err.Error())
			return
		}
		writeFilterRuleResponse(w, r, result)
	})

	mux.HandleFunc("/v1/filters", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailRead) {
			return
		}
		if r.Method != http.MethodGet {
			writeFilterRuleError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed")
			return
		}
		rules, err := ops.GetFilterRules()
		if err != nil {
			writeFilterRuleError(w, r, http.StatusInternalServerError, CodeInternal, "Failed to list filter rules")
			return
		}
		writeFilterRuleResponse(w, r, rules)
	})
	mux.HandleFunc("/v1/filters/mutations", func(w http.ResponseWriter, r *http.Request) {
		if !requireCap(w, r, CapabilityMailWrite) {
			return
		}
		if r.Method != http.MethodPost {
			writeFilterRuleError(w, r, http.StatusMethodNotAllowed, CodeInvalidRequest, "Method not allowed")
			return
		}
		var req FilterRuleMutationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeFilterRuleError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Malformed filter rule request")
			return
		}
		var err error
		switch strings.TrimSpace(req.Operation) {
		case "create":
			if req.Rule == nil {
				err = fmt.Errorf("filter rule is required")
			} else {
				err = ops.CreateFilterRule(req.Rule)
			}
		case "update":
			if req.Rule == nil {
				err = fmt.Errorf("filter rule is required")
			} else {
				err = ops.UpdateFilterRule(req.Rule)
			}
		case "delete":
			err = ops.DeleteFilterRule(req.ID)
		case "reorder":
			err = ops.ReorderFilterRules(req.IDs)
		default:
			writeFilterRuleError(w, r, http.StatusBadRequest, CodeInvalidRequest, "Unknown filter rule operation")
			return
		}
		if err != nil {
			writeFilterRuleError(w, r, http.StatusBadRequest, CodeInvalidRequest, err.Error())
			return
		}
		writeFilterRuleResponse(w, r, struct{}{})
	})
}

func writeFilterRuleResponse(w http.ResponseWriter, r *http.Request, data any) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = "req-unknown"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(Response[any]{Data: data, RequestID: reqID})
}

func writeFilterRuleError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = "req-unknown"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: ErrorBody{Code: code, Message: message, RequestID: reqID}})
}
