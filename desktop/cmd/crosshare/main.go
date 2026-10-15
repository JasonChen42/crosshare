package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crosshare/desktop/internal/config"
	"github.com/crosshare/desktop/internal/discovery"
	"github.com/crosshare/desktop/internal/server"
	"github.com/crosshare/desktop/internal/storage"
	csync "github.com/crosshare/desktop/internal/sync"
	"github.com/crosshare/desktop/internal/tray"
)

func main() {
	noTrayFlag := flag.Bool("no-tray", false, "run without the system tray UI")
	portFlag := flag.Int("port", 0, "override HTTP port for this run")
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("CrossHare starting...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if *portFlag > 0 {
		cfg.HTTPPort = *portFlag
	}
	log.Printf("Config loaded. Target folder: %s", cfg.GetTargetFolder())
	log.Printf("Pair code: %s", cfg.GetPairCode())

	// Ensure target folder exists
	if err := os.MkdirAll(cfg.GetTargetFolder(), 0755); err != nil {
		log.Fatalf("Failed to create target folder: %v", err)
	}

	// Open database
	db, err := storage.Open()
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Clean stale download progress entries (older than 7 days)
	db.CleanStaledProgress(7 * 24 * time.Hour)

	// Create discovery scanner
	interval := time.Duration(cfg.DiscoveryIntervalSeconds) * time.Second
	disc := discovery.New(interval)

	// Create sync manager
	syncMgr := csync.NewManager(cfg, db, disc)

	// Create and start HTTP server (for manual phone push + pairing)
	srv := server.New(cfg, db, syncMgr)
	listener, err := srv.Listen()
	if err != nil {
		log.Fatalf("Failed to bind HTTP server: %v", err)
	}
	go func() {
		if err := srv.Serve(listener); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	_, cancel := context.WithCancel(context.Background())

	log.Printf("HTTP server listening on %s", listener.Addr())
	log.Printf("Photo library ready; phones can upload and receive originals")

	// Set up graceful shutdown
	quit := make(chan struct{})
	var quitOnce sync.Once
	onQuit := func() {
		quitOnce.Do(func() {
			close(quit)
		})
	}

	// Handle OS signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		onQuit()
	}()

	shutdownDone := make(chan struct{})
	go func() {
		<-quit
		log.Println("Shutting down...")

		// Stop discovery
		cancel()
		disc.Stop()

		// Stop HTTP server
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Stop(shutdownCtx)

		log.Println("CrossHare stopped.")
		close(shutdownDone)
	}()

	if *noTrayFlag || envBool("CROSSHARE_NO_TRAY") {
		log.Printf("Running without system tray. Local UI: http://127.0.0.1:%d/", cfg.HTTPPort)
		<-shutdownDone
		return
	}

	// Run the tray (this blocks)
	trayInstance := tray.New(cfg, syncMgr, onQuit)
	trayInstance.Run()
	onQuit()
	<-shutdownDone
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
