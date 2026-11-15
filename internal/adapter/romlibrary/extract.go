package romlibrary

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"app.ddd/internal/domain/rom"
)

func requireExtractor(archiveExt, sevenZip string) error {
	if strings.ToLower(archiveExt) != ".7z" {
		return nil
	}
	if _, err := exec.LookPath(sevenZip); err != nil {
		return fmt.Errorf("romcatalog: extracting .7z archives needs the 7z binary %q, which is not available: %w", sevenZip, err)
	}
	return nil
}

// extractROM unpacks the archive into workDir and returns the path of the ROM
// it contains together with that ROM's extension.
func extractROM(ctx context.Context, sevenZip, archiveExt, archivePath, workDir string, exts []string) (string, string, error) {
	switch strings.ToLower(archiveExt) {
	case ".zip":
		return extractZipROM(archivePath, workDir, exts)
	case ".7z":
		return extract7zROM(ctx, sevenZip, archivePath, workDir, exts)
	}
	return "", "", fmt.Errorf("romcatalog: unsupported archive format %q", archiveExt)
}

func extractZipROM(archivePath, workDir string, exts []string) (string, string, error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("romcatalog: open zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	var names []string
	var entries []*zip.File
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		names = append(names, f.Name)
		entries = append(entries, f)
	}
	i, ok := rom.PickROM(names, exts)
	if !ok {
		return "", "", rom.NoROMError(exts)
	}
	ext := path.Ext(entries[i].Name)
	romPath := filepath.Join(workDir, "rom"+ext)
	if err := copyZipEntry(entries[i], romPath); err != nil {
		return "", "", err
	}
	return romPath, ext, nil
}

func copyZipEntry(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("romcatalog: open %s in archive: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("romcatalog: create %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, rc)
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("romcatalog: extract %s: %w", f.Name, copyErr)
	}
	return nil
}

func extract7zROM(ctx context.Context, sevenZip, archivePath, workDir string, exts []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, sevenZip, "x", "-y", "-o"+workDir, archivePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("romcatalog: 7z extraction of %s failed: %w: %s", filepath.Base(archivePath), err, strings.TrimSpace(string(out)))
	}

	var rels, abs []string
	err := filepath.WalkDir(workDir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(workDir, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		abs = append(abs, p)
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("romcatalog: scan extracted files: %w", err)
	}
	i, ok := rom.PickROM(rels, exts)
	if !ok {
		return "", "", rom.NoROMError(exts)
	}
	return abs[i], path.Ext(rels[i]), nil
}
