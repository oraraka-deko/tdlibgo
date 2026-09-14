package main

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.etcd.io/bbolt"

	"tdlibgo/internal/auth"
	"tdlibgo/internal/logger"
	"tdlibgo/internal/models"
	"tdlibgo/internal/server"
	"tdlibgo/internal/services"
	"tdlibgo/internal/state"
	"tdlibgo/internal/storage"
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

	port := 22816
	if pStr := os.Getenv("PORT"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			port = p
		}
	}

	authEnabled := strings.EqualFold(os.Getenv("AUTH_ENABLED"), "true") || os.Getenv("AUTH_ENABLED") == "1"
	authURL := os.Getenv("AUTH_URL")
	if authURL == "" {
		authURL = fmt.Sprintf("http://localhost:%d", port)
	}

	oauthCfg := auth.Config{
		Enabled:            authEnabled,
		URL:                authURL,
		Secret:             os.Getenv("AUTH_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		GithubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GithubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GithubRedirectURL:  os.Getenv("GITHUB_REDIRECT_URL"),
	}

	oauthMgr, err := auth.NewOAuthManager(oauthCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize OAuth manager: %v\n", err)
	}

	fmt.Println("==================================================================")
	fmt.Println("       Telegram Web Client & MTProto Go State Manager             ")
	fmt.Println("==================================================================")
	fmt.Printf("Phone:    %s\n", phone)
	fmt.Printf("App ID:   %d\n", appID)
	fmt.Printf("Web UI:   http://localhost:%d\n", port)
	if authEnabled {
		fmt.Printf("OAuth:    Enabled (Google / GitHub) [Stateless JWT Sessions]\n")
	} else {
		fmt.Printf("OAuth:    Optional / Disabled (Direct Access)\n")
	}
	fmt.Println("==================================================================")

	cleanPhone := strings.ReplaceAll(strings.ReplaceAll(phone, "+", ""), " ", "")
	sessionDir := filepath.Join("session", "phone-"+cleanPhone)
	_ = os.MkdirAll(sessionDir, 0755)

	// Initialize structured high-visibility logger
	logger.InitLogger(sessionDir, "telegram.log")
	logger.Info("AUTH", "Starting Telegram Web Client for %s", phone)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Initialize State Manager
	stateMgr := state.NewStateManager(phone)

	// Initialize Persistent History Database (bbolt)
	dbPath := filepath.Join(sessionDir, "history.db")
	historyDB, err := storage.OpenHistoryDB(dbPath)
	if err != nil {
		logger.Error("DB", "Failed to open persistent history database at %s: %v", dbPath, err)
	} else {
		stateMgr.SetHistoryDB(historyDB)
		defer historyDB.Close()
	}

	// Initialize MTProto Client Controller
	clientCtrl := telegram.NewClientController(appID, appHash, phone, stateMgr)

	// Initialize Persistent Queue Manager (bbolt)
	queueDBPath := filepath.Join(sessionDir, "queue.db")
	queueDB, err := bbolt.Open(queueDBPath, 0600, nil)
	var queueMgr *services.QueueManager
	if err != nil {
		logger.Error("QUEUE", "Failed to open queue database at %s: %v", queueDBPath, err)
	} else {
		defer queueDB.Close()
		qm, qErr := services.NewQueueManager(queueDB, nil)
		if qErr != nil {
			logger.Error("QUEUE", "Failed to initialize QueueManager: %v", qErr)
		} else {
			queueMgr = qm
			defer queueMgr.Close()
		}
	}

	// Initialize HTTP and WebSocket Server with embedded web assets, OAuth manager, and Queue manager
	srv := server.NewServer(port, stateMgr, clientCtrl, webFiles, oauthMgr, queueMgr)

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
