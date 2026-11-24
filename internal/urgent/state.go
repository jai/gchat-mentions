package urgent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// FileLock holds an exclusive non-blocking advisory flock.
type FileLock struct {
	file *os.File
}

// AcquireLock attempts a non-blocking exclusive flock on lockPath.
// Returns (nil, nil) if another process currently holds the lock.
func AcquireLock(lockPath string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil
		}
		return nil, fmt.Errorf("acquire flock: %w", err)
	}
	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	return &FileLock{file: f}, nil
}

// Release frees the lock and closes the file descriptor.
func (l *FileLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}

// readJSONFile loads JSON into dst; missing or corrupt files leave dst unchanged.
func readJSONFile(path string, dst any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, dst)
}

// atomicWriteJSON writes obj as compact JSON with mode 0600 atomically.
func atomicWriteJSON(path string, obj any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".tmp-*.json")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func appendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	return err
}

func pruneSeen(seen map[string]string, now time.Time, retention time.Duration) map[string]string {
	cutoff := now.Add(-retention)
	out := make(map[string]string, len(seen))
	for id, tsStr := range seen {
		ts, err := time.Parse(time.RFC3339, tsStr)
		if err != nil || !ts.Before(cutoff) {
			out[id] = tsStr
		}
	}
	return out
}

func pruneEscalations(entries []EscalationEntry, now time.Time) []EscalationEntry {
	cutoff := now.Add(-72 * time.Hour)
	out := make([]EscalationEntry, 0, len(entries))
	for _, e := range entries {
		ts, err := time.Parse(time.RFC3339, e.TSUTC)
		if err == nil && !ts.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}
