package browserpage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadText_EmptyURL(t *testing.T) {
	_, err := ReadText(context.Background(), "   ", Options{})
	if err == nil || !strings.Contains(err.Error(), "url missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadText_UsesDefaultsAndDecodesResult(t *testing.T) {
	orig := runFetchScriptFunc
	t.Cleanup(func() { runFetchScriptFunc = orig })

	runFetchScriptFunc = func(_ context.Context, _, _, _ string, input []byte, opts Options, playwright string) ([]byte, error) {
		var in scriptInput
		if err := json.Unmarshal(input, &in); err != nil {
			t.Fatalf("unmarshal input: %v", err)
		}
		if in.URL != "https://example.com" {
			t.Fatalf("url=%q", in.URL)
		}
		if in.TimeoutMillis != int((2 * time.Minute).Milliseconds()) {
			t.Fatalf("timeout=%d", in.TimeoutMillis)
		}
		if in.Headless {
			t.Fatalf("expected headless default false in input")
		}
		if opts.Timeout != 2*time.Minute {
			t.Fatalf("opts timeout=%s", opts.Timeout)
		}
		if playwright != "playwright@1.58.2" {
			t.Fatalf("playwright=%q", playwright)
		}
		return []byte(`{"final_url":"https://example.com/final","title":"T","text":"Body"}`), nil
	}

	got, err := ReadText(context.Background(), "https://example.com", Options{})
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if got.FinalURL != "https://example.com/final" || got.Title != "T" || got.Text != "Body" {
		t.Fatalf("got=%+v", got)
	}
}

func TestReadText_PassesProfileAndCaptureOptions(t *testing.T) {
	orig := runFetchScriptFunc
	t.Cleanup(func() { runFetchScriptFunc = orig })

	runFetchScriptFunc = func(_ context.Context, _, _, _ string, input []byte, opts Options, playwright string) ([]byte, error) {
		var in scriptInput
		if err := json.Unmarshal(input, &in); err != nil {
			t.Fatalf("unmarshal input: %v", err)
		}
		if in.ProfileDir != "/tmp/profile" {
			t.Fatalf("profile_dir=%q", in.ProfileDir)
		}
		if len(in.WaitForURLSubstrings) != 1 || in.WaitForURLSubstrings[0] != "/orders/" {
			t.Fatalf("wait_for_url_substrings=%v", in.WaitForURLSubstrings)
		}
		if len(in.CaptureResponseURLSubstrings) != 2 || in.CaptureResponseURLSubstrings[0] != "activeOrders" || in.CaptureResponseURLSubstrings[1] != "/graphql" {
			t.Fatalf("capture_response_url_substrings=%v", in.CaptureResponseURLSubstrings)
		}
		if in.CaptureResponseBodyBytes != 2048 {
			t.Fatalf("capture_response_body_bytes=%d", in.CaptureResponseBodyBytes)
		}
		if opts.ProfileDir != "/tmp/profile" {
			t.Fatalf("opts profile_dir=%q", opts.ProfileDir)
		}
		if playwright != "playwright@custom" {
			t.Fatalf("playwright=%q", playwright)
		}
		return []byte(`{"final_url":"https://example.com/final","title":"T","text":"Body","user_agent":"Mozilla/5.0","responses":[{"url":"https://example.com/graphql","status":200,"content_type":"application/json","body":"{\"ok\":true}"}]}`), nil
	}

	got, err := ReadText(context.Background(), "https://example.com", Options{
		Timeout:                      5 * time.Second,
		Headless:                     true,
		Playwright:                   "playwright@custom",
		ProfileDir:                   "/tmp/profile",
		WaitForURLSubstrings:         []string{"/orders/"},
		CaptureResponseURLSubstrings: []string{"activeOrders", "/graphql"},
		CaptureResponseBodyBytes:     2048,
	})
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if len(got.Responses) != 1 {
		t.Fatalf("responses=%+v", got.Responses)
	}
	if got.UserAgent != "Mozilla/5.0" {
		t.Fatalf("user_agent=%q", got.UserAgent)
	}
	if got.Responses[0].URL != "https://example.com/graphql" || got.Responses[0].Status != 200 || got.Responses[0].ContentType != "application/json" || got.Responses[0].Body != "{\"ok\":true}" {
		t.Fatalf("response=%+v", got.Responses[0])
	}
}

func TestReadText_InvalidJSON(t *testing.T) {
	orig := runFetchScriptFunc
	t.Cleanup(func() { runFetchScriptFunc = orig })

	runFetchScriptFunc = func(_ context.Context, _, _, _ string, _ []byte, _ Options, _ string) ([]byte, error) {
		return []byte(`{`), nil
	}

	_, err := ReadText(context.Background(), "https://example.com", Options{Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "decode output") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunFetchScript_NodeMissing(t *testing.T) {
	t.Setenv("PATH", "")

	_, err := runFetchScript(context.Background(), t.TempDir(), "script.mjs", "out.json", nil, Options{Timeout: time.Second}, "playwright@1.58.2")
	if err == nil || !strings.Contains(err.Error(), "node not found") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunFetchScript_NpmMissing(t *testing.T) {
	binDir := t.TempDir()
	nodePath := filepath.Join(binDir, "node")
	if err := os.WriteFile(nodePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake node: %v", err)
	}
	t.Setenv("PATH", binDir)

	_, err := runFetchScript(context.Background(), t.TempDir(), "script.mjs", "out.json", nil, Options{Timeout: time.Second}, "playwright@1.58.2")
	if err == nil || !strings.Contains(err.Error(), "npm not found") {
		t.Fatalf("err=%v", err)
	}
}
