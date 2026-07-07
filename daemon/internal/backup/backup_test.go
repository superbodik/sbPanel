package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndRestore(t *testing.T) {
	serverDir := t.TempDir()
	backupDir := t.TempDir()

	mustWrite(t, filepath.Join(serverDir, "world", "level.dat"), "level-data")
	mustWrite(t, filepath.Join(serverDir, "server.properties"), "motd=hello")
	mustWrite(t, filepath.Join(serverDir, "server.log"), "should be ignored")
	mustWrite(t, filepath.Join(serverDir, "cache", "junk.tmp"), "ignored dir contents")

	serverUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	backupUUID := "11111111-2222-3333-4444-555555555555"

	bytesWritten, checksum, err := Create(serverDir, backupDir, serverUUID, backupUUID, []string{"*.log", "cache"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if bytesWritten == 0 {
		t.Fatalf("expected non-zero bytes written")
	}

	archive := archivePath(backupDir, serverUUID, backupUUID)
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != checksum {
		t.Fatalf("checksum mismatch: reported %s, actual %s", checksum, hex.EncodeToString(sum[:]))
	}

	restoreDir := t.TempDir()
	if err := Restore(restoreDir, backupDir, serverUUID, backupUUID); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	mustExist(t, filepath.Join(restoreDir, "world", "level.dat"), "level-data")
	mustExist(t, filepath.Join(restoreDir, "server.properties"), "motd=hello")
	mustNotExist(t, filepath.Join(restoreDir, "server.log"))
	mustNotExist(t, filepath.Join(restoreDir, "cache"))

	if err := Delete(backupDir, serverUUID, backupUUID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("expected archive to be gone after Delete, err=%v", err)
	}
}

func TestRestoreRejectsPathTraversal(t *testing.T) {
	backupDir := t.TempDir()
	serverUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	backupUUID := "99999999-8888-7777-6666-555555555555"

	archive := archivePath(backupDir, serverUUID, backupUUID)
	if err := os.MkdirAll(filepath.Dir(archive), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	payload := []byte("pwned")
	if err := tw.WriteHeader(&tar.Header{
		Name: "../../../../etc/passwd", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0644,
	}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	tw.Close()
	gz.Close()
	f.Close()

	restoreDir := t.TempDir()
	if err := Restore(restoreDir, backupDir, serverUUID, backupUUID); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	root := filepath.VolumeName(restoreDir) + string(filepath.Separator)
	if _, statErr := os.Stat(filepath.Join(root, "etc", "passwd")); statErr == nil {
		t.Fatalf("path traversal escaped to the real filesystem root at %s", filepath.Join(root, "etc", "passwd"))
	}
	if _, statErr := os.Stat(filepath.Join(restoreDir, "etc", "passwd")); statErr != nil {
		t.Fatalf("expected the traversal entry to be safely contained at restoreDir/etc/passwd, got: %v", statErr)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustExist(t *testing.T, path, wantContent string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if string(data) != wantContent {
		t.Fatalf("%s: got %q, want %q", path, data, wantContent)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to not exist, err=%v", path, err)
	}
}
