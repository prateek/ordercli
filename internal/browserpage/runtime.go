package browserpage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var playwrightProjectMu sync.Mutex

func ensurePlaywrightProject(ctx context.Context, playwright string, logWriter io.Writer) (string, error) {
	if _, err := exec.LookPath("node"); err != nil {
		return "", errors.New("browserpage: node not found")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return "", errors.New("browserpage: npm not found")
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	projectDir := filepath.Join(cacheRoot, "ordercli", "browserpage", sanitizePlaywrightPackage(playwright))

	playwrightProjectMu.Lock()
	defer playwrightProjectMu.Unlock()

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return "", err
	}
	if err := withPlaywrightProjectLock(ctx, projectDir, func() error {
		if _, err := os.Stat(filepath.Join(projectDir, "node_modules", "playwright", "package.json")); err != nil {
			install := exec.CommandContext(ctx, "npm", "install", "--silent", "--no-progress", "--no-fund", "--no-audit", playwright) //nolint:gosec
			install.Dir = projectDir
			install.Stdout = io.Discard
			if logWriter != nil {
				install.Stderr = logWriter
			} else {
				install.Stderr = io.Discard
			}
			install.Env = append(os.Environ(), "npm_config_loglevel=error")
			if err := install.Run(); err != nil {
				return fmt.Errorf("browserpage: npm install %s: %w", playwright, err)
			}
		}

		playwrightBin := filepath.Join(projectDir, "node_modules", ".bin", "playwright")
		if runtime.GOOS == "windows" {
			playwrightBin += ".cmd"
		}
		if _, err := os.Stat(filepath.Join(projectDir, ".chromium-installed")); err != nil {
			installBrowsers := exec.CommandContext(ctx, playwrightBin, "install", "chromium") //nolint:gosec
			installBrowsers.Dir = projectDir
			installBrowsers.Stdout = io.Discard
			if logWriter != nil {
				installBrowsers.Stderr = logWriter
			} else {
				installBrowsers.Stderr = io.Discard
			}
			installBrowsers.Env = append(os.Environ(), "npm_config_loglevel=error")
			if err := installBrowsers.Run(); err != nil {
				return fmt.Errorf("browserpage: playwright install chromium: %w", err)
			}
			if err := os.WriteFile(filepath.Join(projectDir, ".chromium-installed"), []byte("ok\n"), 0o600); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	return projectDir, nil
}

func withPlaywrightProjectLock(ctx context.Context, projectDir string, fn func() error) error {
	lockDir := filepath.Join(projectDir, ".install-lock")
	for {
		if info, err := os.Stat(lockDir); err == nil {
			if time.Since(info.ModTime()) > 10*time.Minute && stalePlaywrightProjectLock(lockDir) {
				_ = os.RemoveAll(lockDir)
			}
		}
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			_ = os.WriteFile(filepath.Join(lockDir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
			defer func() { _ = os.RemoveAll(lockDir) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func stalePlaywrightProjectLock(lockDir string) bool {
	pidPath := filepath.Join(lockDir, "pid")
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	if runtime.GOOS == "windows" {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err != nil && !errors.Is(err, syscall.EPERM)
}

func writePlaywrightScript(projectDir string, name string, contents []byte) (string, error) {
	scriptsDir := filepath.Join(projectDir, ".ordercli-scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		return "", err
	}
	scriptPath := filepath.Join(scriptsDir, name)
	if existing, err := os.ReadFile(scriptPath); err == nil && bytes.Equal(existing, contents) {
		return scriptPath, nil
	}
	if err := os.WriteFile(scriptPath, contents, 0o600); err != nil {
		return "", err
	}
	return scriptPath, nil
}

func sanitizePlaywrightPackage(playwright string) string {
	playwright = strings.TrimSpace(playwright)
	if playwright == "" {
		playwright = "playwright"
	}
	replacer := strings.NewReplacer("/", "-", "@", "-", ":", "-", " ", "-")
	return replacer.Replace(playwright)
}
