package romlibrary

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"app.ddd/internal/domain/rom"
)

type bundleMember struct {
	name   string
	digest [sha256.Size]byte
	index  bool
}

func (l *Library) PrepareGameBundle(ctx context.Context, job rom.Job) (rom.GameBundle, error) {
	if !installedPlayStationJob(job) {
		return rom.GameBundle{}, rom.ErrBundleUnavailable
	}
	root, err := os.OpenRoot(l.Root)
	if err != nil {
		return rom.GameBundle{}, err
	}
	defer func() { _ = root.Close() }()
	indexes := make(map[string][sha256.Size]byte)
	report := inspectInstalledGame(ctx, root, job, indexes)
	if !strings.EqualFold(path.Ext(report.EntryPoint), ".m3u") ||
		(report.Status != rom.ReadinessComplete && (report.Status != rom.ReadinessUnknown || report.Reason != undeclaredDiscCount)) {
		return rom.GameBundle{}, rom.ErrBundleUnavailable
	}
	dir := path.Dir(report.EntryPoint)
	bundleRoot, err := root.OpenRoot(dir)
	if err != nil {
		return rom.GameBundle{}, rom.ErrBundleUnavailable
	}
	defer func() { _ = bundleRoot.Close() }()
	members := make([]bundleMember, 0, len(report.Files))
	names := make(map[string]bool, len(report.Files))
	for _, file := range report.Files {
		name, ok := relativeTo(dir, file)
		if !ok || !portableBundleName(name) || names[strings.ToLower(name)] {
			return rom.GameBundle{}, rom.ErrBundleUnavailable
		}
		info, err := bundleRoot.Stat(name)
		if err != nil || !info.Mode().IsRegular() {
			return rom.GameBundle{}, rom.ErrBundleUnavailable
		}
		names[strings.ToLower(name)] = true
		digest, index := indexes[file]
		members = append(members, bundleMember{name: name, digest: digest, index: index})
	}
	base := path.Base(report.EntryPoint)
	return rom.GameBundle{
		Name: strings.TrimSuffix(base, path.Ext(base)) + ".zip",
		WriteZIP: func(ctx context.Context, dst io.Writer) error {
			return writeBundleZIP(ctx, dst, l.Root, dir, members)
		},
	}, nil
}

func portableBundleName(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}

func writeBundleZIP(ctx context.Context, dst io.Writer, libraryPath, dir string, members []bundleMember) error {
	root, err := os.OpenRoot(libraryPath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	bundleDir, err := root.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = bundleDir.Close() }()
	archive := zip.NewWriter(dst)
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeBundleMember(ctx, archive, bundleDir, member); err != nil {
			return err
		}
	}
	return archive.Close()
}

func writeBundleMember(ctx context.Context, archive *zip.Writer, root *os.Root, member bundleMember) error {
	info, err := root.Stat(member.name)
	if err != nil || !info.Mode().IsRegular() {
		return rom.ErrBundleUnavailable
	}
	file, err := root.Open(member.name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return rom.ErrBundleUnavailable
	}
	if member.index {
		body, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
		if err != nil || len(body) > maxIndexBytes || sha256.Sum256(body) != member.digest {
			return rom.ErrBundleUnavailable
		}
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: member.name, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = entry.Write(body)
		return err
	}
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: member.name, Method: zip.Store})
	if err != nil {
		return err
	}
	n, err := copyCapped(ctx, entry, file, info.Size())
	if err != nil {
		return err
	}
	if n != info.Size() {
		return fmt.Errorf("copy game bundle: %w", io.ErrUnexpectedEOF)
	}
	return nil
}
