package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steipete/ordercli/internal/browserpage"
	"github.com/steipete/ordercli/internal/ubereats"
)

var uberEatsCaptureResponseURLSubstrings = []string{"getPastOrdersV1", "getActiveOrdersV1"}

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
		},
	}
}

func newUberEatsConfigSetCmd(st *state) *cobra.Command {
	var baseURL string
	var browserProfile string

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update Uber Eats config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := st.ubereats()
			if strings.TrimSpace(baseURL) == "" && strings.TrimSpace(browserProfile) == "" {
				return errors.New("nothing to set (use --base-url and/or --browser-profile)")
			}
			if strings.TrimSpace(baseURL) != "" {
				cfg.BaseURL = normalizeURLBase(baseURL)
			}
			if strings.TrimSpace(browserProfile) != "" {
				cfg.BrowserProfile = strings.TrimSpace(browserProfile)
			}
			st.markDirty()
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "", "base URL (default: https://www.ubereats.com)")
	cmd.Flags().StringVar(&browserProfile, "browser-profile", "", "persistent Playwright profile dir")
	return cmd
}

func newUberEatsLoginCmd(st *state) *cobra.Command {
	var browser bool
	var browserProfile string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Open Uber Eats in a browser and wait for a logged-in orders page",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !browser {
				return errors.New("only the browser flow is supported right now (use --browser)")
			}

			cfg := st.ubereats()
			profileDir := uberEatsProfileDir(st, browserProfile)
			targetURL := normalizeURLBase(cfg.BaseURL) + "/orders/"
			res, err := uberEatsLoginBrowser(cmd.Context(), targetURL, profileDir, timeout)
			if err != nil {
				return err
			}
			if !strings.Contains(res.FinalURL, "/orders") {
				return fmt.Errorf("login did not reach an orders page (final_url=%s)", res.FinalURL)
			}

			cfg.BrowserProfile = profileDir
			if res.UserAgent != "" {
				cfg.HTTPUserAgent = res.UserAgent
			}
			st.markDirty()
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}

	cmd.Flags().BoolVar(&browser, "browser", false, "open an interactive browser session")
	cmd.Flags().StringVar(&browserProfile, "browser-profile", "", "persistent Playwright profile dir")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "how long to wait for login")
	return cmd
}

func newUberEatsOrdersCmd(st *state) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "orders",
		Short: "List active Uber Eats orders from the web account",
		RunE: func(cmd *cobra.Command, args []string) error {
			page, err := readUberEatsOrdersPage(cmd, st, uberEatsOrdersURL(st))
			if err != nil {
				return err
			}
			return writeUberEatsOrders(cmd.OutOrStdout(), filterUberEatsOrders(page.Orders, true), asJSON, "no active orders")
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print raw JSON")
	return cmd
}

func newUberEatsHistoryCmd(st *state) *cobra.Command {
	var asJSON bool
	var limit int

	cmd := &cobra.Command{
		Use:   "history",
		Short: "List recent Uber Eats orders from the web account",
		RunE: func(cmd *cobra.Command, args []string) error {
			page, err := readUberEatsOrdersPage(cmd, st, uberEatsOrdersURL(st))
			if err != nil {
				return err
			}

			orders := filterUberEatsOrders(page.Orders, false)
			if limit > 0 && len(orders) > limit {
				orders = orders[:limit]
			}
			return writeUberEatsOrders(cmd.OutOrStdout(), orders, asJSON, "no orders")
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print raw JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "max orders to print")
	return cmd
}

func newUberEatsOrderCmd(st *state) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "order <uuid|url|latest>",
		Short: "Show details for a single Uber Eats order",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := strings.TrimSpace(args[0])
			page, err := readUberEatsOrdersPage(cmd, st, uberEatsOrdersURL(st))
			if err != nil {
				return err
			}
			if strings.EqualFold(ref, "latest") {
				if len(page.Orders) == 0 {
					return errors.New("no Uber Eats orders found")
				}
				ref = page.Orders[0].UUID
			}
			targetURL, _ := ubereats.BuildOrderURL(st.ubereats().BaseURL, ref)
			order, err := selectUberEatsOrder(page.Orders, ref, targetURL)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(order)
			}

			fmt.Fprintln(cmd.OutOrStdout(), order.DetailsString())
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print raw JSON")
	return cmd
}

func readUberEatsOrdersPage(cmd *cobra.Command, st *state, targetURL string) (ubereats.Page, error) {
	profileDir := uberEatsProfileDir(st, "")
	res, err := uberEatsReadBrowserPage(cmd.Context(), targetURL, browserpage.Options{
		Timeout:                      2 * time.Minute,
		Headless:                     true,
		LogWriter:                    cmd.ErrOrStderr(),
		ProfileDir:                   profileDir,
		CaptureResponseURLSubstrings: uberEatsCaptureResponseURLSubstrings,
		CaptureResponseBodyBytes:     256 * 1024,
	})
	if err != nil {
		return ubereats.Page{}, err
	}

	if looksLikeUberEatsLoginPage(res) {
		return ubereats.Page{}, errors.New("not logged in (run `ordercli ubereats login --browser`)")
	}

	page := ubereats.ParsePage(res)
	if len(page.Orders) == 0 {
		return ubereats.Page{}, fmt.Errorf("no Uber Eats orders found at %s", targetURL)
	}
	return page, nil
}

func uberEatsOrdersURL(st *state) string {
	return normalizeURLBase(st.ubereats().BaseURL) + "/orders/"
}

func filterUberEatsOrders(orders []ubereats.Order, active bool) []ubereats.Order {
	filtered := make([]ubereats.Order, 0, len(orders))
	for _, order := range orders {
		if order.Active != active {
			continue
		}
		filtered = append(filtered, order)
	}
	return filtered
}

func writeUberEatsOrders(w io.Writer, orders []ubereats.Order, asJSON bool, emptyMessage string) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(orders)
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

func normalizeURLBase(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://www.ubereats.com"
	}
	return strings.TrimRight(baseURL, "/")
}

func looksLikeUberEatsLoginPage(res browserpage.Result) bool {
	finalURL := strings.ToLower(strings.TrimSpace(res.FinalURL))
	body := strings.ToLower(strings.TrimSpace(res.Text))
	return strings.Contains(finalURL, "login") || strings.Contains(finalURL, "login-redirect") || strings.Contains(body, "please sign in")
}

func selectUberEatsOrder(orders []ubereats.Order, ref string, targetURL string) (ubereats.Order, error) {
	ref = strings.TrimSpace(ref)
	if parsedUUID := uberEatsUUIDFromRef(ref); parsedUUID != "" {
		ref = parsedUUID
	}
	for _, order := range orders {
		if order.UUID == ref || strings.EqualFold(order.URL, ref) || strings.EqualFold(order.URL, targetURL) {
			return order, nil
		}
	}
	if len(orders) == 1 {
		return orders[0], nil
	}
	return ubereats.Order{}, fmt.Errorf("order %q not found", ref)
}

func uberEatsUUIDFromRef(ref string) string {
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return strings.TrimSpace(ref)
	}
	u, err := url.Parse(ref)
	if err != nil {
		return strings.TrimSpace(ref)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "orders" {
			return parts[i+1]
		}
	}
	return strings.TrimSpace(ref)
}
