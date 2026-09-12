package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"tdlibgo/internal/models"
	"tdlibgo/internal/server"
	"tdlibgo/internal/state"
	"tdlibgo/internal/telegram"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	// Load environment variables from .env
	_ = godotenv.Load()

	appIDStr := os.Getenv("APP_ID")
	if appIDStr == "" {
		appIDStr = "39015859"
	}
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid APP_ID: %v\n", err)
		os.Exit(1)
	}

	appHash := os.Getenv("APP_HASH")
	if appHash == "" {
		appHash = "73a0e8b9ba584a34a3d8e73762393732"
	}

	phone := os.Getenv("TG_PHONE")
	if phone == "" {
		phone = "+12294660989"
	}

	port := 8080
	if pStr := os.Getenv("PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			port = p
		}
	}

	fmt.Println("==================================================================")
	fmt.Println("       Telegram Web Client & MTProto Go State Manager             ")
	fmt.Println("==================================================================")
	fmt.Printf("Phone:    %s\n", phone)
	fmt.Printf("App ID:   %d\n", appID)
	fmt.Printf("Web UI:   http://localhost:%d\n", port)
	fmt.Println("==================================================================")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize State Manager
	stateMgr := state.NewStateManager(phone)

	// Initialize MTProto Client Controller
	clientCtrl := telegram.NewClientController(appID, appHash, phone, stateMgr)

	// Initialize HTTP and WebSocket Server with embedded web assets
	srv := server.NewServer(port, stateMgr, clientCtrl, webFiles)

	// Interactive terminal console reader
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			currAuth := stateMgr.GetAuthState()
			switch currAuth.State {
			case models.AuthStateWaitingCode:
				fmt.Printf("[CONSOLE] Verification code entered: %s\n", line)
				clientCtrl.SubmitCode(line)
			case models.AuthStateWaitingPassword:
				fmt.Println("[CONSOLE] 2FA Password entered.")
				clientCtrl.SubmitPassword(line)
			default:
				fmt.Printf("[CONSOLE] Received input: %s\n", line)
			}
		}
	}()

	// Start MTProto client in background goroutine
	if err := clientCtrl.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start MTProto client: %v\n", err)
		os.Exit(1)
	}

	// Start HTTP / WebSocket server
	srvErrCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			srvErrCh <- err
		}
	}()

	// Wait for interrupt signal or server error
	select {
	case <-ctx.Done():
		fmt.Println("\nShutting down gracefully...")
	case err := <-srvErrCh:
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = srv.Stop(shutdownCtx)
	fmt.Println("Telegram Web service stopped.")
}
