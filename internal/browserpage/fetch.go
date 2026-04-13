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

//go:embed fetch.mjs
var fetchScript []byte

type Options struct {
	Timeout                      time.Duration
	Headless                     bool
	LogWriter                    io.Writer
	Playwright                   string
	ProfileDir                   string
	WaitForURLSubstrings         []string
	CaptureResponseURLSubstrings []string
	CaptureResponseBodyBytes     int
}

type CapturedResponse struct {
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Body        string `json:"body,omitempty"`
}

type Result struct {
	FinalURL  string             `json:"final_url"`
	Title     string             `json:"title"`
	Text      string             `json:"text"`
	UserAgent string             `json:"user_agent,omitempty"`
	Responses []CapturedResponse `json:"responses,omitempty"`
}

type scriptInput struct {
	URL                          string   `json:"url"`
	TimeoutMillis                int      `json:"timeout_millis"`
	Headless                     bool     `json:"headless"`
	ProfileDir                   string   `json:"profile_dir,omitempty"`
	WaitForURLSubstrings         []string `json:"wait_for_url_substrings,omitempty"`
	CaptureResponseURLSubstrings []string `json:"capture_response_url_substrings,omitempty"`
	CaptureResponseBodyBytes     int      `json:"capture_response_body_bytes,omitempty"`
}

var runFetchScriptFunc = runFetchScript

func ReadText(ctx context.Context, targetURL string, opts Options) (Result, error) {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return Result{}, errors.New("browserpage: url missing")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	pw := strings.TrimSpace(opts.Playwright)
	if pw == "" {
		pw = "playwright@1.58.2"
	}

	td, err := os.MkdirTemp("", "ordercli-browserpage-*")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = os.RemoveAll(td) }()

	scriptPath := filepath.Join(td, "fetch.mjs")
	if err := os.WriteFile(scriptPath, fetchScript, 0o600); err != nil {
		return Result{}, err
	}
	outPath := filepath.Join(td, "out.json")

	in := scriptInput{
		URL:                          targetURL,
		TimeoutMillis:                int(opts.Timeout.Milliseconds()),
		Headless:                     opts.Headless,
		ProfileDir:                   strings.TrimSpace(opts.ProfileDir),
		WaitForURLSubstrings:         append([]string(nil), opts.WaitForURLSubstrings...),
		CaptureResponseURLSubstrings: append([]string(nil), opts.CaptureResponseURLSubstrings...),
		CaptureResponseBodyBytes:     opts.CaptureResponseBodyBytes,
	}
	if len(in.CaptureResponseURLSubstrings) > 0 && in.CaptureResponseBodyBytes <= 0 {
		in.CaptureResponseBodyBytes = 64 * 1024
	}
	b, _ := json.Marshal(in)

	out, err := runFetchScriptFunc(ctx, td, scriptPath, outPath, b, opts, pw)
	if err != nil {
		return Result{}, err
	}

	var res Result
	if err := json.Unmarshal(out, &res); err != nil {
		return Result{}, fmt.Errorf("browserpage: decode output: %w", err)
	}
	return res, nil
}

func runFetchScript(ctx context.Context, td, scriptPath, outPath string, input []byte, opts Options, playwright string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	projectDir, err := ensurePlaywrightProject(cmdCtx, playwright, opts.LogWriter)
	if err != nil {
		return nil, err
	}
	scriptPath, err = writePlaywrightScript(projectDir, "fetch.mjs", fetchScript)
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
		return nil, fmt.Errorf("browserpage: node run: %w", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("browserpage: missing output: %w", err)
	}
	return out, nil
}
