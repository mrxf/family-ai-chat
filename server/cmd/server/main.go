// Command server runs the Nuan (小暖) chat backend.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nuan/internal/api"
	"nuan/internal/chat"
	"nuan/internal/config"
	"nuan/internal/store"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	var handler slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	}
	log := slog.New(handler)

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "nuan.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	ctx := context.Background()
	if n, err := st.MarkInterrupted(ctx); err == nil && n > 0 {
		log.Info("marked interrupted generations", "count", n)
	}
	if err := seed(ctx, st, cfg, log); err != nil {
		return err
	}

	svc := chat.NewService(st, cfg.DataDir, log)
	apiCfg := api.Config{DataDir: cfg.DataDir, AppVersion: version}
	if cfg.ASRURL != "" && cfg.ASRKey != "" {
		apiCfg.ASR = &api.ASRConfig{BaseURL: cfg.ASRURL, APIKey: cfg.ASRKey, Model: cfg.ASRModel}
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.New(apiCfg, st, svc, log).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // uploads; no WriteTimeout because of SSE
		IdleTimeout:       120 * time.Second,
	}

	// Housekeeping.
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for range t.C {
			_ = st.DeleteExpiredSessions(context.Background())
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "version", version, "data", cfg.DataDir, "voice", apiCfg.ASR != nil)
		errCh <- srv.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case s := <-sig:
		log.Info("shutting down", "signal", s.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Stop generations first so their partial output is saved and SSE clients get `done`.
	svc.Shutdown(shutdownCtx)
	_ = srv.Shutdown(shutdownCtx)
	return nil
}
