package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhcp-server/dhcp-server/internal/api"
	"github.com/dhcp-server/dhcp-server/internal/auth"
	"github.com/dhcp-server/dhcp-server/internal/config"
	"github.com/dhcp-server/dhcp-server/internal/db"
	"github.com/dhcp-server/dhcp-server/internal/dhcp"
	"github.com/dhcp-server/dhcp-server/internal/dhcpv6"
	"github.com/dhcp-server/dhcp-server/internal/ha"
	"github.com/dhcp-server/dhcp-server/internal/logger"
	"github.com/dhcp-server/dhcp-server/internal/store"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	consoleLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		consoleLogger.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.New(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		consoleLogger.Error("connect database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.Migrate(); err != nil {
		consoleLogger.Error("migrate database", "err", err)
		os.Exit(1)
	}

	st := store.New(database.Pool)

	// Wrap console logger with a database-backed handler so warn/error records
	// are also persisted and viewable in the web UI.
	logger := slog.New(logger.NewDBHandler(st, cfg.Cluster.NodeID, os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Periodically clean up leases that have been expired for longer than the
	// scope's max lease time. Run once at startup, then every 10 minutes.
	go func() {
		cleanup := func() {
			n, err := st.CleanupExpiredLeases(ctx)
			if err != nil {
				logger.Error("cleanup expired leases", "err", err)
			} else if n > 0 {
				logger.Info("cleaned up expired leases and logs", "count", n)
			}
		}
		cleanup()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()

	authSvc, err := auth.NewService(cfg, st)
	if err != nil {
		logger.Error("init auth", "err", err)
		os.Exit(1)
	}
	if err := authSvc.InitializeDefaultAdmin(ctx); err != nil {
		logger.Error("init admin", "err", err)
		os.Exit(1)
	}

	dhcpServer := dhcp.NewServer(cfg, st, logger)
	if err := dhcpServer.Start(ctx); err != nil {
		logger.Error("start dhcp server", "err", err)
		os.Exit(1)
	}
	defer dhcpServer.Stop()

	dhcpv6Server := dhcpv6.NewServer(cfg, st, logger)
	if err := dhcpv6Server.Start(ctx); err != nil {
		logger.Error("start dhcpv6 server", "err", err)
		os.Exit(1)
	}
	defer dhcpv6Server.Stop()

	apiServer := api.New(cfg, st, authSvc, dhcpServer, logger)
	httpServer := &http.Server{
		Addr:         cfg.Server.HTTPListen,
		Handler:      apiServer.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", cfg.Server.HTTPListen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", "err", err)
		}
	}()

	haMgr := ha.NewManager(cfg, st)
	go haMgr.Run(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	cancel()
	_ = dhcpServer.Stop()
	_ = dhcpv6Server.Stop()
}
