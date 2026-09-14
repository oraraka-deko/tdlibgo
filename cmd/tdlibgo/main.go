package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const banner = `
================================================================================
                    TDLIBGO UNIFIED TELEGRAM PLATFORM                           
        Client Web UI • Zero-Docker MTProto Server • Bot API • Admin            
================================================================================
`

func printUsage() {
	fmt.Print(banner)
	fmt.Println(`Usage:
  tdlibgo [command] [flags...]

Commands:
  client       Run Telegram Web Client & MTProto State Engine (default)
  server       Run Zero-Docker Standalone MTProto Telegram Server (:2401)
  admin        Run Telegram Server Admin Service & Dashboard
  all          Run BOTH MTProto Server and Web Client concurrently
  load         Run MTProto Benchmark & Load Testing Engine
  update       Run Background Update Dispatcher Daemon
  tools <name> Run specialized Telegram offline/online tool:
                 • appearancefetch    • blobmigrate         • catalogfetch
                 • giftcheck          • giftfetch           • langpackfetch
                 • starcheck          • stickerfetch        • stickerseeddeploy
                 • telegramloginkeygen • otpwebhook-example • walletminiapp

Examples:
  go run ./cmd/tdlibgo client           # Starts Web Client at http://localhost:22816
  go run ./cmd/tdlibgo server           # Starts Standalone MTProto Server at :2401
  go run ./cmd/tdlibgo all              # Starts both Server & Client
  go run ./cmd/tdlibgo tools giftfetch  # Runs official gift catalog fetcher
`)
}

func main() {
	if len(os.Args) < 2 {
		runClient(os.Args[1:])
		return
	}

	cmd := strings.ToLower(os.Args[1])
	subArgs := os.Args[2:]

	switch cmd {
	case "client", "web":
		runClient(subArgs)

	case "server", "telesrv":
		runSubcommand("telesrv", subArgs)

	case "admin", "telesrv-admin":
		runSubcommand("telesrv-admin", subArgs)

	case "load", "telesrv-load":
		runSubcommand("telesrv-load", subArgs)

	case "update", "telesrv-update":
		runSubcommand("telesrv-update", subArgs)

	case "all":
		runAll()

	case "tools", "tool":
		if len(subArgs) == 0 {
			fmt.Println("Error: Missing tool name. Available tools:")
			fmt.Println("  appearancefetch, blobmigrate, catalogfetch, giftcheck, giftfetch,")
			fmt.Println("  langpackfetch, starcheck, stickerfetch, stickerseeddeploy,")
			fmt.Println("  telegramloginkeygen, otpwebhook-example, walletminiapp")
			os.Exit(1)
		}
		toolName := subArgs[0]
		runSubcommand(toolName, subArgs[1:])

	case "-h", "--help", "help":
		printUsage()

	default:
		// Check if the argument is a known tool directly
		knownTools := map[string]bool{
			"appearancefetch": true, "blobmigrate": true, "catalogfetch": true,
			"giftcheck": true, "giftfetch": true, "langpackfetch": true,
			"starcheck": true, "stickerfetch": true, "stickerseeddeploy": true,
			"telegramloginkeygen": true, "otpwebhook-example": true, "walletminiapp": true,
		}
		if knownTools[cmd] {
			runSubcommand(cmd, subArgs)
			return
		}
		fmt.Printf("Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runClient(args []string) {
	fmt.Print(banner)
	fmt.Println("Starting Telegram Web Client & MTProto State Engine...")

	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	forwardSignals(cmd)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Client exited: %v\n", err)
		os.Exit(1)
	}
}

func runSubcommand(pkgName string, args []string) {
	target := filepath.Join(".", "cmd", pkgName)
	cmd := exec.Command("go", append([]string{"run", target}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	forwardSignals(cmd)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Tool %s exited: %v\n", pkgName, err)
		os.Exit(1)
	}
}

func runAll() {
	fmt.Print(banner)
	fmt.Println("[TDLIBGO ALL] Launching MTProto Server + Web Client concurrently...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Launch Server
	serverCmd := exec.CommandContext(ctx, "go", "run", "./cmd/telesrv")
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start MTProto server: %v\n", err)
		os.Exit(1)
	}
	go func() {
		if err := serverCmd.Wait(); err != nil {
			fmt.Printf("\n[TDLIBGO ALL] MTProto server process exited (%v).\n", err)
		}
	}()

	// 2. Launch Client
	clientCmd := exec.CommandContext(ctx, "go", "run", ".")
	clientCmd.Stdout = os.Stdout
	clientCmd.Stderr = os.Stderr
	clientCmd.Stdin = os.Stdin
	if err := clientCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start Web Client: %v\n", err)
		os.Exit(1)
	}
	go func() {
		if err := clientCmd.Wait(); err != nil {
			fmt.Printf("\n[TDLIBGO ALL] Web Client process exited (%v).\n", err)
		}
		stop()
	}()

	fmt.Println("[TDLIBGO ALL] Both services are actively running. Press Ctrl+C to terminate.")

	select {
	case <-ctx.Done():
		fmt.Println("\n[TDLIBGO ALL] Graceful shutdown triggered...")
	}
}

func forwardSignals(cmd *exec.Cmd) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()
}
