package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/hkdb/aerion/internal/automation/client"
	"github.com/hkdb/aerion/internal/automation/mcp"
)

func main() {
	endpoint := flag.String("endpoint", os.Getenv("POSTILAATIKKO_API_URL"), "Postilaatikko automation endpoint URL")
	token := flag.String("token", os.Getenv("POSTILAATIKKO_API_TOKEN"), "Bearer token")
	flag.Parse()

	if *endpoint == "" {
		*endpoint = "http://127.0.0.1:8080"
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: POSTILAATIKKO_API_TOKEN or --token flag is required")
		os.Exit(1)
	}

	c := client.New(client.Config{
		Endpoint: *endpoint,
		Token:    *token,
	})

	server := mcp.NewServer(c, os.Stdin, os.Stdout)
	if err := server.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server stopped: %v\n", err)
		os.Exit(1)
	}
}
