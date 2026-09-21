package runner

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxEntrySize bounds a single file unpacked from the archive. GitHub's runner
// archive is a few hundred megabytes in total and its largest file is well
// under this, so the limit only ever catches a malformed or hostile archive.
const maxEntrySize = 2 << 30 // 2 GiB

// Extract unpacks a runner release into dest.
//
// GitHub ships a gzip-compressed tar for Linux and macOS and a zip for
// Windows, so which one this is comes from the name GitHub gave it rather
// than from the platform Runnerly is running on — a release is downloaded
// by name and the two must agree.
//
// Every entry is resolved against dest and rejected if it escapes, including
// symlink targets. An archive that unpacks outside the directory it was told
// to use is treated as hostile, not as a quirk to work around.
func Extract(archive, dest string) error {
	if strings.HasSuffix(strings.ToLower(archive), ".zip") {
		return extractZip(archive, dest)
	}
	return extractTarGz(archive, dest)
}

func extractTarGz(archive, dest string) error {
	file, err := os.Open(archive) //nolint:gosec // archive is a path Runnerly just wrote
	if err != nil {
		return fmt.Errorf("open %s: %w", archive, err)
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read %s as gzip: %w", archive, err)
	}
	defer func() { _ = gz.Close() }()

	root, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dest, err)
	}

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", archive, err)
		}

		target, err := safeJoin(root, header.Name)
		if err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, entryMode(header, 0o750)); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}

		case tar.TypeReg:
			if header.Size > maxEntrySize {
				return fmt.Errorf("%s: entry %s is %d bytes, which is implausible for a runner release",
					archive, header.Name, header.Size)
			}
			if err := writeFile(target, reader, entryMode(header, 0o600), header.Size); err != nil {
				return err
			}

		case tar.TypeSymlink:
			if err := writeSymlink(root, target, header.Linkname); err != nil {
				return err
			}

		case tar.TypeLink:
			source, err := safeJoin(root, header.Linkname)
			if err != nil {
				return fmt.Errorf("%s: hard link %s: %w", archive, header.Name, err)
			}
			_ = os.Remove(target)
			if err := os.Link(source, target); err != nil {
				return fmt.Errorf("link %s: %w", target, err)
			}

		default:
			// Character devices, FIFOs and the like have no business in a
			// runner release. Skipping is safer than creating them.
			continue
		}
	}
}

// safeJoin resolves name inside root and refuses anything that escapes it.
func safeJoin(root, name string) (string, error) {
	if name == "" {
		return "", errors.New("archive contains an entry with no name")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive entry %q is an absolute path", name)
	}

	target := filepath.Join(root, filepath.FromSlash(name))
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q would be written outside %s", name, root)
	}
	return target, nil
}

// writeFile creates one file from the archive, preserving its mode so that
// config.sh and run.sh stay executable.
func writeFile(target string, r io.Reader, mode os.FileMode, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create directory for %s: %w", target, err)
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // target was checked by safeJoin
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}

	// LimitReader bounds what a lying header can write, on top of the size
	// check the caller already made.
	_, copyErr := io.Copy(f, io.LimitReader(r, size))
	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", target, copyErr)
	}
	// O_CREATE only applies the mode to a new file; an existing one keeps its
	// own, so set it explicitly.
	if err := os.Chmod(target, mode); err != nil {
		return fmt.Errorf("set mode on %s: %w", target, err)
	}
	return nil
}

// writeSymlink creates a symlink, refusing one that points outside root.
func writeSymlink(root, target, link string) error {
	resolved := link
	if !filepath.IsAbs(link) {
		resolved = filepath.Join(filepath.Dir(target), link)
	}
	resolved = filepath.Clean(resolved)
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return fmt.Errorf("symlink %s points to %s, outside %s", target, link, root)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create directory for %s: %w", target, err)
	}
	_ = os.Remove(target)
	if err := os.Symlink(link, target); err != nil {
		return fmt.Errorf("create symlink %s: %w", target, err)
	}
	return nil
}

// entryMode keeps the archive's permission bits but never grants group or
// other write access, and never sets setuid or setgid.
func entryMode(h *tar.Header, fallback os.FileMode) os.FileMode {
	mode := os.FileMode(h.Mode).Perm() //nolint:gosec // Perm() masks to permission bits
	if mode == 0 {
		return fallback
	}
	return mode &^ 0o022
}

// extractZip unpacks a zip archive into dest, with the same guarantees
// extractTarGz gives.
//
// Zip has no symlink or hard-link entry type of its own — the unix
// extensions encode one in the mode bits — and the runner's Windows
// release contains neither. One is refused rather than followed: a link
// nobody expects in an archive nobody signs is not something to be
// creative about.
func extractZip(archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("read %s as zip: %w", archive, err)
	}
	defer func() { _ = reader.Close() }()

	root, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dest, err)
	}

	for _, entry := range reader.File {
		target, err := safeJoin(root, entry.Name)
		if err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}

		mode := entry.Mode()
		switch {
		case entry.FileInfo().IsDir():
			if err := os.MkdirAll(target, zipMode(mode, 0o750)); err != nil {
				return fmt.Errorf("create directory %s: %w", target, err)
			}

		case mode&os.ModeSymlink != 0:
			return fmt.Errorf("%s: entry %s is a symlink, which a runner release does not contain",
				archive, entry.Name)

		case mode.IsRegular():
			size := entry.FileInfo().Size()
			if size > maxEntrySize {
				return fmt.Errorf("%s: entry %s is %d bytes, which is implausible for a runner release",
					archive, entry.Name, size)
			}
			if err := copyZipEntry(entry, target, zipMode(mode, 0o600), size); err != nil {
				return err
			}

		default:
			// Devices and the like have no business in a runner release.
			continue
		}
	}
	return nil
}

func copyZipEntry(entry *zip.File, target string, mode os.FileMode, size int64) error {
	r, err := entry.Open()
	if err != nil {
		return fmt.Errorf("read %s from the archive: %w", entry.Name, err)
	}
	defer func() { _ = r.Close() }()

	return writeFile(target, r, mode, size)
}

// zipMode keeps a zip's recorded permissions when it has any, with the
// same group and other write bits stripped that a tar entry gets.
//
// A zip written on Windows records no unix mode at all, which arrives here
// as zero and would otherwise create files nobody can read. The runner's
// own release does carry modes, so its scripts stay executable; the
// fallback is for everything else.
func zipMode(mode os.FileMode, fallback os.FileMode) os.FileMode {
	perm := mode.Perm()
	if perm == 0 {
		return fallback
	}
	return perm &^ 0o022
}
