package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/hkdb/aerion/internal/automation/client"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/operations"
	"github.com/hkdb/aerion/internal/views"
)

// JSONRPCRequest represents an incoming JSON-RPC 2.0 message.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError defines standard JSON-RPC 2.0 errors.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool represents an MCP Tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines JSON Schema for MCP Tool inputs.
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef describes a property in JSON Schema.
type PropertyDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolContent represents returned tool content for an LLM.
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallToolResult represents the MCP call_tool return payload.
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Server serves Model Context Protocol tools over standard I/O.
type Server struct {
	client *client.Client
	reader io.Reader
	writer io.Writer
}

// NewServer initializes an MCP Server connected to the automation client.
func NewServer(c *client.Client, r io.Reader, w io.Writer) *Server {
	if r == nil {
		r = os.Stdin
	}
	if w == nil {
		w = os.Stdout
	}
	return &Server{
		client: c,
		reader: r,
		writer: w,
	}
}

// Run executes the stdio message loop until EOF or error.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.reader)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "Parse error", nil)
			continue
		}

		s.handleRequest(ctx, req)
	}

	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		res := map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "postilaatikko-mcp",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		}
		s.sendResult(req.ID, res)

	case "notifications/initialized":

	case "tools/list":
		s.sendResult(req.ID, map[string]any{
			"tools": s.getTools(),
		})

	case "tools/call":
		s.handleToolCall(ctx, req)

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method), nil)
	}
}

func (s *Server) getTools() []Tool {
	return []Tool{
		{
			Name:        "postilaatikko_list_accounts",
			Description: "List all configured email accounts in Postilaatikko.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "postilaatikko_list_folders",
			Description: "List folders for a given account or all accounts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"accountId": {Type: "string", Description: "Optional account ID to filter by"},
				},
			},
		},
		{
			Name:        "postilaatikko_search_mail",
			Description: "Search email messages and conversations using search query.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"query": {Type: "string", Description: "Search query expression (e.g. 'is:unread', 'subject:report')"},
					"limit": {Type: "integer", Description: "Max results to return (1-100)"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "postilaatikko_read_thread",
			Description: "Read an email thread, including all message metadata and participants.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"accountId": {Type: "string", Description: "The account ID owning the thread"},
					"threadId":  {Type: "string", Description: "The thread ID"},
				},
				Required: []string{"accountId", "threadId"},
			},
		},
		{
			Name:        "postilaatikko_fetch_body",
			Description: "Retrieve message body and extracted reply content. Set fetch=true to fetch missing body from server.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"accountId": {Type: "string", Description: "Account ID"},
					"messageId": {Type: "string", Description: "Message ID"},
					"fetch":     {Type: "boolean", Description: "Whether to explicitly contact the mail server if uncached"},
				},
				Required: []string{"accountId", "messageId"},
			},
		},
		{
			Name:        "postilaatikko_mutate_mail",
			Description: "Perform a mailbox mutation: archive, mark_read, mark_unread, star, unstar, trash, or move.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"action":      {Type: "string", Description: "archive | mark_read | mark_unread | star | unstar | trash | move"},
					"accountId":   {Type: "string", Description: "Account ID of target message"},
					"messageId":   {Type: "string", Description: "Message ID of target message"},
					"destination": {Type: "string", Description: "Destination folder ID (only for 'move' action)"},
				},
				Required: []string{"action", "accountId", "messageId"},
			},
		},
	}
}

func (s *Server) handleToolCall(ctx context.Context, req JSONRPCRequest) {
	var callParams struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &callParams); err != nil {
		s.sendError(req.ID, -32602, "Invalid params", nil)
		return
	}

	switch callParams.Name {
	case "postilaatikko_list_accounts":
		accounts, err := s.client.GetAccounts(ctx)
		s.sendToolResult(req.ID, accounts, err)

	case "postilaatikko_list_folders":
		var args struct {
			AccountID string `json:"accountId"`
		}
		_ = json.Unmarshal(callParams.Arguments, &args)
		folders, err := s.client.GetFolders(ctx, args.AccountID)
		s.sendToolResult(req.ID, folders, err)

	case "postilaatikko_search_mail":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
			s.sendToolError(req.ID, "Invalid arguments: "+err.Error())
			return
		}
		res, err := s.client.Search(ctx, v1.SearchRequest{
			Scope: views.Scope{
				Kind:  views.ScopeSearch,
				Query: args.Query,
			},
			Page: v1.PageRequest{
				Limit: args.Limit,
			},
		})
		s.sendToolResult(req.ID, res, err)

	case "postilaatikko_read_thread":
		var args struct {
			AccountID string `json:"accountId"`
			ThreadID  string `json:"threadId"`
		}
		if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
			s.sendToolError(req.ID, "Invalid arguments: "+err.Error())
			return
		}
		res, err := s.client.ReadThread(ctx, v1.ThreadReadRequest{
			Ref: views.ThreadRef{
				AccountID: args.AccountID,
				ThreadID:  args.ThreadID,
			},
		})
		s.sendToolResult(req.ID, res, err)

	case "postilaatikko_fetch_body":
		var args struct {
			AccountID string `json:"accountId"`
			MessageID string `json:"messageId"`
			Fetch     bool   `json:"fetch"`
		}
		if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
			s.sendToolError(req.ID, "Invalid arguments: "+err.Error())
			return
		}
		mode := v1.FetchCachedOnly
		if args.Fetch {
			mode = v1.FetchExplicit
		}
		res, err := s.client.ReadBody(ctx, v1.BodyReadRequest{
			Ref: views.MessageRef{
				AccountID: args.AccountID,
				MessageID: args.MessageID,
			},
			Mode:            mode,
			IncludeOriginal: true,
		})
		s.sendToolResult(req.ID, res, err)

	case "postilaatikko_mutate_mail":
		var args struct {
			Action      string `json:"action"`
			AccountID   string `json:"accountId"`
			MessageID   string `json:"messageId"`
			Destination string `json:"destination"`
		}
		if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
			s.sendToolError(req.ID, "Invalid arguments: "+err.Error())
			return
		}
		idempotencyKey := fmt.Sprintf("mcp-%s-%s", args.Action, args.MessageID)
		res, err := s.client.MutateAction(ctx, v1.ActionRequest{
			Action: v1.ActionKind(args.Action),
			Target: operations.Target{
				Kind: operations.TargetMessage,
				Messages: []operations.MessageRef{
					{AccountID: args.AccountID, MessageID: args.MessageID},
				},
			},
			Destination: args.Destination,
		}, idempotencyKey)
		s.sendToolResult(req.ID, res, err)

	default:
		s.sendToolError(req.ID, fmt.Sprintf("Unknown tool %q", callParams.Name))
	}
}

func (s *Server) sendToolResult(id any, data any, err error) {
	if err != nil {
		s.sendToolError(id, err.Error())
		return
	}
	jsonData, _ := json.Marshal(data)
	s.sendResult(id, CallToolResult{
		Content: []ToolContent{
			{
				Type: "text",
				Text: string(jsonData),
			},
		},
	})
}

func (s *Server) sendToolError(id any, msg string) {
	s.sendResult(id, CallToolResult{
		Content: []ToolContent{
			{
				Type: "text",
				Text: "Error: " + msg,
			},
		},
		IsError: true,
	})
}

func (s *Server) sendResult(id any, result any) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	b, _ := json.Marshal(resp)
	_, _ = fmt.Fprintln(s.writer, string(b))
}

func (s *Server) sendError(id any, code int, message string, data any) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	b, _ := json.Marshal(resp)
	_, _ = fmt.Fprintln(s.writer, string(b))
}
