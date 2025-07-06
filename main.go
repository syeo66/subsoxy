package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/syeo66/subsoxy/config"
	"github.com/syeo66/subsoxy/server"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}
	
	proxyServer, err := server.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create proxy server: %v\n", err)
		os.Exit(1)
	}

	handlers := proxyServer.GetHandlers()

	proxyServer.AddHook("/rest/ping", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandlePing(w, r, endpoint)
	})

	proxyServer.AddHook("/rest/getLicense", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandleGetLicense(w, r, endpoint)
	})

	proxyServer.AddHook("/rest/stream", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandleStream(w, r, endpoint, proxyServer.RecordPlayEvent)
	})

	proxyServer.AddHook("/rest/scrobble", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandleScrobble(w, r, endpoint, proxyServer.RecordPlayEvent, proxyServer.SetLastPlayed)
	})

	proxyServer.AddHook("/rest/getRandomSongs", func(w http.ResponseWriter, r *http.Request, endpoint string) bool {
		return handlers.HandleShuffle(w, r, endpoint)
	})

	if err := proxyServer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start proxy server: %v\n", err)
		os.Exit(1)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	
	<-quit
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := proxyServer.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to shutdown proxy server gracefully: %v\n", err)
		os.Exit(1)
	}
}