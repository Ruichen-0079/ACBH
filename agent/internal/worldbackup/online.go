package worldbackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RCONRunner executes Minecraft RCON commands for online consistent backups.
type RCONRunner interface {
	Execute(ctx context.Context, command string) (string, error)
}

// OnlineStagingOptions configures an online consistent world backup staging session.
type OnlineStagingOptions struct {
	ServerDir     string
	AppDataDir    string
	WorldRoots    []string
	TransactionID string
	RCON          RCONRunner
}

// StagedWorldFile records metadata for a file copied into transaction staging.
type StagedWorldFile struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	MTimeUnixNano int64  `json:"mtimeUnixNano"`
	SHA256        string `json:"sha256"`
	WorldRoot     string `json:"worldRoot"`
}

// OnlineStagingResult is produced after files are staged and save-on has completed.
type OnlineStagingResult struct {
	TransactionID string            `json:"transactionId"`
	StagingDir    string            `json:"stagingDir"`
	StagedFiles   []StagedWorldFile `json:"stagedFiles"`
	CommandLog    []string          `json:"commandLog,omitempty"`
}

// PrepareOnlineConsistentBackup runs save-off, save-all flush, copies world files into
// transaction staging while saves remain disabled, then save-on. Snapshot hashing must
// read only from StagingDir after this returns.
func PrepareOnlineConsistentBackup(ctx context.Context, opts OnlineStagingOptions) (OnlineStagingResult, error) {
	if opts.RCON == nil {
		return OnlineStagingResult{}, errors.New("RCON runner is required for online consistent backup")
	}
	if strings.TrimSpace(opts.ServerDir) == "" {
		return OnlineStagingResult{}, errors.New("server directory is required")
	}
	if strings.TrimSpace(opts.AppDataDir) == "" {
		return OnlineStagingResult{}, errors.New("app data directory is required")
	}
	transactionID := opts.TransactionID
	if transactionID == "" {
		transactionID = "txn_" + time.Now().UTC().Format("20060102_150405.000000000")
	}
	transactionID = safeTransactionID(transactionID)

	result := OnlineStagingResult{
		TransactionID: transactionID,
		StagingDir:    TransactionStagingDir(opts.AppDataDir, transactionID),
	}

	recovery := &saveOnRecovery{runner: opts.RCON}
	var runErr error
	defer func() {
		if err := recovery.release(context.Background()); err != nil {
			if runErr == nil {
				runErr = err
			} else if !strings.Contains(runErr.Error(), "save_on_failed") {
				runErr = fmt.Errorf("%w; additionally %v", runErr, err)
			}
		}
	}()

	if _, err := opts.RCON.Execute(ctx, "save-off"); err != nil {
		return result, fmt.Errorf("RCON save-off failed: %w", err)
	}
	recovery.arm()

	if _, err := opts.RCON.Execute(ctx, "save-all flush"); err != nil {
		return result, fmt.Errorf("RCON save-all flush failed: %w", err)
	}

	staged, err := stageWorldFiles(ctx, opts.ServerDir, result.StagingDir, opts.WorldRoots)
	if err != nil {
		return result, err
	}
	result.StagedFiles = staged

	if err := recovery.release(ctx); err != nil {
		return result, err
	}
	return result, nil
}

// TransactionDir returns <AppData>/world-backup/transactions/<transactionId>.
func TransactionDir(appDataDir, transactionID string) string {
	return filepath.Join(appDataDir, "world-backup", "transactions", safeTransactionID(transactionID))
}

// TransactionStagingDir returns <AppData>/world-backup/transactions/<transactionId>/staging/.
func TransactionStagingDir(appDataDir, transactionID string) string {
	return filepath.Join(TransactionDir(appDataDir, transactionID), "staging")
}

// RemoveTransactionDir deletes a completed or abandoned transaction directory.
func RemoveTransactionDir(appDataDir, transactionID string) error {
	return os.RemoveAll(TransactionDir(appDataDir, transactionID))
}

type saveOnRecovery struct {
	runner RCONRunner
	armed  bool
	done   bool
}

func (s *saveOnRecovery) arm() {
	s.armed = true
}

func (s *saveOnRecovery) release(ctx context.Context) error {
	if !s.armed || s.done {
		return nil
	}
	s.done = true
	_, err := s.runner.Execute(ctx, "save-on")
	if err != nil {
		return fmt.Errorf(
			"save_on_failed: RCON save-on failed: %w; manually execute save-on on the Minecraft server to resume automatic saves",
			err,
		)
	}
	return nil
}

func stageWorldFiles(ctx context.Context, serverDir, stagingDir string, worldRoots []string) ([]StagedWorldFile, error) {
	serverAbs, err := filepath.Abs(serverDir)
	if err != nil {
		return nil, fmt.Errorf("resolve server directory: %w", err)
	}
	serverAbs, err = filepath.EvalSymlinks(serverAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve server symlinks: %w", err)
	}
	stagingAbs, err := filepath.Abs(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("resolve staging directory: %w", err)
	}
	if err := os.RemoveAll(stagingAbs); err != nil {
		return nil, fmt.Errorf("clean staging directory: %w", err)
	}
	if err := os.MkdirAll(stagingAbs, 0o700); err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	// Canonicalize the root after it exists. On Windows, EvalSymlinks(target)
	// may expand an 8.3 component in the temporary-directory path; comparing
	// that result with the unexpanded staging root makes a file inside staging
	// look as though it escaped.
	stagingAbs, err = filepath.EvalSymlinks(stagingAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve staging symlinks: %w", err)
	}

	roots, err := ResolveWorldRoots(serverAbs, worldRoots)
	if err != nil {
		return nil, err
	}
	ignore, err := LoadIgnoreFile(serverAbs)
	if err != nil {
		return nil, err
	}

	sources, err := ListWorldBackupSourceFiles(serverAbs, roots, ignore)
	if err != nil {
		return nil, err
	}

	staged := make([]StagedWorldFile, 0, len(sources))
	for _, source := range sources {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		target, err := safeJoin(stagingAbs, source.Path)
		if err != nil {
			return nil, err
		}
		if err := copyFileStable(source.AbsPath, target); err != nil {
			return nil, fmt.Errorf("stage %s: %w", source.Path, err)
		}
		real, err := filepath.EvalSymlinks(target)
		if err != nil {
			return nil, fmt.Errorf("resolve staged file %s: %w", source.Path, err)
		}
		if !underRoot(stagingAbs, real) {
			return nil, fmt.Errorf("staged file %s escapes staging directory", source.Path)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("stat staged file %s: %w", source.Path, err)
		}
		sum, err := hashFile(real)
		if err != nil {
			return nil, fmt.Errorf("hash staged file %s: %w", source.Path, err)
		}
		staged = append(staged, StagedWorldFile{
			Path:          source.Path,
			Size:          info.Size(),
			MTimeUnixNano: info.ModTime().UnixNano(),
			SHA256:        sum,
			WorldRoot:     source.WorldRoot,
		})
	}
	return staged, nil
}

// beforeCopyHook is set by tests to simulate source changes between stat and copy.
var beforeCopyHook func(src string) error

func copyFileStable(src, dst string) error {
	for attempt := 0; attempt < 2; attempt++ {
		before, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("stat source: %w", err)
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("source is a symlink")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
		if beforeCopyHook != nil {
			if err := beforeCopyHook(src); err != nil {
				return err
			}
		}
		if err := copyFileContents(src, dst); err != nil {
			return err
		}
		after, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("stat source after copy: %w", err)
		}
		if before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) {
			return nil
		}
	}
	return errors.New("source file changed during staging copy")
}

func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy file: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename staged file: %w", err)
	}
	return nil
}
