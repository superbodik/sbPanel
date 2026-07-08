package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yourorg/panel-daemon/internal/console"
	"github.com/yourorg/panel-daemon/internal/docker"
)

const maxRequestBodyBytes = 100 << 20

func maxBodySize(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func NewRouter(dockerManager *docker.Manager, consoleHub *console.Hub, daemonToken, backupDir, version string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(maxBodySize(maxRequestBodyBytes))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
	})

	r.Group(func(r chi.Router) {
		r.Use(RequireDaemonToken(daemonToken))

		h := &Handlers{Docker: dockerManager, Console: consoleHub, BackupDir: backupDir}

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))

			r.Route("/api/v1", func(r chi.Router) {
				r.Post("/servers", h.CreateServer)
				r.Post("/servers/{uuid}/power", h.Power)
				r.Post("/servers/{uuid}/command", h.SendCommand)
				r.Delete("/servers/{uuid}", h.Delete)
				r.Get("/servers/{uuid}/stats", h.Stats)

				r.Get("/servers/{uuid}/files", h.ListFiles)
				r.Get("/servers/{uuid}/files/contents", h.ReadFile)
				r.Put("/servers/{uuid}/files/contents", h.WriteFile)
				r.Delete("/servers/{uuid}/files", h.DeleteFile)
				r.Post("/servers/{uuid}/files/directory", h.CreateDirectory)
				r.Post("/servers/{uuid}/files/rename", h.RenameFile)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(150 * time.Second))
			r.Post("/api/v1/servers/{uuid}/domains", h.AddDomain)
			r.Delete("/api/v1/servers/{uuid}/domains/{domain}", h.RemoveDomain)

			r.Post("/api/v1/servers/{uuid}/backups", h.CreateBackup)
			r.Post("/api/v1/servers/{uuid}/backups/{backup_uuid}/restore", h.RestoreBackup)
			r.Delete("/api/v1/servers/{uuid}/backups/{backup_uuid}", h.DeleteBackup)
			r.Get("/api/v1/servers/{uuid}/backups/{backup_uuid}/download", h.DownloadBackup)
		})

		r.Get("/ws/servers/{uuid}", h.ConsoleSocket)
	})

	return r
}
