package romlibrary

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"app.ddd/internal/domain/rom"
)

// checksum is one digest computed while the bytes stream and the value the
// source published for it.
type checksum struct {
	algorithm string
	digest    hash.Hash
	expected  string
}

// checksums holds the digests of one download.
type checksums struct {
	entries []*checksum
}

// newChecksums returns the digests the source published for the file.
func newChecksums(file rom.File) *checksums {
	c := &checksums{}
	if file.MD5 != "" {
		c.entries = append(c.entries, &checksum{algorithm: "md5", digest: md5.New(), expected: file.MD5})
	}
	if file.SHA1 != "" {
		c.entries = append(c.entries, &checksum{algorithm: "sha1", digest: sha1.New(), expected: file.SHA1})
	}
	if file.SHA256 != "" {
		c.entries = append(c.entries, &checksum{algorithm: "sha256", digest: sha256.New(), expected: file.SHA256})
	}
	return c
}

// Write feeds the bytes into every digest.
func (c *checksums) Write(b []byte) (int, error) {
	for _, e := range c.entries {
		e.digest.Write(b)
	}
	return len(b), nil
}

// mismatch returns the first digest that differs from the published value.
func (c *checksums) mismatch() (algorithm, expected, actual string, found bool) {
	for _, e := range c.entries {
		actual := hex.EncodeToString(e.digest.Sum(nil))
		if !strings.EqualFold(actual, e.expected) {
			return e.algorithm, e.expected, actual, true
		}
	}
	return "", "", "", false
}

// verify reports the first digest that differs from the published value.
func (c *checksums) verify() error {
	algorithm, expected, actual, found := c.mismatch()
	if !found {
		return nil
	}
	return notRetryable(fmt.Errorf("romcatalog: %s checksum mismatch: expected %s, got %s", algorithm, expected, actual))
}

// hashFile feeds the bytes of an existing file into the digests, stopping
// when the context is done.
func (c *checksums) hashFile(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("romcatalog: open %s for checksum: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := copyToDigests(ctx, c, f); err != nil {
		return fmt.Errorf("romcatalog: checksum %s: %w", path, err)
	}
	return nil
}

// copyToDigests copies src into the digests, checking the context between
// chunks so a canceled download stops reading.
func copyToDigests(ctx context.Context, c *checksums, src io.Reader) (int64, error) {
	buf := make([]byte, copyChunkSize)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return read, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := c.Write(buf[:n]); err != nil {
				return read, err
			}
			read += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			return read, nil
		}
		if readErr != nil {
			return read, readErr
		}
	}
}
