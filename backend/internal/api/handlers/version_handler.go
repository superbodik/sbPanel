package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type VersionHandler struct {
	Version   string
	Commit    string
	BuildDate string
	SourceDir string
	RepoSlug  string
}

func (h *VersionHandler) Get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    h.Version,
		"commit":     h.Commit,
		"build_date": h.BuildDate,
		"source_dir": h.SourceDir,
		"repo_slug":  h.RepoSlug,
	})
}

func (h *VersionHandler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	latest, err := latestReleasedVersion(ctx, h.RepoSlug)
	if err != nil {
		http.Error(w, "failed to reach GitHub: "+err.Error(), http.StatusBadGateway)
		return
	}

	updateAvailable := h.Version != "" && h.Version != "0.0.0-dev" && latest != "" && latest != h.Version

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  h.Version,
		"latest_version":   latest,
		"update_available": updateAvailable,
	})
}

func latestReleasedVersion(ctx context.Context, repoSlug string) (string, error) {
	url := "https://raw.githubusercontent.com/" + repoSlug + "/main/VERSION"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("GitHub returned %d fetching VERSION", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}
