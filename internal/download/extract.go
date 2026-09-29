package download

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const (
	FormatTarGzip          Format = "tar.gz"
	FormatZip              Format = "zip"
	defaultMaxArchiveBytes int64  = 256 << 20
)

// Format identifies a supported archive container.
type Format string

var drivePrefix = regexp.MustCompile(`^[A-Za-z]:`)

// Limits bound both archive metadata and decompressed content. MaxArchiveBytes
// defaults to 256 MiB when omitted.
type Limits struct {
	MaxEntries      int64
	MaxBytes        int64
	MaxFileBytes    int64
	MaxArchiveBytes int64
}

type archiveEntry struct {
	name    string
	kind    byte
	size    int64
	mode    os.FileMode
	link    string
	zipFile *zip.File
}

const (
	entryFile byte = iota + 1
	entryDir
	entrySymlink
	entryHardlink
)

// Extract validates a zip or gzip-compressed tar archive, stages its contents
// beside target, then renames the completed tree into place. target must not
// already exist and its parent must already exist as a directory.
func Extract(ctx context.Context, archivePath, target string, format Format, limits Limits) error {
	if ctx == nil {
		return errors.New("archive context is nil")
	}
	if archivePath == "" || target == "" {
		return errors.New("archive and target paths are required")
	}
	if limits.MaxEntries <= 0 || limits.MaxBytes <= 0 || limits.MaxFileBytes <= 0 {
		return errors.New("archive entry and decompression limits must be positive")
	}
	if limits.MaxEntries > 1_000_000 || limits.MaxBytes > maxSupportedBytes || limits.MaxFileBytes > maxSupportedBytes {
		return errors.New("archive limits exceed supported bounds")
	}
	if limits.MaxFileBytes > limits.MaxBytes {
		return errors.New("per-file limit exceeds total decompression limit")
	}
	if limits.MaxArchiveBytes <= 0 {
		limits.MaxArchiveBytes = defaultMaxArchiveBytes
	}
	if limits.MaxArchiveBytes > maxSupportedBytes {
		return errors.New("compressed archive limit exceeds supported bound")
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve extraction target: %w", err)
	}
	parent := filepath.Dir(absoluteTarget)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve extraction target parent: %w", err)
	}
	parent = resolvedParent
	absoluteTarget = filepath.Join(parent, filepath.Base(absoluteTarget))
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect extraction target parent: %w", err)
	}
	if !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("extraction target parent must be a real directory")
	}
	if _, err := os.Lstat(absoluteTarget); err == nil {
		return errors.New("extraction target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect extraction target: %w", err)
	}

	archiveInfo, err := os.Lstat(archivePath)
	if err != nil {
		return fmt.Errorf("inspect archive path: %w", err)
	}
	if !archiveInfo.Mode().IsRegular() {
		return errors.New("archive must be a regular non-symlink file")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect archive: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(archiveInfo, info) {
		return errors.New("archive must remain the same regular file during open")
	}
	if info.Size() > limits.MaxArchiveBytes {
		return errors.New("compressed archive exceeds configured limit")
	}

	var entries []archiveEntry
	switch format {
	case FormatZip:
		entries, err = validateZip(ctx, file, info.Size(), limits)
	case FormatTarGzip:
		entries, err = validateTarGzip(ctx, file, limits)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
	if err != nil {
		return fmt.Errorf("validate archive: %w", err)
	}
	if err := validateEntryTree(entries); err != nil {
		return fmt.Errorf("validate archive paths: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".anza-extract-*")
	if err != nil {
		return fmt.Errorf("create extraction staging directory: %w", err)
	}
	if err := os.Chmod(stage, 0700); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("protect extraction staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	switch format {
	case FormatZip:
		err = extractZip(ctx, entries, stage, limits)
	case FormatTarGzip:
		err = extractTarGzip(ctx, file, entries, stage, limits)
	}
	if err != nil {
		return fmt.Errorf("stage archive contents: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(absoluteTarget); err == nil {
		return errors.New("extraction target appeared during staging")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("recheck extraction target: %w", err)
	}
	if err := os.Rename(stage, absoluteTarget); err != nil {
		return fmt.Errorf("publish extracted tree: %w", err)
	}
	return nil
}

func validateZip(ctx context.Context, file *os.File, size int64, limits Limits) ([]archiveEntry, error) {
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, err
	}
	if int64(len(reader.File)) > limits.MaxEntries {
		return nil, errors.New("archive has too many entries")
	}
	entries := make([]archiveEntry, 0, len(reader.File))
	var total int64
	for _, item := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name, err := cleanArchivePath(item.Name, item.FileInfo().IsDir())
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", item.Name, err)
		}
		mode := item.Mode()
		entry := archiveEntry{name: name, mode: safeMode(mode), zipFile: item}
		switch {
		case mode&os.ModeSymlink != 0:
			entry.kind = entrySymlink
			if item.UncompressedSize64 > uint64(limits.MaxFileBytes) {
				return nil, errors.New("symlink target exceeds per-file limit")
			}
			r, err := item.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: r}, limits.MaxFileBytes+1))
			closeErr := r.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if int64(len(data)) > limits.MaxFileBytes {
				return nil, errors.New("symlink target exceeds per-file limit")
			}
			_, err = cleanLinkTarget(path.Dir(name), string(data), false)
			if err != nil {
				return nil, fmt.Errorf("entry %q symlink: %w", name, err)
			}
			entry.link = string(data)
			total += int64(len(data))
			if total > limits.MaxBytes {
				return nil, errors.New("archive exceeds decompressed size limit")
			}
		case item.FileInfo().IsDir():
			entry.kind = entryDir
			if item.UncompressedSize64 != 0 {
				return nil, fmt.Errorf("directory entry %q has content", name)
			}
		case mode.Type() == 0:
			entry.kind = entryFile
			if item.UncompressedSize64 > uint64(limits.MaxFileBytes) {
				return nil, fmt.Errorf("entry %q exceeds per-file limit", name)
			}
			if item.UncompressedSize64 > uint64(limits.MaxBytes-total) {
				return nil, errors.New("archive exceeds decompressed size limit")
			}
			entry.size = int64(item.UncompressedSize64)
			total += entry.size
		default:
			return nil, fmt.Errorf("entry %q has an unsupported special file type", name)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func validateTarGzip(ctx context.Context, file *os.File, limits Limits) ([]archiveEntry, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	maxStream := limits.MaxBytes + limits.MaxEntries*1024 + 1024
	bounded := &limitReader{reader: contextReader{ctx: ctx, reader: gz}, limit: maxStream}
	reader := tar.NewReader(bounded)
	entries := make([]archiveEntry, 0)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			_ = gz.Close()
			return nil, err
		}
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = gz.Close()
			return nil, nextErr
		}
		if int64(len(entries)) >= limits.MaxEntries {
			_ = gz.Close()
			return nil, errors.New("archive has too many entries")
		}
		name, err := cleanArchivePath(header.Name, header.Typeflag == tar.TypeDir)
		if err != nil {
			_ = gz.Close()
			return nil, fmt.Errorf("entry %q: %w", header.Name, err)
		}
		entry := archiveEntry{name: name, size: header.Size, mode: safeMode(os.FileMode(header.Mode))}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			entry.kind = entryFile
			if header.Size < 0 || header.Size > limits.MaxFileBytes || header.Size > limits.MaxBytes-total {
				_ = gz.Close()
				return nil, fmt.Errorf("entry %q exceeds decompression limit", name)
			}
			total += header.Size
		case tar.TypeDir:
			entry.kind = entryDir
			if header.Size != 0 {
				_ = gz.Close()
				return nil, fmt.Errorf("directory entry %q has content", name)
			}
		case tar.TypeSymlink:
			entry.kind = entrySymlink
			if header.Size != 0 {
				_ = gz.Close()
				return nil, fmt.Errorf("symlink entry %q unexpectedly contains file data", name)
			}
			_, err = cleanLinkTarget(path.Dir(name), header.Linkname, false)
			if err != nil {
				_ = gz.Close()
				return nil, fmt.Errorf("entry %q symlink: %w", name, err)
			}
			entry.link = header.Linkname
		case tar.TypeLink:
			entry.kind = entryHardlink
			if header.Size != 0 {
				_ = gz.Close()
				return nil, fmt.Errorf("hardlink entry %q unexpectedly contains file data", name)
			}
			entry.link, err = cleanLinkTarget(".", header.Linkname, true)
			if err != nil {
				_ = gz.Close()
				return nil, fmt.Errorf("entry %q hardlink: %w", name, err)
			}
		default:
			_ = gz.Close()
			return nil, fmt.Errorf("entry %q has an unsupported tar type %d", name, header.Typeflag)
		}
		entries = append(entries, entry)
	}
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if bounded.exceeded {
		_ = gz.Close()
		return nil, errors.New("decompressed archive stream exceeds limit")
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if int64(len(entries)) > limits.MaxEntries {
		return nil, errors.New("archive has too many entries")
	}
	return entries, nil
}

func validateEntryTree(entries []archiveEntry) error {
	typePath := make(map[string]byte, len(entries))
	explicit := make(map[string]byte, len(entries))
	casePaths := make(map[string]string, len(entries)*2)
	for _, entry := range entries {
		if _, exists := explicit[entry.name]; exists {
			return fmt.Errorf("duplicate archive target %q", entry.name)
		}
		explicit[entry.name] = entry.kind
		parts := strings.Split(entry.name, "/")
		for i := range parts {
			actual := strings.Join(parts[:i+1], "/")
			folded := strings.ToLower(actual)
			if earlier, exists := casePaths[folded]; exists && earlier != actual {
				return fmt.Errorf("case-fold target collision between %q and %q", earlier, actual)
			}
			casePaths[folded] = actual
			if i < len(parts)-1 {
				if kind, exists := explicit[actual]; exists && kind != entryDir {
					return fmt.Errorf("archive file %q is a parent of another target", actual)
				}
				typePath[actual] = entryDir
			}
		}
		if prior, exists := typePath[entry.name]; exists && prior == entryDir && entry.kind != entryDir {
			return fmt.Errorf("archive target %q conflicts with a directory path", entry.name)
		}
		typePath[entry.name] = entry.kind
	}
	for _, entry := range entries {
		if entry.kind == entryHardlink {
			if explicit[entry.link] != entryFile {
				return fmt.Errorf("hardlink %q does not name an archive regular file", entry.name)
			}
		}
	}
	return nil
}

func cleanArchivePath(name string, directory bool) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, `\`) || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || drivePrefix.MatchString(name) {
		return "", errors.New("absolute, drive, UNC, or malformed archive path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errors.New("archive path contains a parent traversal component")
		}
	}
	cleaned := path.Clean(name)
	if cleaned == "." && directory {
		return ".", nil
	}
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("archive path escapes extraction root")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if err := validateArchiveComponent(part); err != nil {
			return "", errors.New("archive path contains an unsafe component")
		}
	}
	return cleaned, nil
}

func cleanLinkTarget(parent, target string, hardlink bool) (string, error) {
	if target == "" || strings.ContainsRune(target, '\x00') || strings.Contains(target, `\`) || strings.Contains(target, ":") || strings.HasPrefix(target, "/") || drivePrefix.MatchString(target) {
		return "", errors.New("link target is absolute or malformed")
	}
	if hardlink {
		parent = "."
	}
	cleaned := path.Clean(path.Join(parent, target))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("link target escapes extraction root")
	}
	for _, part := range strings.Split(cleaned, "/") {
		if err := validateArchiveComponent(part); err != nil {
			return "", errors.New("link target contains an unsafe component")
		}
	}
	return cleaned, nil
}

func validateArchiveComponent(part string) error {
	if part == "" || part == "." || part == ".." || strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") || strings.ContainsAny(part, `<>"|?*`) {
		return errors.New("invalid archive path component")
	}
	for _, char := range part {
		if unicode.IsControl(char) || unicode.Is(unicode.Mn, char) {
			return errors.New("archive path component uses unsupported control or combining characters")
		}
	}
	base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return errors.New("archive path component is a reserved Windows device name")
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return errors.New("archive path component is a reserved Windows device name")
	}
	return nil
}

func safeMode(mode os.FileMode) os.FileMode {
	if mode&0111 != 0 {
		return 0755
	}
	return 0644
}

func extractZip(ctx context.Context, entries []archiveEntry, stage string, limits Limits) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.kind == entryDir {
			if err := os.MkdirAll(filepath.Join(stage, filepath.FromSlash(entry.name)), 0755); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		if entry.kind != entryFile {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeZipEntry(ctx, stage, entry, limits); err != nil {
			return err
		}
	}
	return createLinks(stage, entries)
}

func writeZipEntry(ctx context.Context, stage string, entry archiveEntry, limits Limits) error {
	if entry.zipFile == nil || entry.size > limits.MaxFileBytes || entry.size > limits.MaxBytes {
		return errors.New("invalid zip file entry")
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(stage, filepath.FromSlash(entry.name))), 0755); err != nil {
		return err
	}
	reader, err := entry.zipFile.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	return writeRegular(ctx, stage, entry.name, entry.mode, entry.size, reader, limits.MaxFileBytes)
}

func extractTarGzip(ctx context.Context, file *os.File, entries []archiveEntry, stage string, limits Limits) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(contextReader{ctx: ctx, reader: gz})
	index := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		entryName, nameErr := cleanArchivePath(header.Name, header.Typeflag == tar.TypeDir)
		if nameErr != nil || index >= len(entries) || entryName != entries[index].name {
			return errors.New("archive changed between validation and extraction")
		}
		entry := entries[index]
		index++
		switch entry.kind {
		case entryDir:
			if err := os.MkdirAll(filepath.Join(stage, filepath.FromSlash(entry.name)), 0755); err != nil {
				return err
			}
		case entryFile:
			if err := os.MkdirAll(filepath.Dir(filepath.Join(stage, filepath.FromSlash(entry.name))), 0755); err != nil {
				return err
			}
			if err := writeRegular(ctx, stage, entry.name, entry.mode, entry.size, reader, limits.MaxFileBytes); err != nil {
				return err
			}
		case entrySymlink, entryHardlink:
			if _, err := io.Copy(io.Discard, reader); err != nil {
				return err
			}
		}
	}
	if index != len(entries) {
		return errors.New("archive changed between validation and extraction")
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return createLinks(stage, entries)
}

func writeRegular(ctx context.Context, stage, name string, mode os.FileMode, expected int64, source io.Reader, maxFile int64) error {
	if expected < 0 || expected > maxFile {
		return errors.New("file exceeds extraction limit")
	}
	target := filepath.Join(stage, filepath.FromSlash(name))
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(contextReader{ctx: ctx, reader: source}, expected+1))
	if copyErr == nil && written != expected {
		copyErr = fmt.Errorf("archive entry %q size mismatch", name)
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	if copyErr == nil {
		copyErr = file.Chmod(mode)
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func createLinks(stage string, entries []archiveEntry) error {
	for _, entry := range entries {
		if entry.kind != entryHardlink {
			continue
		}
		source := filepath.Join(stage, filepath.FromSlash(entry.link))
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("hardlink %q source is not a regular staged file", entry.name)
		}
		target := filepath.Join(stage, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.Link(source, target); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if entry.kind != entrySymlink {
			continue
		}
		target := filepath.Join(stage, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.Symlink(filepath.FromSlash(entry.link), target); err != nil {
			return err
		}
	}
	return nil
}

type limitReader struct {
	reader   io.Reader
	limit    int64
	read     int64
	exceeded bool
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func (r *limitReader) Read(buffer []byte) (int, error) {
	if r.read > r.limit {
		r.exceeded = true
		return 0, errors.New("decompressed archive exceeds limit")
	}
	remaining := r.limit + 1 - r.read
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	n, err := r.reader.Read(buffer)
	r.read += int64(n)
	if r.read > r.limit {
		r.exceeded = true
		return n, errors.New("decompressed archive exceeds limit")
	}
	return n, err
}
