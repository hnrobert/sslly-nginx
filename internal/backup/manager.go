package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu sync.Mutex

	backupRoot string
	configDir  string
	sslDir     string
	runtimeDir string
	nginxConf  string
}

type state struct {
	LastGood     string `json:"lastGood"`
	InProgress   string `json:"inProgress"`
	LastGoodAt   string `json:"lastGoodAt,omitempty"`
	InProgressAt string `json:"inProgressAt,omitempty"`
}

func NewManager(backupRoot, configDir, sslDir, runtimeDir, nginxConf string) (*Manager, error) {
	absBackupRoot, err := filepath.Abs(backupRoot)
	if err != nil {
		return nil, fmt.Errorf("backup root abs: %w", err)
	}
	absConfigDir, err := filepath.Abs(configDir)
	if err != nil {
		return nil, fmt.Errorf("config dir abs: %w", err)
	}
	absSSLDir, err := filepath.Abs(sslDir)
	if err != nil {
		return nil, fmt.Errorf("ssl dir abs: %w", err)
	}
	absRuntimeDir, err := filepath.Abs(runtimeDir)
	if err != nil {
		return nil, fmt.Errorf("runtime dir abs: %w", err)
	}

	m := &Manager{
		backupRoot: absBackupRoot,
		configDir:  absConfigDir,
		sslDir:     absSSLDir,
		runtimeDir: absRuntimeDir,
		nginxConf:  nginxConf,
	}
	if err := os.MkdirAll(m.snapshotsDir(), 0755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	return m, nil
}

// DefaultBackupRoot stores backups under the mounted config directory.
func DefaultBackupRoot(configDir string) string {
	return filepath.Join(configDir, ".sslly-backups")
}

func (m *Manager) snapshotsDir() string {
	return filepath.Join(m.backupRoot, "snapshots")
}

func (m *Manager) statePath() string {
	return filepath.Join(m.backupRoot, "state.json")
}

func (m *Manager) snapshotPath(id string) string {
	return filepath.Join(m.snapshotsDir(), id)
}

// Begin marks a new snapshot attempt as in-progress.
// If the process crashes before Commit/Abort clears it, the next start can rollback.
func (m *Manager) Begin() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, err := m.readStateLocked()
	if err != nil {
		return "", err
	}

	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.MkdirAll(m.snapshotPath(id), 0755); err != nil {
		return "", fmt.Errorf("create snapshot dir: %w", err)
	}

	st.InProgress = id
	st.InProgressAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := m.writeStateLocked(st); err != nil {
		return "", err
	}
	return id, nil
}

// Abort clears in-progress state and removes the snapshot directory (best-effort).
func (m *Manager) Abort(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, err := m.readStateLocked()
	if err != nil {
		return err
	}
	if st.InProgress == id {
		st.InProgress = ""
		st.InProgressAt = ""
		if err := m.writeStateLocked(st); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(m.snapshotPath(id))
	return nil
}

// Commit captures the current runtime configuration into the snapshot,
// then promotes it to last-good and clears the in-progress marker.
func (m *Manager) Commit(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, err := m.readStateLocked()
	if err != nil {
		return err
	}
	if st.InProgress != id {
		return fmt.Errorf("snapshot %s is not in progress", id)
	}

	snapDir := m.snapshotPath(id)
	cfgDst := filepath.Join(snapDir, "configs")
	sslDst := filepath.Join(snapDir, "ssl")
	runtimeDst := filepath.Join(snapDir, "runtime")
	nginxDst := filepath.Join(snapDir, "nginx", "nginx.conf")

	_ = os.RemoveAll(cfgDst)
	_ = os.RemoveAll(sslDst)
	_ = os.RemoveAll(runtimeDst)
	_ = os.RemoveAll(filepath.Dir(nginxDst))

	if err := copyDir(m.configDir, cfgDst, func(srcPath string, d os.DirEntry) bool {
		// Avoid snapshot recursion when backup root lives inside config dir.
		cleanSrc := filepath.Clean(srcPath)
		cleanBackup := filepath.Clean(m.backupRoot)
		if cleanSrc == cleanBackup {
			return true
		}
		if isUnder(cleanSrc, cleanBackup) {
			return true
		}
		return false
	}); err != nil {
		return fmt.Errorf("copy configs: %w", err)
	}
	if err := copyDir(m.sslDir, sslDst, nil); err != nil {
		return fmt.Errorf("copy ssl: %w", err)
	}
	if err := copyDir(m.runtimeDir, runtimeDst, nil); err != nil {
		// runtime dir may not exist on first run
		if !os.IsNotExist(err) {
			return fmt.Errorf("copy runtime: %w", err)
		}
	}
	if m.nginxConf != "" {
		if err := copyFile(m.nginxConf, nginxDst); err != nil {
			// nginx.conf might not exist on first run; treat as non-fatal.
			if !os.IsNotExist(err) {
				return fmt.Errorf("copy nginx conf: %w", err)
			}
		}
	}

	st.LastGood = id
	st.LastGoodAt = time.Now().UTC().Format(time.RFC3339Nano)
	st.InProgress = ""
	st.InProgressAt = ""
	return m.writeStateLocked(st)
}

// MaybeRestoreAfterCrash restores last-good when it detects a previous crash mid-reload.
// It returns true if a restore happened.
func (m *Manager) MaybeRestoreAfterCrash() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, err := m.readStateLocked()
	if err != nil {
		return false, err
	}
	if st.InProgress == "" {
		return false, nil
	}
	if st.LastGood == "" {
		// Nothing to restore to; clear marker to avoid loops.
		st.InProgress = ""
		st.InProgressAt = ""
		return false, m.writeStateLocked(st)
	}

	if err := m.restoreSnapshotLocked(st.LastGood); err != nil {
		return false, err
	}
	st.InProgress = ""
	st.InProgressAt = ""
	if err := m.writeStateLocked(st); err != nil {
		return false, err
	}
	return true, nil
}

func (m *Manager) RestoreLastGood() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	st, err := m.readStateLocked()
	if err != nil {
		return err
	}
	if st.LastGood == "" {
		return fmt.Errorf("no last-good snapshot available")
	}
	return m.restoreSnapshotLocked(st.LastGood)
}

func (m *Manager) restoreSnapshotLocked(id string) error {
	snapDir := m.snapshotPath(id)
	runtimeSrc := filepath.Join(snapDir, "runtime")
	nginxSrc := filepath.Join(snapDir, "nginx", "nginx.conf")

	// Rollback is limited to the runtime cache directory and nginx.conf.
	if err := replaceDirContents(m.runtimeDir, runtimeSrc, nil); err != nil {
		return fmt.Errorf("restore runtime: %w", err)
	}
	if m.nginxConf != "" {
		if err := copyFile(nginxSrc, m.nginxConf); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("restore nginx conf: %w", err)
			}
		}
	}
	return nil
}

func (m *Manager) readStateLocked() (*state, error) {
	data, err := os.ReadFile(m.statePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &state{}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &st, nil
}

func (m *Manager) writeStateLocked(st *state) error {
	if err := os.MkdirAll(m.backupRoot, 0755); err != nil {
		return fmt.Errorf("mkdir backup root: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return writeFileAtomic(m.statePath(), data, 0644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func replaceDirContents(dstDir, srcDir string, keepNames map[string]bool) error {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if keepNames != nil && keepNames[name] {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dstDir, name))
	}

	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyDir(srcDir, dstDir, nil)
}

func copyDir(srcDir, dstDir string, skip func(srcPath string, d os.DirEntry) bool) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skip != nil && skip(path, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dstPath := filepath.Join(dstDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return copyFile(path, dstPath)
	})
}

func copyFile(srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return err
	}
	if info, err := os.Stat(srcPath); err == nil {
		_ = os.Chmod(dstPath, info.Mode().Perm())
	}
	return nil
}

func isUnder(path, parent string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}
