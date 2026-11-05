package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hkdb/aerion/internal/automation/client"
	v1 "github.com/hkdb/aerion/internal/automation/v1"
	"github.com/hkdb/aerion/internal/operations"
	"github.com/hkdb/aerion/internal/views"
)

func main() {
	endpoint := flag.String("endpoint", os.Getenv("POSTILAATIKKO_API_URL"), "Postilaatikko automation endpoint URL (or POSTILAATIKKO_API_URL env)")
	token := flag.String("token", os.Getenv("POSTILAATIKKO_API_TOKEN"), "Bearer token (or POSTILAATIKKO_API_TOKEN env)")
	jsonOutput := flag.Bool("json", false, "Output results as formatted JSON")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	if *endpoint == "" {
		*endpoint = "http://127.0.0.1:8080"
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: authentication token is required (--token or POSTILAATIKKO_API_TOKEN)")
		os.Exit(1)
	}

	c := client.New(client.Config{
		Endpoint: *endpoint,
		Token:    *token,
		Timeout:  30 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "capabilities":
		res, err := c.GetCapabilities(ctx)
		handleResult(res, err, *jsonOutput)

	case "accounts":
		res, err := c.GetAccounts(ctx)
		handleResult(res, err, *jsonOutput)

	case "folders":
		accountID := ""
		if len(cmdArgs) > 0 {
			accountID = cmdArgs[0]
		}
		res, err := c.GetFolders(ctx, accountID)
		handleResult(res, err, *jsonOutput)

	case "views":
		res, err := c.GetViews(ctx)
		handleResult(res, err, *jsonOutput)

	case "search":
		if len(cmdArgs) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: postilaatikko-cli search <query>")
			os.Exit(1)
		}
		query := strings.Join(cmdArgs, " ")
		req := v1.SearchRequest{
			Scope: views.Scope{
				Kind:  views.ScopeSearch,
				Query: query,
			},
		}
		res, err := c.Search(ctx, req)
		handleResult(res, err, *jsonOutput)

	case "thread":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: postilaatikko-cli thread <accountId> <threadId>")
			os.Exit(1)
		}
		req := v1.ThreadReadRequest{
			Ref: views.ThreadRef{
				AccountID: cmdArgs[0],
				ThreadID:  cmdArgs[1],
			},
		}
		res, err := c.ReadThread(ctx, req)
		handleResult(res, err, *jsonOutput)

	case "body":
		if len(cmdArgs) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: postilaatikko-cli body <accountId> <messageId> [fetch]")
			os.Exit(1)
		}
		mode := v1.FetchCachedOnly
		if len(cmdArgs) > 2 && cmdArgs[2] == "fetch" {
			mode = v1.FetchExplicit
		}
		req := v1.BodyReadRequest{
			Ref: views.MessageRef{
				AccountID: cmdArgs[0],
				MessageID: cmdArgs[1],
			},
			Mode:            mode,
			IncludeOriginal: true,
		}
		res, err := c.ReadBody(ctx, req)
		handleResult(res, err, *jsonOutput)

	case "action":
		if len(cmdArgs) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: postilaatikko-cli action <actionKind> <accountId> <messageId> [destination]")
			os.Exit(1)
		}
		actionKind := v1.ActionKind(cmdArgs[0])
		accountID := cmdArgs[1]
		messageID := cmdArgs[2]
		destination := ""
		if len(cmdArgs) > 3 {
			destination = cmdArgs[3]
		}

		req := v1.ActionRequest{
			Action: actionKind,
			Target: operations.Target{
				Kind: operations.TargetMessage,
				Messages: []operations.MessageRef{
					{AccountID: accountID, MessageID: messageID},
				},
			},
			Destination: destination,
		}
		idempotencyKey := fmt.Sprintf("cli-%d", time.Now().UnixNano())
		res, err := c.MutateAction(ctx, req, idempotencyKey)
		handleResult(res, err, *jsonOutput)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func handleResult(data any, err error, jsonOutput bool) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data)
		return
	}

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("%+v\n", data)
		return
	}
	fmt.Println(string(out))
}

func printUsage() {
	fmt.Println(`postilaatikko-cli - CLI for Postilaatikko Local Agent API

Usage:
  postilaatikko-cli [flags] <command> [args...]

Commands:
  capabilities                               Show authorized capabilities
  accounts                                   List configured accounts
  folders [accountId]                        List folders
  views                                      List saved views / virtual folders
  search <query>                             Search messages
  thread <accountId> <threadId>              Read thread metadata & messages
  body <accountId> <messageId> [fetch]       Read/fetch message body & extracted reply
  action <action> <accountId> <msgId> [dest] Perform mail mutation (archive, mark_read, star, move, trash)

Flags:
  --endpoint string   API URL (default: http://127.0.0.1:8080)
  --token string      Bearer token
  --json              Output raw formatted JSON`)
}
