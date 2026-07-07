package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourorg/panel-daemon/internal/api"
	"github.com/yourorg/panel-daemon/internal/config"
	"github.com/yourorg/panel-daemon/internal/console"
	"github.com/yourorg/panel-daemon/internal/docker"
	"github.com/yourorg/panel-daemon/internal/sftpd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	if cfg.DaemonToken == "" {
		log.Fatal("WINGSD_DAEMON_TOKEN is required (issued by the panel when the node is created)")
	}

	dockerManager, err := docker.NewManager(cfg.DockerSocket, cfg.DataDir)
	if err != nil {
		log.Fatalf("docker: %v", err)
	}
	consoleHub := console.NewHub(dockerManager)

	if cfg.PanelURL == "" {
		log.Println("warning: WINGSD_PANEL_URL is not set — SFTP logins will be rejected until it's configured")
	}
	sftpServer, err := sftpd.NewServer(dockerManager, cfg.PanelURL, cfg.DaemonToken, cfg.SFTPHostKey)
	if err != nil {
		log.Fatalf("sftp: %v", err)
	}
	go func() {
		if err := sftpServer.ListenAndServe(cfg.SFTPAddr); err != nil {
			log.Fatalf("sftp: %v", err)
		}
	}()

	router := api.NewRouter(dockerManager, consoleHub, cfg.DaemonToken, cfg.BackupDir)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	go func() {
		log.Printf("wingsd listening on %s", cfg.HTTPAddr)
		var err error
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			log.Println("warning: running without TLS — set WINGSD_TLS_CERT/WINGSD_TLS_KEY in production")
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
