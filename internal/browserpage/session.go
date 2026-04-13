package browserpage

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed session.mjs
var sessionScript []byte

type SessionResult struct {
	FinalURL     string `json:"final_url,omitempty"`
	UserAgent    string `json:"user_agent,omitempty"`
	CookieHeader string `json:"cookie_header,omitempty"`
}

type sessionScriptInput struct {
	URL                  string   `json:"url"`
	TimeoutMillis        int      `json:"timeout_millis"`
	Headless             bool     `json:"headless"`
	ProfileDir           string   `json:"profile_dir,omitempty"`
	WaitForURLSubstrings []string `json:"wait_for_url_substrings,omitempty"`
}

var runSessionScriptFunc = runSessionScript

func ReadSession(ctx context.Context, targetURL string, opts Options) (SessionResult, error) {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return SessionResult{}, errors.New("browserpage: url missing")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	pw := strings.TrimSpace(opts.Playwright)
	if pw == "" {
		pw = "playwright@1.58.2"
	}

	td, err := os.MkdirTemp("", "ordercli-browserpage-session-*")
	if err != nil {
		return SessionResult{}, err
	}
	defer func() { _ = os.RemoveAll(td) }()

	outPath := filepath.Join(td, "out.json")
	in := sessionScriptInput{
		URL:                  targetURL,
		TimeoutMillis:        int(opts.Timeout.Milliseconds()),
		Headless:             opts.Headless,
		ProfileDir:           strings.TrimSpace(opts.ProfileDir),
		WaitForURLSubstrings: append([]string(nil), opts.WaitForURLSubstrings...),
	}
	b, _ := json.Marshal(in)

	out, err := runSessionScriptFunc(ctx, td, outPath, b, opts, pw)
	if err != nil {
		return SessionResult{}, err
	}

	var res SessionResult
	if err := json.Unmarshal(out, &res); err != nil {
		return SessionResult{}, fmt.Errorf("browserpage: decode session output: %w", err)
	}
	return res, nil
}

func runSessionScript(ctx context.Context, td, outPath string, input []byte, opts Options, playwright string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	projectDir, err := ensurePlaywrightProject(cmdCtx, playwright, opts.LogWriter)
	if err != nil {
		return nil, err
	}
	scriptPath, err := writePlaywrightScript(projectDir, "session.mjs", sessionScript)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(cmdCtx, "node", scriptPath) //nolint:gosec
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(),
		"ORDERCLI_OUTPUT_PATH="+outPath,
		"npm_config_loglevel=error",
	)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = io.Discard
	if opts.LogWriter != nil {
		cmd.Stderr = opts.LogWriter
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("browserpage: node session run: %w", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("browserpage: missing session output: %w", err)
	}
	return out, nil
}
