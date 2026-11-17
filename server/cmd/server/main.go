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
	if err := svc.RepairSettings(ctx); err != nil {
		return err
	}
	apiSrv := api.New(apiCfg, st, svc, log)
	if err := apiSrv.InitSetup(ctx); err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiSrv.Routes(),
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
	go func() {
		sweep := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if n, err := svc.CleanupFiles(ctx, chat.FileGrace); err != nil {
				log.Warn("file cleanup", "removed", n, "err", err)
			} else if n > 0 {
				log.Info("file cleanup", "removed", n)
			}
		}
		time.Sleep(2 * time.Minute) // let startup settle
		sweep()
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			sweep()
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		voiceOn, _, voiceKind := apiSrv.ASR.ActiveStatus(context.Background())
		log.Info("listening", "addr", cfg.Addr, "version", version, "data", cfg.DataDir, "voice", voiceOn, "asr", voiceKind)
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
