package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func archivePath(backupDir, serverUUID, backupUUID string) string {
	return filepath.Join(backupDir, serverUUID, backupUUID+".tar.gz")
}

func matchesIgnored(relPath string, patterns []string) bool {
	base := filepath.Base(relPath)
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if ok, _ := filepath.Match(p, relPath); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

func Create(serverDir, backupDir, serverUUID, backupUUID string, ignoredFiles []string) (bytesWritten int64, checksum string, err error) {
	dest := archivePath(backupDir, serverUUID, backupUUID)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return 0, "", err
	}

	f, err := os.Create(dest)
	if err != nil {
		return 0, "", err
	}

	hasher := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(f, hasher))
	tw := tar.NewWriter(gz)

	walkErr := filepath.WalkDir(serverDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(serverDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if matchesIgnored(rel, ignoredFiles) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if d.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(tw, src)
		return err
	})

	if walkErr != nil {
		tw.Close()
		gz.Close()
		f.Close()
		os.Remove(dest)
		return 0, "", walkErr
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		f.Close()
		os.Remove(dest)
		return 0, "", err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		os.Remove(dest)
		return 0, "", err
	}

	info, err := f.Stat()
	closeErr := f.Close()
	if err != nil {
		return 0, "", err
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	return info.Size(), hex.EncodeToString(hasher.Sum(nil)), nil
}

func Restore(serverDir, backupDir, serverUUID, backupUUID string) error {
	src := archivePath(backupDir, serverUUID, backupUUID)
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		cleaned := filepath.Clean("/" + header.Name)
		target := filepath.Join(serverDir, cleaned)
		rel, err := filepath.Rel(serverDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("backup archive contains an unsafe path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
}

func Delete(backupDir, serverUUID, backupUUID string) error {
	return os.Remove(archivePath(backupDir, serverUUID, backupUUID))
}

func Open(backupDir, serverUUID, backupUUID string) (*os.File, error) {
	return os.Open(archivePath(backupDir, serverUUID, backupUUID))
}
