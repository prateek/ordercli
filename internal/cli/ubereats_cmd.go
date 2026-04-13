package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steipete/ordercli/internal/ubereats"
)

const uberEatsProfileMarker = ".ordercli-ubereats-profile"

func newUberEatsConfigCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show/edit Uber Eats config",
	}
	cmd.AddCommand(newUberEatsConfigShowCmd(st))
	cmd.AddCommand(newUberEatsConfigSetCmd(st))
	return cmd
}

func newUberEatsConfigShowCmd(st *state) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print current Uber Eats config",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := st.ubereats()
			fmt.Fprintf(cmd.OutOrStdout(), "base_url=%s\n", cfg.BaseURL)
			if cfg.BrowserProfile != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "browser_profile=%s\n", cfg.BrowserProfile)
			}
			if cfg.HTTPUserAgent != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "http_user_agent=%s\n", cfg.HTTPUserAgent)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "default_watch_interval=%s\n", cfg.DefaultWatchInterval)
			if cfg.Debug {
				fmt.Fprintln(cmd.OutOrStdout(), "debug=on")
				return
			}
			fmt.Fprintln(cmd.OutOrStdout(), "debug=off")
		},
	}
}

func newUberEatsConfigSetCmd(st *state) *cobra.Command {
	var baseURL string
	var browserProfile string
	var watchInterval time.Duration
	var watchIntervalSet bool
	var debug string

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update Uber Eats config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := st.ubereats()
			if strings.TrimSpace(baseURL) == "" && strings.TrimSpace(browserProfile) == "" && !watchIntervalSet && strings.TrimSpace(debug) == "" {
				return errors.New("nothing to set")
			}
			if strings.TrimSpace(baseURL) != "" {
				normalized, err := normalizeUberEatsBaseURL(baseURL)
				if err != nil {
					return err
				}
				cfg.BaseURL = normalized
			}
			if strings.TrimSpace(browserProfile) != "" {
				profileDir, err := ensureManagedUberEatsProfileDir(strings.TrimSpace(browserProfile))
				if err != nil {
					return err
				}
				cfg.BrowserProfile = profileDir
			}
			if watchIntervalSet {
				if watchInterval <= 0 {
					return errors.New("watch interval must be positive")
				}
				cfg.DefaultWatchInterval = watchInterval
			}
			if strings.TrimSpace(debug) != "" {
				switch strings.ToLower(strings.TrimSpace(debug)) {
				case "on", "true", "1", "yes":
					cfg.Debug = true
				case "off", "false", "0", "no":
					cfg.Debug = false
				default:
					return errors.New("debug must be on or off")
				}
			}
			st.markDirty()
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "", "base URL (default: https://www.ubereats.com)")
	cmd.Flags().StringVar(&browserProfile, "browser-profile", "", "CLI-managed browser profile dir")
	cmd.Flags().DurationVar(&watchInterval, "watch-interval", 0, "default polling interval for --watch")
	cmd.Flags().BoolVar(&watchIntervalSet, "watch-interval-set", false, "internal marker")
	_ = cmd.Flags().MarkHidden("watch-interval-set")
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		if cmd.Flags().Changed("watch-interval") {
			watchIntervalSet = true
		}
	}
	cmd.Flags().StringVar(&debug, "debug", "", "debug tracing default: on|off")
	return cmd
}

func newUberEatsLoginCmd(st *state) *cobra.Command {
	var browser bool
	var browserProfile string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Open Uber Eats in a browser and wait for a logged-in session",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := st.ubereats()
			profileDir, finalizeProfileDir, err := prepareUberEatsProfileDirForLogin(st, browserProfile)
			if err != nil {
				return err
			}
			baseURL, err := normalizeUberEatsBaseURL(cfg.BaseURL)
			if err != nil {
				return err
			}
			targetURL := baseURL + "/orders/"
			res, err := uberEatsLoginBrowser(cmd.Context(), targetURL, profileDir, timeout)
			if err != nil {
				return err
			}
			cfg.BaseURL = baseURL
			cfg.BrowserProfile = profileDir
			if strings.TrimSpace(res.CookieHeader) == "" {
				return errors.New("login did not produce an authenticated cookie jar")
			}
			if res.UserAgent != "" {
				cfg.HTTPUserAgent = res.UserAgent
			}
			st.markDirty()

			client := uberEatsClientFactory(st, uberEatsCommand{})
			client.SetCookieHeader(strings.TrimSpace(res.CookieHeader))
			session, err := client.CheckSession(cmd.Context())
			if err != nil {
				return err
			}
			if !session.LoggedIn {
				return errors.New("login did not result in a logged-in Uber Eats session")
			}
			if err := finalizeProfileDir(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}

	cmd.Flags().BoolVar(&browser, "browser", false, "deprecated compatibility flag")
	_ = cmd.Flags().MarkHidden("browser")
	cmd.Flags().StringVar(&browserProfile, "browser-profile", "", "CLI-managed browser profile dir")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait for login")
	return cmd
}

func newUberEatsLogoutCmd(st *state) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Clear the local Uber Eats session",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := st.ubereats()
			if !yes {
				confirmed, err := promptForYes(cmd.ErrOrStderr(), "Delete the local Uber Eats session? [y/N]: ")
				if err != nil {
					return err
				}
				if !confirmed {
					return errors.New("logout aborted")
				}
			}
			if cfg.BrowserProfile != "" {
				if err := removeManagedUberEatsProfileDir(cfg.BrowserProfile); err != nil {
					return err
				}
			}
			cfg.BrowserProfile = ""
			cfg.HTTPUserAgent = ""
			st.markDirty()
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

func newUberEatsOrdersCmd(st *state) *cobra.Command {
	var asJSON bool
	var watch bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "orders",
		Short: "Inspect Uber Eats orders",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilterActive, 20, asJSON, watchInterval(st, interval, watch), true)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&watch, "watch", false, "poll until interrupted")
	cmd.Flags().DurationVar(&interval, "interval", 0, "override polling interval")
	cmd.AddCommand(newUberEatsOrdersListCmd(st))
	cmd.AddCommand(newUberEatsOrdersShowCmd(st))
	return cmd
}

func newUberEatsHistoryCmd(st *state) *cobra.Command {
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:    "history",
		Short:  "Deprecated compatibility alias for `orders list --filter past`",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilterPast, limit, asJSON, 0, true)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "max orders to return")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsOrderCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:    "order <order-ref>",
		Short:  "Deprecated compatibility alias for `orders show`",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUberEatsOrdersShow(cmd, st, args[0], asJSON, 0, true)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsOrdersListCmd(st *state) *cobra.Command {
	var filter string
	var limit int
	var asJSON bool
	var watch bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Uber Eats orders",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilter(strings.TrimSpace(filter)), limit, asJSON, watchInterval(st, interval, watch), false)
		},
	}

	cmd.Flags().StringVar(&filter, "filter", string(ubereats.OrderFilterActive), "active, past, or all")
	cmd.Flags().IntVar(&limit, "limit", 20, "max orders to return")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&watch, "watch", false, "poll until interrupted")
	cmd.Flags().DurationVar(&interval, "interval", 0, "override polling interval")
	return cmd
}

func newUberEatsOrdersShowCmd(st *state) *cobra.Command {
	var asJSON bool
	var watch bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "show <order-ref>",
		Short: "Show one Uber Eats order",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUberEatsOrdersShow(cmd, st, args[0], asJSON, watchInterval(st, interval, watch), false)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&watch, "watch", false, "poll until interrupted")
	cmd.Flags().DurationVar(&interval, "interval", 0, "override polling interval")
	return cmd
}

func runUberEatsOrdersList(cmd *cobra.Command, st *state, filter ubereats.OrderFilter, limit int, asJSON bool, interval time.Duration, legacyJSON bool) error {
	if filter == "" {
		filter = ubereats.OrderFilterActive
	}
	if interval > 0 && filter != ubereats.OrderFilterActive {
		return errors.New("--watch is only supported for active orders")
	}
	client := uberEatsClientFactory(st, uberEatsCommand{})
	run := func() error {
		orders, err := client.ListOrders(cmd.Context(), filter, limit)
		if err != nil {
			return err
		}
		return writeUberEatsOrders(cmd.OutOrStdout(), orders, asJSON, legacyJSON, "no orders")
	}
	if interval <= 0 {
		return run()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := run(); err != nil {
			return err
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runUberEatsOrdersShow(cmd *cobra.Command, st *state, ref string, asJSON bool, interval time.Duration, legacyJSON bool) error {
	client := uberEatsClientFactory(st, uberEatsCommand{})
	run := func() error {
		if strings.EqualFold(strings.TrimSpace(ref), "latest") {
			orders, err := client.ListOrders(cmd.Context(), ubereats.OrderFilterAll, 1)
			if err != nil {
				return err
			}
			if len(orders) == 0 {
				return errors.New("no Uber Eats orders found")
			}
			return writeUberEatsOrder(cmd.OutOrStdout(), orders[0], asJSON, legacyJSON)
		}
		order, err := client.GetOrder(cmd.Context(), ref)
		if err != nil {
			return err
		}
		return writeUberEatsOrder(cmd.OutOrStdout(), order, asJSON, legacyJSON)
	}
	if interval <= 0 {
		return run()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := run(); err != nil {
			return err
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

func writeUberEatsOrders(w io.Writer, orders []ubereats.Order, asJSON bool, legacyJSON bool, emptyMessage string) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if legacyJSON {
			return enc.Encode(orders)
		}
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"items": orders,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	if len(orders) == 0 {
		_, err := fmt.Fprintln(w, emptyMessage)
		return err
	}
	for _, order := range orders {
		if _, err := fmt.Fprintln(w, order.Summary()); err != nil {
			return err
		}
	}
	return nil
}

func writeUberEatsOrder(w io.Writer, order ubereats.Order, asJSON bool, legacyJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if legacyJSON {
			return enc.Encode(order)
		}
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": order,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	_, err := fmt.Fprintln(w, order.DetailsString())
	return err
}

func uberEatsProfileDir(st *state, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	cfg := st.ubereats()
	if strings.TrimSpace(cfg.BrowserProfile) != "" {
		return strings.TrimSpace(cfg.BrowserProfile)
	}
	return filepath.Join(filepath.Dir(st.configPath), "ubereats-browser-profile")
}

func ensureManagedUberEatsProfileDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", errors.New("browser profile dir missing")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	markerPath := filepath.Join(dir, uberEatsProfileMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return "", errors.New("refusing to use non-empty unmanaged profile dir")
	}
	if err := os.WriteFile(markerPath, []byte("managed by ordercli\n"), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func prepareUberEatsProfileDirForLogin(st *state, override string) (string, func() error, error) {
	dir := uberEatsProfileDir(st, override)
	managedDir, err := ensureManagedUberEatsProfileDir(dir)
	if err == nil {
		return managedDir, func() error { return nil }, nil
	}
	if !strings.Contains(err.Error(), "non-empty unmanaged profile dir") {
		return "", nil, err
	}
	if strings.TrimSpace(override) == "" && strings.TrimSpace(st.ubereats().BrowserProfile) == strings.TrimSpace(dir) && looksLikeLegacyUberEatsProfileDir(dir) {
		return dir, func() error {
			markerPath := filepath.Join(dir, uberEatsProfileMarker)
			return os.WriteFile(markerPath, []byte("managed by ordercli\n"), 0o600)
		}, nil
	}
	return "", nil, err
}

func removeManagedUberEatsProfileDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, uberEatsProfileMarker)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("refusing to delete unmanaged Uber Eats profile dir")
		}
		return err
	}
	return os.RemoveAll(dir)
}

func looksLikeLegacyUberEatsProfileDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "Local State")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "Default", "Cookies")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "Default", "Network", "Cookies")); err == nil {
		return true
	}
	return false
}

func normalizeUberEatsBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://www.ubereats.com"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" {
		return "", errors.New("Uber Eats base URL must use https")
	}
	if host := strings.ToLower(u.Hostname()); host != "www.ubereats.com" {
		return "", fmt.Errorf("unsupported Uber Eats host %q", u.Host)
	}
	if port := strings.TrimSpace(u.Port()); port != "" && port != "443" {
		return "", fmt.Errorf("unsupported Uber Eats port %q", port)
	}
	u.Host = u.Hostname()
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func promptForYes(w io.Writer, prompt string) (bool, error) {
	if _, err := fmt.Fprint(w, prompt); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func watchInterval(st *state, override time.Duration, watch bool) time.Duration {
	if !watch {
		return 0
	}
	if override > 0 {
		return override
	}
	return st.ubereats().DefaultWatchInterval
}
