package browserpage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReadSession_EmptyURL(t *testing.T) {
	_, err := ReadSession(context.Background(), "", Options{})
	if err == nil || !strings.Contains(err.Error(), "url missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadSession_UsesDefaultsAndDecodesResult(t *testing.T) {
	orig := runSessionScriptFunc
	t.Cleanup(func() { runSessionScriptFunc = orig })

	runSessionScriptFunc = func(_ context.Context, _, _ string, input []byte, opts Options, playwright string) ([]byte, error) {
		var in sessionScriptInput
		if err := json.Unmarshal(input, &in); err != nil {
			t.Fatalf("unmarshal input: %v", err)
		}
		if in.URL != "https://www.ubereats.com/orders/" {
			t.Fatalf("url=%q", in.URL)
		}
		if in.TimeoutMillis != int((2 * time.Minute).Milliseconds()) {
			t.Fatalf("timeout=%d", in.TimeoutMillis)
		}
		if playwright != "playwright@1.58.2" {
			t.Fatalf("playwright=%q", playwright)
		}
		return []byte(`{"final_url":"https://www.ubereats.com/orders/","user_agent":"Mozilla/5.0 Test","cookie_header":"sid=abc; auth=xyz"}`), nil
	}

	got, err := ReadSession(context.Background(), "https://www.ubereats.com/orders/", Options{})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if got.FinalURL != "https://www.ubereats.com/orders/" || got.UserAgent != "Mozilla/5.0 Test" || got.CookieHeader != "sid=abc; auth=xyz" {
		t.Fatalf("got=%+v", got)
	}
}

func TestReadSession_PassesProfileAndWaitTargets(t *testing.T) {
	orig := runSessionScriptFunc
	t.Cleanup(func() { runSessionScriptFunc = orig })

	runSessionScriptFunc = func(_ context.Context, _, _ string, input []byte, opts Options, playwright string) ([]byte, error) {
		var in sessionScriptInput
		if err := json.Unmarshal(input, &in); err != nil {
			t.Fatalf("unmarshal input: %v", err)
		}
		if in.ProfileDir != "/tmp/ubereats-profile" {
			t.Fatalf("profile_dir=%q", in.ProfileDir)
		}
		if len(in.WaitForURLSubstrings) != 1 || in.WaitForURLSubstrings[0] != "/orders" {
			t.Fatalf("wait_for_url_substrings=%v", in.WaitForURLSubstrings)
		}
		if opts.Timeout != 5*time.Second {
			t.Fatalf("opts timeout=%s", opts.Timeout)
		}
		if playwright != "playwright@custom" {
			t.Fatalf("playwright=%q", playwright)
		}
		return []byte(`{"final_url":"https://www.ubereats.com/orders/","user_agent":"Mozilla/5.0 Test","cookie_header":"sid=abc"}`), nil
	}

	_, err := ReadSession(context.Background(), "https://www.ubereats.com/orders/", Options{
		Timeout:              5 * time.Second,
		ProfileDir:           "/tmp/ubereats-profile",
		Playwright:           "playwright@custom",
		WaitForURLSubstrings: []string{"/orders"},
	})
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
}
