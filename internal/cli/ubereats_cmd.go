package cli

import (
	"bufio"
	"context"
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

func newUberEatsAddressesCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addresses",
		Short: "Inspect Uber Eats delivery addresses",
	}
	cmd.AddCommand(newUberEatsAddressesListCmd(st))
	cmd.AddCommand(newUberEatsAddressesShowCmd(st))
	return cmd
}

func newUberEatsCartsCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "carts",
		Short: "Inspect Uber Eats draft carts",
	}
	cmd.AddCommand(newUberEatsCartsListCmd(st))
	cmd.AddCommand(newUberEatsCartsShowCmd(st))
	cmd.AddCommand(newUberEatsCartsCreateCmd(st))
	cmd.AddCommand(newUberEatsCartsUpdateCmd(st))
	cmd.AddCommand(newUberEatsCartsDiscardCmd(st))
	cmd.AddCommand(newUberEatsCartItemsCmd(st))
	return cmd
}

func newUberEatsCartsListCmd(st *state) *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Uber Eats draft carts",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			carts, err := client.ListCarts(cmd.Context(), limit)
			if err != nil {
				return err
			}
			return writeUberEatsCarts(cmd.OutOrStdout(), carts, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "max carts to return")
	return cmd
}

func newUberEatsCartsShowCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <cart-ref>",
		Short: "Show one Uber Eats draft cart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			cart, err := client.GetCart(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeUberEatsCart(cmd.OutOrStdout(), cart, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsCartsCreateCmd(st *state) *cobra.Command {
	var asJSON bool
	var fromOrderRef string
	var storeRef string
	var itemRef string
	var quantity int
	var note string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create one Uber Eats draft cart",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			var (
				cart ubereats.Cart
				err  error
			)
			switch {
			case strings.TrimSpace(fromOrderRef) != "":
				if strings.TrimSpace(storeRef) != "" || strings.TrimSpace(itemRef) != "" || strings.TrimSpace(note) != "" || quantity != 1 {
					return fmt.Errorf("ubereats: --from-order cannot be combined with --store, --item, --note, or a non-default --quantity")
				}
				cart, err = client.CreateCartFromOrder(cmd.Context(), fromOrderRef)
			case strings.TrimSpace(storeRef) != "" && strings.TrimSpace(itemRef) != "":
				cart, err = client.CreateCartFromItem(cmd.Context(), storeRef, itemRef, quantity, note)
			default:
				return fmt.Errorf("ubereats: either --from-order or both --store and --item are required")
			}
			if err != nil {
				return err
			}
			return writeUberEatsCart(cmd.OutOrStdout(), cart, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&fromOrderRef, "from-order", "", "order ref")
	cmd.Flags().StringVar(&storeRef, "store", "", "store ref")
	cmd.Flags().StringVar(&itemRef, "item", "", "item ref")
	cmd.Flags().IntVar(&quantity, "quantity", 1, "item quantity")
	cmd.Flags().StringVar(&note, "note", "", "special instructions")
	return cmd
}

func newUberEatsCartsUpdateCmd(st *state) *cobra.Command {
	var asJSON bool
	var deliveryType string
	var interactionType string
	cmd := &cobra.Command{
		Use:   "update <cart-ref>",
		Short: "Update one Uber Eats draft cart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			result, err := client.UpdateCart(cmd.Context(), args[0], ubereats.CartUpdate{
				DeliveryType:    deliveryType,
				InteractionType: interactionType,
			})
			if err != nil {
				return err
			}
			return writeUberEatsCartMutation(cmd.OutOrStdout(), result, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&deliveryType, "delivery-type", "", "delivery type (regular or premium)")
	cmd.Flags().StringVar(&interactionType, "interaction-type", "", "interaction type")
	return cmd
}

func newUberEatsCartsDiscardCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "discard <cart-ref>",
		Short: "Discard one Uber Eats draft cart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			result, err := client.DiscardCart(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeUberEatsCartMutation(cmd.OutOrStdout(), result, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsCartItemsCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "items",
		Short: "Mutate Uber Eats cart items",
	}
	cmd.AddCommand(newUberEatsCartItemsAddCmd(st))
	cmd.AddCommand(newUberEatsCartItemsUpdateCmd(st))
	cmd.AddCommand(newUberEatsCartItemsRemoveCmd(st))
	return cmd
}

func newUberEatsCartItemsAddCmd(st *state) *cobra.Command {
	var asJSON bool
	var itemRef string
	var quantity int
	var note string
	cmd := &cobra.Command{
		Use:   "add <cart-ref>",
		Short: "Add one item to an Uber Eats draft cart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(itemRef) == "" {
				return fmt.Errorf("ubereats: --item is required")
			}
			client := uberEatsClientFactory(st, uberEatsCommand{})
			result, err := client.AddCartItem(cmd.Context(), args[0], itemRef, quantity, note)
			if err != nil {
				return err
			}
			return writeUberEatsCartMutation(cmd.OutOrStdout(), result, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&itemRef, "item", "", "item ref")
	cmd.Flags().IntVar(&quantity, "quantity", 1, "item quantity")
	cmd.Flags().StringVar(&note, "note", "", "special instructions")
	return cmd
}

func newUberEatsCartItemsUpdateCmd(st *state) *cobra.Command {
	var asJSON bool
	var quantity int
	var note string
	cmd := &cobra.Command{
		Use:   "update <cart-ref> <cart-item-ref>",
		Short: "Update one item in an Uber Eats draft cart",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			update := ubereats.CartItemUpdate{}
			if cmd.Flags().Changed("quantity") {
				update.Quantity = &quantity
			}
			if cmd.Flags().Changed("note") {
				noteValue := note
				update.Note = &noteValue
			}
			client := uberEatsClientFactory(st, uberEatsCommand{})
			result, err := client.UpdateCartItem(cmd.Context(), args[0], args[1], update)
			if err != nil {
				return err
			}
			return writeUberEatsCartMutation(cmd.OutOrStdout(), result, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&quantity, "quantity", 0, "replacement quantity")
	cmd.Flags().StringVar(&note, "note", "", "replacement special instructions")
	return cmd
}

func newUberEatsCartItemsRemoveCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "remove <cart-ref> <cart-item-ref>",
		Short: "Remove one item from an Uber Eats draft cart",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			result, err := client.RemoveCartItem(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return writeUberEatsCartMutation(cmd.OutOrStdout(), result, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsAddressesListCmd(st *state) *cobra.Command {
	var asJSON bool
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Uber Eats delivery addresses",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			locations, err := client.ListLocations(cmd.Context())
			if err != nil {
				return err
			}
			locations = savedUberEatsLocations(locations)
			locations = limitUberEatsLocations(locations, limit)
			return writeUberEatsLocations(cmd.OutOrStdout(), locations, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "max addresses to return")
	return cmd
}

func newUberEatsAddressesShowCmd(st *state) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <address-ref|default>",
		Short: "Show one Uber Eats delivery address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			location, err := resolveUberEatsLocation(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			instructionContext, err := client.GetInstructionContext(cmd.Context(), location)
			if err != nil {
				return err
			}
			return writeUberEatsAddressDetails(cmd.OutOrStdout(), uberEatsAddressDetails{
				Location:           location,
				InstructionContext: instructionContext,
			}, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsStoresCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stores",
		Short: "Inspect Uber Eats stores",
	}
	cmd.AddCommand(newUberEatsStoresListCmd(st))
	cmd.AddCommand(newUberEatsStoresSearchCmd(st))
	cmd.AddCommand(newUberEatsStoresShowCmd(st))
	cmd.AddCommand(newUberEatsStoresMenuCmd(st))
	return cmd
}

func newUberEatsStoresListCmd(st *state) *cobra.Command {
	var asJSON bool
	var filter string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Uber Eats stores",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			favoritesOnly := strings.EqualFold(strings.TrimSpace(filter), "favorites")
			if filter != "" && !favoritesOnly {
				return fmt.Errorf("unsupported store filter %q", filter)
			}
			stores, err := client.ListStores(cmd.Context(), favoritesOnly, limit)
			if err != nil {
				return err
			}
			return writeUberEatsStores(cmd.OutOrStdout(), stores, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&filter, "filter", "", "favorites")
	cmd.Flags().IntVar(&limit, "limit", 20, "max stores to return")
	return cmd
}

func newUberEatsStoresSearchCmd(st *state) *cobra.Command {
	var asJSON bool
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search Uber Eats stores",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			stores, err := client.SearchStores(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			return writeUberEatsStores(cmd.OutOrStdout(), stores, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&limit, "limit", 20, "max stores to return")
	return cmd
}

func newUberEatsStoresShowCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <store-ref>",
		Short: "Show one Uber Eats store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			store, err := client.GetStore(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeUberEatsStore(cmd.OutOrStdout(), store, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsStoresMenuCmd(st *state) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "menu <store-ref>",
		Short: "Show one Uber Eats store menu",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			menu, err := client.GetStoreMenu(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeUberEatsStoreMenu(cmd.OutOrStdout(), menu, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func newUberEatsItemsCmd(st *state) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "items",
		Short: "Inspect Uber Eats menu items",
	}
	cmd.AddCommand(newUberEatsItemsSearchCmd(st))
	cmd.AddCommand(newUberEatsItemsShowCmd(st))
	return cmd
}

func newUberEatsItemsSearchCmd(st *state) *cobra.Command {
	var asJSON bool
	var storeRef string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search store items within one Uber Eats store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			var (
				items []ubereats.StoreItem
				err   error
			)
			if strings.TrimSpace(storeRef) == "" {
				items, err = client.SearchItems(cmd.Context(), args[0], limit)
			} else {
				items, err = client.SearchStoreItems(cmd.Context(), storeRef, args[0], limit)
			}
			if err != nil {
				return err
			}
			return writeUberEatsStoreItems(cmd.OutOrStdout(), items, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&storeRef, "store", "", "store ref")
	cmd.Flags().IntVar(&limit, "limit", 20, "max items to return")
	return cmd
}

func newUberEatsItemsShowCmd(st *state) *cobra.Command {
	var asJSON bool
	var storeRef string
	cmd := &cobra.Command{
		Use:   "show <item-ref>",
		Short: "Show one Uber Eats menu item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := uberEatsClientFactory(st, uberEatsCommand{})
			item, err := client.GetMenuItem(cmd.Context(), storeRef, args[0])
			if err != nil {
				return err
			}
			return writeUberEatsItemDetail(cmd.OutOrStdout(), item, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&storeRef, "store", "", "store ref")
	_ = cmd.MarkFlagRequired("store")
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
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilterActive, 20, asJSON, watchInterval(st, interval, watch))
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
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilterPast, limit, asJSON, 0)
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
			return runUberEatsOrdersShow(cmd, st, args[0], asJSON, 0)
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
			return runUberEatsOrdersList(cmd, st, ubereats.OrderFilter(strings.TrimSpace(filter)), limit, asJSON, watchInterval(st, interval, watch))
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
			return runUberEatsOrdersShow(cmd, st, args[0], asJSON, watchInterval(st, interval, watch))
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&watch, "watch", false, "poll until interrupted")
	cmd.Flags().DurationVar(&interval, "interval", 0, "override polling interval")
	return cmd
}

func runUberEatsOrdersList(cmd *cobra.Command, st *state, filter ubereats.OrderFilter, limit int, asJSON bool, interval time.Duration) error {
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
		return writeUberEatsOrders(cmd.OutOrStdout(), orders, asJSON, "no orders")
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

func runUberEatsOrdersShow(cmd *cobra.Command, st *state, ref string, asJSON bool, interval time.Duration) error {
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
			return writeUberEatsOrder(cmd.OutOrStdout(), orders[0], asJSON)
		}
		order, err := client.GetOrder(cmd.Context(), ref)
		if err != nil {
			return err
		}
		return writeUberEatsOrder(cmd.OutOrStdout(), order, asJSON)
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

func writeUberEatsOrders(w io.Writer, orders []ubereats.Order, asJSON bool, emptyMessage string) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
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

func writeUberEatsLocations(w io.Writer, locations []ubereats.Location, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"items": locations,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	if len(locations) == 0 {
		_, err := fmt.Fprintln(w, "no addresses")
		return err
	}
	for _, location := range locations {
		if _, err := fmt.Fprintln(w, formatUberEatsLocation(location)); err != nil {
			return err
		}
	}
	return nil
}

type uberEatsAddressDetails struct {
	Location           ubereats.Location           `json:"location"`
	InstructionContext ubereats.InstructionContext `json:"instruction_context,omitempty"`
}

func writeUberEatsAddressDetails(w io.Writer, details uberEatsAddressDetails, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": details,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	lines := []string{formatUberEatsLocation(details.Location)}
	if details.InstructionContext.DefaultInteractionType != "" {
		lines = append(lines, "default_interaction_type="+details.InstructionContext.DefaultInteractionType)
	}
	if details.InstructionContext.PreferredInteractionType != "" {
		lines = append(lines, "preferred_interaction_type="+details.InstructionContext.PreferredInteractionType)
	}
	if details.InstructionContext.SelectedInstruction.InteractionType != "" {
		lines = append(lines, "selected_interaction_type="+details.InstructionContext.SelectedInstruction.InteractionType)
	}
	if len(details.InstructionContext.AvailableInteractionTypes) > 0 {
		lines = append(lines, "available_interaction_types="+strings.Join(details.InstructionContext.AvailableInteractionTypes, ","))
	}
	if strings.TrimSpace(details.InstructionContext.SelectedInstruction.DisplayString) != "" {
		lines = append(lines, "selected_instruction="+details.InstructionContext.SelectedInstruction.DisplayString)
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func writeUberEatsCarts(w io.Writer, carts []ubereats.Cart, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"items": carts,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	if len(carts) == 0 {
		_, err := fmt.Fprintln(w, "no carts")
		return err
	}
	for _, cart := range carts {
		if _, err := fmt.Fprintln(w, cartSummaryString(cart)); err != nil {
			return err
		}
	}
	return nil
}

func writeUberEatsCart(w io.Writer, cart ubereats.Cart, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": cart,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	lines := []string{
		"ref=" + cart.Ref,
	}
	appendLine := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, key+"="+value)
	}
	appendLine("cart_ref", cart.CartRef)
	appendLine("store_ref", cart.StoreRef)
	appendLine("store", cart.StoreTitle)
	appendLine("state", cart.State)
	appendLine("subtotal", cart.Subtotal)
	appendLine("total", cart.Total)
	appendLine("delivery_type", cart.DeliveryType)
	appendLine("interaction_type", cart.InteractionType)
	appendLine("checkout_ready", fmt.Sprintf("%t", cart.CheckoutReady))
	appendLine("fee_summary", cart.FeeSummary)
	appendLine("address", cart.Address)
	appendLine("location_source", cart.SessionInfo.LocationSource)
	appendLine("location_ref", cart.SessionInfo.LocationRef)
	appendLine("location", cart.SessionInfo.Location)
	appendLine("profile", cart.SessionInfo.Profile)
	paymentProfileRef := cart.PaymentProfileRef
	if strings.TrimSpace(paymentProfileRef) == "" {
		paymentProfileRef = cart.SessionInfo.PaymentProfileRef
	}
	appendLine("payment_profile_ref", paymentProfileRef)
	if cart.ItemCount > 0 {
		appendLine("item_count", fmt.Sprintf("%d", cart.ItemCount))
	}
	if len(cart.Items) > 0 {
		lines = append(lines, "items:")
		for _, item := range cart.Items {
			lines = append(lines, "  "+formatUberEatsCartItem(item))
		}
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func writeUberEatsCartMutation(w io.Writer, result ubereats.CartMutation, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": result,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	parts := []string{"ref=" + result.Ref}
	if result.Added {
		parts = append(parts, "added=true")
	}
	if result.Updated {
		parts = append(parts, "updated=true")
	}
	if result.Removed {
		parts = append(parts, "removed=true")
	}
	if result.Discarded {
		parts = append(parts, "discarded=true")
	}
	if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
		return err
	}
	if result.Cart != nil {
		return writeUberEatsCart(w, *result.Cart, false)
	}
	return nil
}

func writeUberEatsStore(w io.Writer, store ubereats.Store, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": store,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	parts := []string{
		"ref=" + store.Ref,
		"title=" + store.Title,
		"currency=" + store.CurrencyCode,
		fmt.Sprintf("orderable=%t", store.Orderable),
		fmt.Sprintf("favorite=%t", store.Favorite),
	}
	if store.Rating != 0 {
		parts = append(parts, fmt.Sprintf("rating=%.1f", store.Rating))
	}
	if strings.TrimSpace(store.RatingCount) != "" {
		parts = append(parts, "rating_count="+store.RatingCount)
	}
	if strings.TrimSpace(store.ETADisplay) != "" {
		parts = append(parts, "eta="+store.ETADisplay)
	}
	if strings.TrimSpace(store.FeeDisplay) != "" {
		parts = append(parts, "fee="+store.FeeDisplay)
	}
	_, err := fmt.Fprintln(w, strings.Join(parts, " "))
	return err
}

func writeUberEatsStores(w io.Writer, stores []ubereats.Store, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"items": stores,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	if len(stores) == 0 {
		_, err := fmt.Fprintln(w, "no stores")
		return err
	}
	for _, store := range stores {
		if err := writeUberEatsStore(w, store, false); err != nil {
			return err
		}
	}
	return nil
}

func writeUberEatsStoreMenu(w io.Writer, menu ubereats.StoreMenu, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": menu,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	lines := []string{
		"store_ref=" + menu.Store.Ref + " title=" + menu.Store.Title + " currency=" + menu.Store.CurrencyCode,
	}
	for _, section := range menu.Sections {
		line := "section_ref=" + section.Ref + " title=" + section.Title
		if strings.TrimSpace(section.Subtitle) != "" {
			line += " subtitle=" + section.Subtitle
		}
		lines = append(lines, line)
		for _, item := range section.Items {
			lines = append(lines, "  "+formatUberEatsStoreItem(item))
		}
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func writeUberEatsStoreItems(w io.Writer, items []ubereats.StoreItem, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"items": items,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "no items")
		return err
	}
	for _, item := range items {
		if _, err := fmt.Fprintln(w, formatUberEatsStoreItem(item)); err != nil {
			return err
		}
	}
	return nil
}

func writeUberEatsItemDetail(w io.Writer, item ubereats.ItemDetail, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"ok": true,
			"data": map[string]any{
				"item": item,
			},
			"meta": map[string]any{
				"provider": "ubereats",
			},
		})
	}
	lines := []string{formatUberEatsStoreItem(item.StoreItem)}
	for _, group := range item.Customizations {
		lines = append(lines, "customization_ref="+group.Ref+" title="+group.Title)
		for _, option := range group.Options {
			line := "  option_ref=" + option.Ref + " title=" + option.Title
			if price := formatUberEatsMinorMoney(option.PriceMinor, item.CurrencyCode); price != "" {
				line += " price=" + price
			}
			lines = append(lines, line)
		}
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func formatUberEatsLocation(location ubereats.Location) string {
	parts := []string{
		"ref=" + location.Ref,
		"source=" + location.Source,
	}
	if strings.TrimSpace(location.Label) != "" {
		parts = append(parts, "label="+location.Label)
	}
	if strings.TrimSpace(location.Title) != "" {
		parts = append(parts, "title="+location.Title)
	}
	if strings.TrimSpace(location.FullAddress) != "" {
		parts = append(parts, "address="+location.FullAddress)
	}
	return strings.Join(parts, " ")
}

func formatUberEatsStoreItem(item ubereats.StoreItem) string {
	parts := []string{
		"item_ref=" + item.Ref,
		"store_ref=" + item.StoreRef,
		"section_ref=" + item.SectionRef,
		"subsection_ref=" + item.SubsectionRef,
		"title=" + item.Title,
	}
	if strings.TrimSpace(item.Description) != "" {
		parts = append(parts, "description="+item.Description)
	}
	if item.PriceMinor > 0 {
		parts = append(parts, "price="+formatUberEatsMinorMoney(item.PriceMinor, item.CurrencyCode))
	}
	if item.SoldOut {
		parts = append(parts, "sold_out=true")
	}
	if item.HasCustomizations {
		parts = append(parts, "has_customizations=true")
	}
	return strings.Join(parts, " ")
}

func cartSummaryString(cart ubereats.Cart) string {
	parts := []string{
		"ref=" + cart.Ref,
	}
	if strings.TrimSpace(cart.CartRef) != "" {
		parts = append(parts, "cart_ref="+cart.CartRef)
	}
	if strings.TrimSpace(cart.StoreRef) != "" {
		parts = append(parts, "store_ref="+cart.StoreRef)
	}
	if strings.TrimSpace(cart.StoreTitle) != "" {
		parts = append(parts, "store="+cart.StoreTitle)
	}
	if cart.ItemCount > 0 {
		parts = append(parts, fmt.Sprintf("item_count=%d", cart.ItemCount))
	}
	if strings.TrimSpace(cart.Subtotal) != "" {
		parts = append(parts, "subtotal="+cart.Subtotal)
	}
	if strings.TrimSpace(cart.DeliveryType) != "" {
		parts = append(parts, "delivery_type="+cart.DeliveryType)
	}
	parts = append(parts, fmt.Sprintf("checkout_ready=%t", cart.CheckoutReady))
	if strings.TrimSpace(cart.FeeSummary) != "" {
		parts = append(parts, "fee_summary="+cart.FeeSummary)
	}
	if strings.TrimSpace(cart.Address) != "" {
		parts = append(parts, "address="+cart.Address)
	}
	return strings.Join(parts, " ")
}

func formatUberEatsCartItem(item ubereats.CartItem) string {
	parts := []string{
		"cart_item_ref=" + item.Ref,
		"item_ref=" + item.ItemRef,
		"title=" + item.Title,
	}
	if item.Quantity > 0 {
		parts = append(parts, fmt.Sprintf("quantity=%d", item.Quantity))
	}
	if strings.TrimSpace(item.Note) != "" {
		parts = append(parts, "note="+item.Note)
	}
	if price := formatUberEatsMinorMoney(item.PriceMinor, item.CurrencyCode); price != "" {
		parts = append(parts, "price="+price)
	}
	if total := formatUberEatsMinorMoney(item.TotalMinor, item.CurrencyCode); total != "" {
		parts = append(parts, "total="+total)
	}
	return strings.Join(parts, " ")
}

func limitUberEatsLocations(locations []ubereats.Location, limit int) []ubereats.Location {
	if limit <= 0 || len(locations) <= limit {
		return locations
	}
	return locations[:limit]
}

func savedUberEatsLocations(locations []ubereats.Location) []ubereats.Location {
	out := make([]ubereats.Location, 0, len(locations))
	for _, location := range locations {
		if location.Source == "SAVED" {
			out = append(out, location)
		}
	}
	return out
}

func formatUberEatsMinorMoney(minor int, currency string) string {
	if minor <= 0 {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USD":
		return fmt.Sprintf("$%.2f", float64(minor)/100)
	default:
		if strings.TrimSpace(currency) == "" {
			return fmt.Sprintf("%.2f", float64(minor)/100)
		}
		return fmt.Sprintf("%s %.2f", strings.ToUpper(strings.TrimSpace(currency)), float64(minor)/100)
	}
}

func resolveUberEatsLocation(ctx context.Context, client uberEatsClient, ref string) (ubereats.Location, error) {
	ref = strings.TrimSpace(ref)
	if strings.EqualFold(ref, "default") {
		return client.DefaultLocation(ctx)
	}
	locations, err := client.ListLocations(ctx)
	if err != nil {
		return ubereats.Location{}, err
	}
	for _, location := range locations {
		if location.Ref == ref {
			return location, nil
		}
	}
	return ubereats.Location{}, fmt.Errorf("address %q not found", ref)
}

func writeUberEatsOrder(w io.Writer, order ubereats.Order, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
	if err != nil {
		return "", nil, err
	}
	return managedDir, func() error { return nil }, nil
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
