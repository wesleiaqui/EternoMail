package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"
)

// startLocalProfiler starts the standard Go pprof handlers only when explicitly
// requested. It binds an ephemeral loopback port so profiling is not exposed to
// the local network and does not conflict with another app instance.
func startLocalProfiler() func() {
	if os.Getenv("ETERNOMAIL_PPROF") != "1" {
		return func() {}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not start local Go profiler: %v\n", err)
		return func() {}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Local Go profiler stopped: %v\n", err)
		}
	}()
	fmt.Fprintf(os.Stderr, "Local Go profiler: http://%s/debug/pprof/\n", listener.Addr())

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}
}
