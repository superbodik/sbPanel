package daemonclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Client struct {
	baseURL     string
	daemonToken string
	http        *http.Client
	longHTTP    *http.Client
}

func New(baseURL, daemonToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		daemonToken: daemonToken,
		http:        &http.Client{Timeout: 15 * time.Second},
		longHTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

type PowerAction string

const (
	PowerStart   PowerAction = "start"
	PowerStop    PowerAction = "stop"
	PowerRestart PowerAction = "restart"
	PowerKill    PowerAction = "kill"
)

type CreateServerRequest struct {
	ServerUUID     uuid.UUID         `json:"server_uuid"`
	DockerImage    string            `json:"docker_image"`
	StartupCommand string            `json:"startup_command"`
	Environment    map[string]string `json:"environment"`
	MemoryMB       int64             `json:"memory_mb"`
	SwapMB         int64             `json:"swap_mb"`
	DiskMB         int64             `json:"disk_mb"`
	IOWeight       int               `json:"io_weight"`
	CPUPercent     int               `json:"cpu_percent"`
	InstallScript  string            `json:"install_script"`
	PortBindings   map[string]string `json:"port_bindings"`
}

type OperationResponse struct {
	ServerUUID uuid.UUID `json:"server_uuid"`
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	State      string    `json:"state"`
}

func (c *Client) CreateServer(ctx context.Context, req CreateServerRequest) (*OperationResponse, error) {
	var resp OperationResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/servers", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Power(ctx context.Context, serverUUID uuid.UUID, action PowerAction) (*OperationResponse, error) {
	var resp OperationResponse
	path := fmt.Sprintf("/api/v1/servers/%s/power", serverUUID)
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]string{"action": string(action)}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteServer(ctx context.Context, serverUUID uuid.UUID) error {
	path := fmt.Sprintf("/api/v1/servers/%s", serverUUID)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) SendCommand(ctx context.Context, serverUUID uuid.UUID, command string) error {
	path := fmt.Sprintf("/api/v1/servers/%s/command", serverUUID)
	return c.doJSON(ctx, http.MethodPost, path, map[string]string{"command": command}, nil)
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call node daemon: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("node daemon returned %d", resp.StatusCode)
	}
	return nil
}

type ResourceStats struct {
	ServerUUID    uuid.UUID `json:"server_uuid"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryBytes   int64     `json:"memory_bytes"`
	DiskBytes     int64     `json:"disk_bytes"`
	NetworkRx     int64     `json:"network_rx"`
	NetworkTx     int64     `json:"network_tx"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	State         string    `json:"state"`
}

func (c *Client) Stats(ctx context.Context, serverUUID uuid.UUID) (*ResourceStats, error) {
	var resp ResourceStats
	path := fmt.Sprintf("/api/v1/servers/%s/stats", serverUUID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type FileEntry struct {
	Name        string `json:"name"`
	IsDirectory bool   `json:"is_directory"`
	SizeBytes   int64  `json:"size_bytes"`
	ModifiedAt  int64  `json:"modified_at"`
	Mode        string `json:"mode"`
}

func (c *Client) ListFiles(ctx context.Context, serverUUID uuid.UUID, path string) ([]FileEntry, error) {
	var entries []FileEntry
	p := fmt.Sprintf("/api/v1/servers/%s/files?path=%s", serverUUID, url.QueryEscape(path))
	if err := c.doJSON(ctx, http.MethodGet, p, nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *Client) ReadFile(ctx context.Context, serverUUID uuid.UUID, path string) ([]byte, error) {
	p := fmt.Sprintf("/api/v1/servers/%s/files/contents?path=%s", serverUUID, url.QueryEscape(path))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+p, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.daemonToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call node daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("node daemon returned %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) WriteFile(ctx context.Context, serverUUID uuid.UUID, path string, content []byte) error {
	p := fmt.Sprintf("/api/v1/servers/%s/files/contents?path=%s", serverUUID, url.QueryEscape(path))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+p, bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.daemonToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call node daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("node daemon returned %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) DeleteFile(ctx context.Context, serverUUID uuid.UUID, path string) error {
	p := fmt.Sprintf("/api/v1/servers/%s/files?path=%s", serverUUID, url.QueryEscape(path))
	return c.doJSON(ctx, http.MethodDelete, p, nil, nil)
}

func (c *Client) CreateDirectory(ctx context.Context, serverUUID uuid.UUID, path string) error {
	p := fmt.Sprintf("/api/v1/servers/%s/files/directory?path=%s", serverUUID, url.QueryEscape(path))
	return c.doJSON(ctx, http.MethodPost, p, nil, nil)
}

func (c *Client) RenameFile(ctx context.Context, serverUUID uuid.UUID, from, to string) error {
	p := fmt.Sprintf("/api/v1/servers/%s/files/rename", serverUUID)
	return c.doJSON(ctx, http.MethodPost, p, map[string]string{"from": from, "to": to}, nil)
}

func (c *Client) DialConsole(ctx context.Context, serverUUID uuid.UUID) (*websocket.Conn, error) {
	wsURL := strings.Replace(c.baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	url := fmt.Sprintf("%s/ws/servers/%s", wsURL, serverUUID)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.daemonToken)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, header)
	if err != nil {
		return nil, fmt.Errorf("dial daemon console: %w", err)
	}
	return conn, nil
}

type AddDomainRequest struct {
	Domain string `json:"domain"`
	Port   int    `json:"port"`
	Email  string `json:"email"`
}

type AddDomainResponse struct {
	Domain    string `json:"domain"`
	TLSStatus string `json:"tls_status"`
}

func (c *Client) AddDomain(ctx context.Context, serverUUID uuid.UUID, req AddDomainRequest) (*AddDomainResponse, error) {
	var resp AddDomainResponse
	path := fmt.Sprintf("/api/v1/servers/%s/domains", serverUUID)
	if err := c.doJSONWith(c.longHTTP, ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) RemoveDomain(ctx context.Context, serverUUID uuid.UUID, domain string) error {
	path := fmt.Sprintf("/api/v1/servers/%s/domains/%s", serverUUID, url.QueryEscape(domain))
	return c.doJSONWith(c.longHTTP, ctx, http.MethodDelete, path, nil, nil)
}

type CreateBackupRequest struct {
	BackupUUID   string   `json:"backup_uuid"`
	IgnoredFiles []string `json:"ignored_files"`
}

type CreateBackupResponse struct {
	Bytes    int64  `json:"bytes"`
	Checksum string `json:"checksum"`
}

func (c *Client) CreateBackup(ctx context.Context, serverUUID uuid.UUID, req CreateBackupRequest) (*CreateBackupResponse, error) {
	var resp CreateBackupResponse
	path := fmt.Sprintf("/api/v1/servers/%s/backups", serverUUID)
	if err := c.doJSONWith(c.longHTTP, ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) RestoreBackup(ctx context.Context, serverUUID uuid.UUID, backupUUID string) error {
	path := fmt.Sprintf("/api/v1/servers/%s/backups/%s/restore", serverUUID, backupUUID)
	return c.doJSONWith(c.longHTTP, ctx, http.MethodPost, path, nil, nil)
}

func (c *Client) DeleteBackup(ctx context.Context, serverUUID uuid.UUID, backupUUID string) error {
	path := fmt.Sprintf("/api/v1/servers/%s/backups/%s", serverUUID, backupUUID)
	return c.doJSONWith(c.http, ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) DownloadBackup(ctx context.Context, serverUUID uuid.UUID, backupUUID string) (io.ReadCloser, error) {
	path := fmt.Sprintf("/api/v1/servers/%s/backups/%s/download", serverUUID, backupUUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.daemonToken)

	resp, err := c.longHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call node daemon: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("node daemon returned %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out interface{}) error {
	return c.doJSONWith(c.http, ctx, method, path, body, out)
}

func (c *Client) doJSONWith(client *http.Client, ctx context.Context, method, path string, body, out interface{}) error {
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.daemonToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call node daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("node daemon returned %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
