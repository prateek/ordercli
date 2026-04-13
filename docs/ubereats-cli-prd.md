# Uber Eats CLI UX PRD

Status: Draft

Owner: Prateek + Codex

Last updated: 2026-04-12

## Summary

Uber Eats in `ordercli` should expose a small, stable, resource-oriented CLI built on top of a logged-in browser session.

This document defines the target-state Uber Eats provider contract.

The current repository may still expose a smaller or older Uber Eats surface. That existing implementation is not the source of truth for future Uber Eats work. This PRD is.

The interface should center on these nouns:

- `config`
- `login`
- `logout`
- `orders`
- `stores`
- `items`
- `carts`
- `addresses`

The interface should not expose internal implementation concepts such as feed pages, search-home payloads, or captured endpoint names.

Uber Eats is allowed to diverge from the repo-wide command shape when the Uber Eats interface is cleaner on its own terms.

## Reading Guide

- `Summary`, `Constraints`, `Design Principles`, and the namespace sections define the user-facing product contract.
- `CLI Interaction Pseudocode` defines the canonical command-to-workflow mapping.
- `Canonical Reference And Output Summary` defines the CLI-facing placeholder, ref, and output conventions used throughout the document.
- `Observed Web API Contract` defines the backend contract and request/response shapes.
- `Minimal Curl Examples` is illustrative and non-normative. If it disagrees with the observed contract above it, the observed contract wins.
- `CLI Examples` and `Draft Skill.md` are illustrative sections meant to show how the final interface should feel in real use.
- When two sections overlap, the more specific section wins:
  - namespace semantics beat examples
  - pseudocode beats prose summaries
  - endpoint contracts beat guessed backend behavior

## Constraints

- Uber Eats support is browser-session based.
- The durable secret store is the managed browser profile, not `config.json`.
- Uber Eats supports multiple saved addresses.
- Uber Eats supports multiple carts.
- Read operations and write operations need different safety boundaries.
- Human-readable output is the default.
- Structured JSON output must be available for automation and debugging.

## Design Principles

- Use stable product nouns.
- Keep one canonical path for each job.
- Default to safe reads and staged writes.
- Persist defaults that should survive process boundaries.
- Make debugging first-class.
- Keep the interface small.

## Shared Command Semantics

- `--limit` bounds the returned result set, not just the printed lines.
- Browser-backed list and search commands may perform multiple underlying interactions to satisfy `--limit`.
- `--watch` is a polling interface.
- `--watch` uses `default_watch_interval` unless `--interval` is supplied on the command line.
- `orders list --watch` monitors the active-order collection for new orders and state changes.
- `orders show <order-ref> --watch` monitors one specific order for state changes.
- Watch commands continue until user interruption, such as `Ctrl-C`, or command failure.

## Canonical Reference And Output Summary

The command surface uses canonical refs throughout this document.

- `order-ref` means the Uber order UUID.
- `cart-ref` means the Uber `draftOrderUUID`.
- `cart-item-ref` means the Uber `shoppingCartItemUuid`.
- `store-ref` means the Uber `storeUuid`.
- `item-ref` means the Uber menu-item UUID.
- `group-ref` means the Uber customization-group UUID.
- `option-ref` means the Uber customization-option UUID.

Human-readable output is the default.

- Default stdout is human-oriented.
- `--json` switches stdout to machine-oriented JSON.
- Diagnostics, warnings, and `--trace` output go to stderr only.
- List commands return arrays under `data.items`.
- Show commands return a single object under `data.item`.
- Money uses normalized objects with `currency_code`, `amount_minor`, and `display`.
- Timestamps use RFC 3339 strings.

## Capability Boundaries

This document is intentionally self-contained, but not every Uber Eats behavior is fully locked yet.

- Address override and address selection behavior are deferred.
- Group-cart item removal is named and partially understood, but only normal-cart removal is live-validated.
- Invoice enrichment is understood only enough to expose invoice presence; fuller invoice states still need more captures.
- The store, item, and cart surfaces are part of the product contract even when some lower-level backend details remain under `Remaining Unknowns`.

## Required Persistent Config State

Uber Eats config exists to hold durable local state for the CLI. It is not the primary credential store.

The provider config must support:

- `base_url`
- `browser_profile`
- `default_watch_interval`
- `debug`

### Config Semantics

- `browser_profile` points at the CLI-managed browser profile directory that holds the live Uber Eats session.
- The `browser_profile` recorded in Uber Eats config is treated as the CLI-managed profile for lifecycle operations such as `logout`.
- `base_url` identifies the supported Uber Eats host to use.
- `default_watch_interval` controls polling cadence for watch flows.
- `debug` controls whether redacted JSON tracing is emitted to stderr by default.

### Config Commands

```sh
ordercli ubereats config show
ordercli ubereats config set --browser-profile <path>
ordercli ubereats config set --base-url https://www.ubereats.com
ordercli ubereats config set --watch-interval 15s
ordercli ubereats config set --debug on
ordercli ubereats config set --debug off
```

## Login And Logout

The CLI must expose:

```sh
ordercli ubereats login
ordercli ubereats logout
ordercli ubereats logout --yes
```

### Login State

- `login` opens a real browser and waits until the Uber Eats session is authenticated.
- The managed browser profile becomes the active Uber Eats session store for later commands.
- `login` succeeds only after the CLI can use that session to call `getUserV1` and observe a successful logged-in response.
- After successful login, the CLI should be able to resolve the effective default delivery location, either immediately or on the first later location-aware command.

### Logout State

- `logout` clears the entire local Uber Eats session state managed by the CLI.
- `logout` removes the CLI-managed browser profile directory recorded in Uber Eats config.
- `logout` clears cached Uber Eats state and config values that depend on that session.
- `logout` prompts before deletion in interactive mode.
- `logout --yes` is required for non-interactive deletion.

## Session And Location Model

The Uber Eats provider is browser-backed, but only `login` requires an interactive visible browser.

The normal command path is:

- reuse the CLI-managed browser profile
- run later commands headlessly or otherwise non-interactively
- issue session-authenticated `/_p/api/` requests through that managed Uber session

The CLI should not treat raw address strings as the canonical source of location metadata.

The canonical source of delivery coordinates is Uber's own saved delivery-location object.

The source order for location context is:

1. the effective location object returned by `getDeliveryLocationsV2`
2. the persisted session location cookie, such as `uev2.loc`, inside the CLI-managed browser profile
3. external geocoding only as an emergency fallback when the Uber session cannot provide coordinates

The canonical coordinate fields are:

- `location.coordinate.latitude`
- `location.coordinate.longitude`

The CLI should derive request headers from that Uber location object rather than geocoding the human-readable address text.

The CLI should not require a visible browser window after login, but it does require the managed Uber session profile.

## Orders Namespace

Orders are one resource family. Active orders, past orders, and single-order detail should live under the same namespace.

### Commands

```sh
ordercli ubereats orders list
ordercli ubereats orders list --filter active
ordercli ubereats orders list --filter past
ordercli ubereats orders list --filter all
ordercli ubereats orders list --limit 20
ordercli ubereats orders list --watch
ordercli ubereats orders list --watch --interval 15s
ordercli ubereats orders show <order-ref>
ordercli ubereats orders show <order-ref> --watch
ordercli ubereats orders show <order-ref> --watch --interval 15s
```

### Orders Semantics

- `orders list` defaults to `--filter active`.
- `--filter` selects the slice of the order collection to display.
- `orders list --watch` is valid only with `--filter active`.
- `orders show <order-ref>` is the canonical single-order detail command.
- `orders show <order-ref> --watch` refreshes one order until user interruption or command failure.
- The canonical interface does not require a special `latest` sentinel.
- Users who want the newest items can use `orders list --filter past --limit N`.

## Stores Namespace

`stores` is the user-facing discovery noun.

### Commands

```sh
ordercli ubereats stores list
ordercli ubereats stores list --filter favorites
ordercli ubereats stores list --limit 20
ordercli ubereats stores search "<query>"
ordercli ubereats stores search "<query>" --limit 20
ordercli ubereats stores show <store-ref>
ordercli ubereats stores menu <store-ref>
```

### Stores Semantics

- `stores list` shows stores available for the effective address.
- `stores list --filter favorites` shows the user's favorite stores.
- `stores search "<query>"` is the query-driven discovery flow.
- `stores show <store-ref>` is the summary view for one store.
- `stores menu <store-ref>` is the browsing view for one store's menu.
- `stores menu <store-ref>` should normalize to one store plus an ordered section list.
- Each normalized menu section should include:
  - section UUID
  - title
  - subtitle when available
  - item summaries when available from the observed store payload

## Items Namespace

`items` is the cross-store discovery noun for menu items.

### Commands

```sh
ordercli ubereats items search "<query>"
ordercli ubereats items search "<query>" --store <store-ref>
ordercli ubereats items search "<query>" --limit 20
ordercli ubereats items show <item-ref> --store <store-ref>
```

### Items Semantics

- `items search "<query>"` searches menu items across stores for the effective address.
- `items search "<query>" --store <store-ref>` narrows the search to one store.
- `items search "<query>" --store <store-ref>` is implemented as a local filter over that store's catalog payload from `getStoreV1`.
- Store-scoped item search should match against item title and item description when available.
- Store-scoped item search should dedupe repeated merchandising placements by `(store-ref, item-ref)`.
- `items show <item-ref> --store <store-ref>` is the canonical item-detail command.
- Item search results should include enough store context to make the next step obvious.
- Each normalized item-search result should include:
  - item ref when available
  - item title
  - item description when available
  - store ref
  - store title
  - section ref when available
  - subsection ref when available
  - price when available
  - enough context to route into `stores show`, `stores menu`, or cart creation
- `items show` should include:
  - item ref
  - store ref
  - section ref
  - subsection ref
  - title
  - description
  - price
  - sold-out state
  - customization groups and options when present

## Carts Namespace

Carts are first-class because Uber Eats can hold more than one cart at a time.

### Commands

```sh
ordercli ubereats carts list
ordercli ubereats carts list --limit 20
ordercli ubereats carts show <cart-ref>
ordercli ubereats carts create --from-order <order-ref>
ordercli ubereats carts create --store <store-ref> --item <item-ref>
ordercli ubereats carts create --store <store-ref> --item <item-ref> --quantity 2
ordercli ubereats carts create --store <store-ref> --item <item-ref> --customization <group-ref>:<option-ref>:<qty>
ordercli ubereats carts items add <cart-ref> --item <item-ref>
ordercli ubereats carts items add <cart-ref> --item <item-ref> --quantity 2
ordercli ubereats carts items add <cart-ref> --item <item-ref> --note "extra salsa"
ordercli ubereats carts items add <cart-ref> --item <item-ref> --customization <group-ref>:<option-ref>:<qty>
ordercli ubereats carts items update <cart-ref> <cart-item-ref> --quantity 2
ordercli ubereats carts items update <cart-ref> <cart-item-ref> --note "no onions"
ordercli ubereats carts items update <cart-ref> <cart-item-ref> --customization <group-ref>:<option-ref>:<qty>
ordercli ubereats carts items remove <cart-ref> <cart-item-ref>
ordercli ubereats carts update <cart-ref> --delivery-type regular
ordercli ubereats carts update <cart-ref> --delivery-type premium
ordercli ubereats carts update <cart-ref> --interaction-type leave_at_door
ordercli ubereats carts discard <cart-ref>
ordercli ubereats carts checkout <cart-ref>
ordercli ubereats carts checkout <cart-ref> --confirm
```

### Carts Semantics

- `carts list` shows carts in the current Uber Eats session.
- `carts show <cart-ref>` shows one cart's contents, merchant, address, totals, checkout readiness, delivery type, interaction type, and effective session context.
- `carts create --from-order <order-ref>` is the canonical reorder flow.
- `carts create --store <store-ref> --item <item-ref>` is the canonical new-cart flow from store or item discovery.
- Cart creation does not require `--confirm`.
- `carts create` is a safe staged write because it does not submit an order or commit payment.
- `carts create` creates a new draft cart, then prints the resulting cart.
- Base cart creation requires at least one seed item. Empty-cart creation is not part of the canonical interface.
- `carts items add` grows an existing cart with another store item.
- `carts items update` mutates one existing cart line.
- `carts items remove` removes one existing cart line.
- `carts update` mutates cart-level delivery settings without placing the order.
- `carts discard` removes one draft cart without checking out.
- `carts items add` is valid only when the target item belongs to the same store as the target cart.
- `carts items add` and `carts create --store ... --item ...` should accept `--quantity`, repeated `--customization <group-ref>:<option-ref>:<qty>`, and optional `--note`.
- `carts items update` should replace quantity, note, and customization state for the targeted cart line when those flags are supplied.
- `carts items remove` must not be implemented as `updateItemInDraftOrderV2` with `quantity: 0`.
- `carts items remove` should call `removeItemsFromDraftOrderV2` for normal carts and `removeItemsFromGroupDraftOrderV2` for group carts.
- `carts items remove` accepts one or more `shoppingCartItemUUIDs` internally even though the human CLI surface removes one cart line per invocation.
- removing the last cart line should be treated as a cart-collapse case and refreshed through `getCartsViewForEaterUuidV1` rather than assuming `getDraftOrderByUuidV1` will still return a readable cart body.
- `carts update --delivery-type regular` maps to Uber's observed `ASAP` draft-order mode.
- `carts update --delivery-type premium` maps to Uber's observed `PREMIUM_DELIVERY` draft-order mode.
- `carts update --interaction-type <value>` should use values exposed by `getInstructionForLocationV1`.
- `carts checkout <cart-ref>` is a dry-run mode that does not place the order.
- Dry-run checkout loads the target cart, confirms that the cart exists, and shows what the CLI is about to check out.
- `carts show` and checkout dry run must surface the effective write context, including the resolved delivery location and the active Uber profile when that profile context is available.
- `carts show` and checkout dry run should also expose the active payment-profile ref when the upstream payload makes it visible.
- Dry-run checkout is valuable even without full backend validation.
- `carts checkout <cart-ref> --confirm` performs the live checkout.
- Checkout stays under `carts checkout`, not under a separate top-level command.

### Reorder Semantics

There is no separate `reorder` command in the canonical interface.

Reordering means creating a cart from a prior order:

```sh
ordercli ubereats carts create --from-order <order-ref>
```

## Addresses Namespace

Addresses are first-class because Uber Eats supports multiple saved addresses and the effective address changes store availability, carts, and checkout.

### Commands

```sh
ordercli ubereats addresses list
ordercli ubereats addresses list --limit 20
ordercli ubereats addresses show default
```

### Address Semantics

- `addresses list` shows the saved Uber Eats addresses.
- `addresses show default` shows the effective default address.
- Address override and address selection behavior are deferred and will be specified later.

### Default Address Behavior

The effective default address is the default saved address from Uber Eats.

The CLI should not depend on device GPS or local geolocation to choose a default address.

The implementation should resolve the effective default location in this order:

1. first `TARGET` location from `getDeliveryLocationsV2`, if present
2. else the default saved address from Uber Eats
3. else the first `SUGGESTED` location

## CLI Interaction Pseudocode

This section is the canonical command-to-workflow mapping for the Uber Eats provider.

### `ordercli ubereats login`

```text
1. Launch the CLI-managed browser profile at the Uber Eats base URL.
2. Wait for the session to become authenticated.
3. Call getUserV1 through that browser session.
4. Fail if getUserV1 does not report a logged-in session.
5. Optionally warm the effective default delivery location by calling getDeliveryLocationsV2.
6. Persist the CLI-managed browser profile path in config.
```

### `ordercli ubereats logout`

```text
1. Read the configured CLI-managed browser profile path.
2. Confirm deletion unless --yes is supplied.
3. Remove the profile directory.
4. Clear cached Uber Eats session-dependent config state.
5. Exit successfully even if the profile was already absent.
```

### `ordercli ubereats orders list`

```text
1. Ensure a logged-in browser session and effective default location.
2. If --filter active, call getActiveOrdersV1.
3. If --filter past, page getPastOrdersV1 until --limit is satisfied or the cursor ends.
4. If --filter all, merge active and past orders and dedupe by order UUID.
5. Normalize, sort, and emit.
6. If --watch, repeat the active-order path on the polling interval until interrupted.
```

### `ordercli ubereats orders show <order-ref>`

```text
1. Call getActiveOrdersV1 and match by order UUID.
2. If not found, page getPastOrdersV1 until the order is found or pagination ends.
3. Optionally call getInvoiceStatusV1 for invoice enrichment.
4. Normalize the single order and emit.
5. If --watch, poll getActiveOrdersV1 on the interval until interrupted or the order disappears.
```

### `ordercli ubereats stores list`

```text
1. Ensure a logged-in browser session and effective default location.
2. Call getFeedV1 with the favorites flag set appropriately for the requested view.
3. Normalize store-bearing feed cards into store rows.
4. If --filter favorites, prefer the favorites-oriented feed view and favorites map.
5. Emit stores up to --limit.
```

### `ordercli ubereats stores search "<query>"`

```text
1. If the query is empty, call getSearchHomeV2.
2. If the query is non-empty, call getSearchSuggestionsV1, then call getSearchFeedV1.
3. Normalize store-bearing feed cards into store rows.
4. Emit stores up to --limit.
```

### `ordercli ubereats stores show <store-ref>`

```text
1. Call getStoreV1 for the target store.
2. Normalize store identity, orderability, hours, ETA, fee, rating, and summary catalog state.
3. Emit one store summary object.
```

### `ordercli ubereats stores menu <store-ref>`

```text
1. Call getStoreV1 for the target store.
2. Read catalogSectionsMap and sections.
3. Normalize ordered menu sections plus item summaries embedded in the catalog payload.
4. Emit one store-menu object with sections and items.
```

### `ordercli ubereats items search "<query>"`

```text
1. If --store is absent, call getSearchFeedV1 and normalize item-bearing feed cards.
2. If --store is present, call getStoreV1 and locally filter catalog items by title and description.
3. Dedupe store-scoped results by (store-ref, item-ref) because Uber repeats promoted items across sections.
4. Emit item rows with store ref, section ref, subsection ref, and price when available.
```

### `ordercli ubereats items show <item-ref> --store <store-ref>`

```text
1. Resolve the item's section ref and subsection ref from the store catalog if they are not already cached.
2. Call getMenuItemV1 with store, section, subsection, and menu item UUID.
3. Normalize item detail, sold-out state, and customization groups/options.
4. Emit one item-detail object.
```

### `ordercli ubereats carts list`

```text
1. Call getCartsViewForEaterUuidV1 for the lightweight cart list.
2. If richer cart fields are needed, supplement with getDraftOrdersByEaterUuidV1.
3. Normalize cart summaries and emit up to --limit.
```

### `ordercli ubereats carts show <cart-ref>`

```text
1. Call getDraftOrderByUuidV1 with the draft-order UUID.
2. Resolve the effective delivery location via getDeliveryLocationsV2.
3. Optionally resolve the active Uber profile via getProfilesForUserV1 when profile context is available.
4. Normalize merchant, cart lines, totals, delivery address, delivery type, interaction type, readiness state, and session context.
5. Emit one cart object.
```

### `ordercli ubereats carts create --from-order <order-ref>`

```text
1. Resolve the source order via getPastOrdersV1.
2. Extract source line items, prices, quantities, store ref, section refs, subsection refs, and customizations.
3. Resolve the effective default delivery location and default payment profile.
4. Call createDraftOrderV2 with the reconstructed shoppingCartItems payload to create a new draft cart.
5. If needed, reconcile cart-level state with updateDraftOrderV2.
6. Emit the resulting cart via getDraftOrderByUuidV1 or the create response payload.
```

### `ordercli ubereats carts create --store <store-ref> --item <item-ref>`

```text
1. Resolve the item's section ref and subsection ref from the store catalog or item cache.
2. Build one shoppingCartItems entry from the item ref, quantity, customizations, and note.
3. Resolve the effective default delivery location and default payment profile.
4. Call createDraftOrderV2 with that single seeded cart line to create a new draft cart.
5. Emit the resulting cart.
```

### `ordercli ubereats carts items add <cart-ref> --item <item-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1.
2. Verify the target item belongs to the same store as the target cart.
3. Resolve section and subsection refs for the item.
4. Build one items[] entry from the item ref, quantity, customizations, and note.
5. Call addItemsToDraftOrderV2.
6. Emit the updated cart.
```

### `ordercli ubereats carts items update <cart-ref> <cart-item-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1.
2. Locate the target shopping cart line by shoppingCartItemUuid.
3. Apply quantity, note, and customization changes to the full line-item object.
4. Call updateItemInDraftOrderV2.
5. Emit the updated cart.
```

### `ordercli ubereats carts items remove <cart-ref> <cart-item-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1.
2. Call removeItemsFromDraftOrderV2 with cartUUID, draftOrderUUID, storeUUID, and shoppingCartItemUUIDs.
3. Do not attempt removal by sending quantity zero to updateItemInDraftOrderV2, because Uber rejects non-positive quantities.
4. Refresh carts view.
5. If the draft order still exists, emit the updated cart.
6. If the removed line was the last line and the draft disappears from carts view, emit a cart-removed state instead of trying to treat the cart as still readable.
```

### `ordercli ubereats carts update <cart-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1.
2. Apply cart-level changes such as delivery type or interaction type.
3. Map --delivery-type regular to ASAP.
4. Map --delivery-type premium to PREMIUM_DELIVERY.
5. Validate interaction-type values against getInstructionForLocationV1 when needed.
6. Call updateDraftOrderV2.
7. Emit the updated cart.
```

### `ordercli ubereats carts discard <cart-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1 to recover its store ref.
2. Call discardDraftOrdersV1 with the draft-order UUID and store UUID.
3. Emit success with the discarded cart ref.
```

### `ordercli ubereats carts checkout <cart-ref>`

```text
1. Read the target cart via getDraftOrderByUuidV1.
2. Resolve the effective delivery location via getDeliveryLocationsV2.
3. Optionally resolve the active Uber profile via getProfilesForUserV1 when profile context is available.
4. Call getCheckoutPresentationV1 with a stable payloadTypes set for preview.
5. Normalize checkout payloads plus effective session context into a dry-run preview.
6. If --confirm is absent, stop here and emit the preview.
7. If --confirm is present, call checkoutOrdersByDraftOrdersV1 and emit the resulting order state.
```

### `ordercli ubereats addresses list`

```text
1. Call getDeliveryLocationsV2.
2. Normalize SAVED, TARGET, and SUGGESTED delivery locations.
3. Emit the address list up to --limit.
```

### `ordercli ubereats addresses show default`

```text
1. Call getDeliveryLocationsV2.
2. Resolve the effective default location by TARGET, then default saved address, then first SUGGESTED.
3. Call getInstructionForLocationV1 for that location.
4. Emit the effective default address plus delivery-instruction context.
```

## Observed Web API Contract

This section records the observed Uber Eats private web API used by the browser experience.

The contract below is not an official API. It is an implementation appendix for this CLI. It exists so an LLM or engineer can implement the Uber Eats provider from one document without separately mining HAR files or source notes.

This appendix is the backend source of truth. The later curl section is illustrative and should be read as worked examples, not as a second competing contract.

### Scope Of This Appendix

- This appendix covers the session-authenticated JSON web API surface.
- `login` and `logout` remain browser-lifecycle features and are not expressed as API endpoints here.
- The CLI should treat this API as private, session-bound, and subject to change.
- All command sections above remain the product surface. This appendix is the backend contract they rely on.

### Shared Transport Contract

- Base origin: `https://www.ubereats.com`
- Observed API prefix: `/_p/api/`
- Method: JSON `POST` for every endpoint in this appendix
- Session model: browser-managed cookies plus CSRF token
- Observed useful headers:
  - `content-type: application/json`
  - `x-csrf-token`
  - `x-uber-device-location-latitude`
  - `x-uber-device-location-longitude`
  - `x-uber-target-location-latitude`
  - `x-uber-target-location-longitude`
  - `x-uber-session-id`
  - `x-uber-ciid`
  - `x-uber-client-gitref`
  - `referer`
- Common top-level response envelope:
  - `status`
  - `data`

The browser session is part of the transport contract. Requests are not anonymous and should not be modeled as stateless public API calls.

Read endpoints sometimes tolerate a smaller header set. Cart and checkout mutations should use the richer header set observed from the live web client.

### Request Context Derivation

- Supported host set for this PRD: `https://www.ubereats.com`
- The implementation should send `x-csrf-token: x` unless later captures prove a stronger requirement.
- The implementation should derive `x-uber-device-location-latitude` and `x-uber-device-location-longitude` from the effective default delivery location returned by `getDeliveryLocationsV2`.
- The implementation should derive `x-uber-target-location-latitude` and `x-uber-target-location-longitude` from the same effective Uber location object.
- The implementation should prefer Uber-provided coordinates over any external geocoding result.
- The implementation may read `uev2.loc` from the managed browser profile as a fallback location source when `getDeliveryLocationsV2` is not yet available.
- The implementation should refresh the effective Uber location when the session changes or when Uber returns a different `TARGET` location.

### CLI To Contract Mapping

- `orders` uses `Orders API`
- `stores` uses `Discovery API` and `Store Catalog API`
- `items` uses `Discovery API` and `Store Catalog API`
- `carts` uses `Draft Order And Cart API`
- `addresses` uses `Address And Session Context API`

### Orders API

#### `POST /_p/api/getActiveOrdersV1`

Used by:

- `orders list --filter active`
- `orders show <order-ref> --watch`

Observed request body:

```json
{
  "orderUuid": null,
  "timezone": "America/New_York",
  "showAppUpsellIllustration": true,
  "isDirectTracking": false
}
```

Observed response contract:

- `data.orders` is an array
- sampled live response returned an empty array when no active orders existed

Implementation note:

- This is the pollable active-order read path.
- `orders list --watch` should repeatedly call this endpoint.

#### `POST /_p/api/getPastOrdersV1`

Used by:

- `orders list --filter past`
- `orders show <order-ref>`
- `carts create --from-order <order-ref>` as the source order lookup

Observed request bodies:

```json
{"lastWorkflowUUID":""}
```

```json
{"limit":1}
```

Observed response contract:

- `data.ordersMap`
- `data.orderUuids`
- `data.paginationData.nextCursor`
- `data.meta.hasMore`

Observed order fields include:

- lifecycle timestamps such as `completedAt` and `lastStateChangeAt`
- `orderStateChanges`
- `deliveryStateChanges`
- store metadata
- fare metadata
- shopping cart contents and pricing

Implementation note:

- This is the canonical historical-order source.
- The CLI should treat it as cursor-capable and use `nextCursor` or the equivalent server-side cursor field when satisfying larger `--limit` values.

#### `POST /_p/api/getInvoiceStatusV1`

Used by:

- optional receipt or invoice enrichment in `orders show`

Observed request body:

```json
{
  "orderUUID": "7723a4f6-7cce-4b29-a4d8-fc98f5ee3a6f"
}
```

Observed response contract:

- `data.invoiceStatus`

Sampled live value:

```json
{
  "invoiceStatus": "EMPTY"
}
```

Implementation note:

- This endpoint is useful for invoice presence, not for the primary order-detail read.
- More invoice states still need additional captures.

#### `POST /_p/api/getOrderEntitiesV1`

Used by:

- optional order enrichment if Uber later populates it

Observed request body:

```json
{}
```

Observed response contract:

- `data.orderEntities`
- `data.orderEntitiesView`

Sampled captured response was effectively empty.

Implementation note:

- Do not make this a hard dependency for base order detail.

#### Single-Order Lookup Strategy

`orders show <order-ref>` should resolve a target order in this order:

1. call `getActiveOrdersV1` and match the target UUID against `data.orders`
2. if not found, page through `getPastOrdersV1`
3. once the base order is found, optionally enrich with `getInvoiceStatusV1`
4. ignore `getOrderEntitiesV1` unless it begins returning useful data

### Discovery API

#### `POST /_p/api/getFeedV1`

Used by:

- `stores list`
- `stores list --filter favorites`

Observed request body:

```json
{
  "cacheKey": "/DELIVERY///0/0//JTVCJTVE/undefined///true///HOME////////",
  "feedSessionCount": {
    "announcementCount": 0,
    "announcementLabel": ""
  },
  "userQuery": "",
  "date": "",
  "startTime": 0,
  "endTime": 0,
  "carouselId": "",
  "sortAndFilters": [],
  "billboardUuid": "",
  "feedProvider": "",
  "promotionUuid": "",
  "targetingStoreTag": "",
  "venueUUID": "",
  "selectedSectionUUID": "",
  "favorites": "true",
  "vertical": "",
  "searchSource": "",
  "searchType": "",
  "keyName": "",
  "serializedRequestContext": "",
  "isUserInitiatedRefresh": false
}
```

Observed response contract:

- `data.cacheKey`
- `data.feedItems`
- `data.favorites`

Observed feed behavior:

- sampled live response returned 137 `feedItems`
- the first sampled item was a `SECTION_HEADER`
- the feed then returned store-oriented cards such as `MINI_STORE`
- `data.favorites` was an object keyed by store UUID
- the sampled favorites-oriented response began with a section titled `More favorites`

Implementation note:

- This is the strongest observed source for nearby and favorite store listing.
- Treat `feedItems` as a heterogeneous feed, not as a pure array of stores.

#### `POST /_p/api/getSearchHomeV2`

Used by:

- zero-query discovery behind `stores search`
- future search bootstrap and suggestion UI

Observed request body:

```json
{
  "dropPastOrders": true
}
```

Observed response contract:

- `data.searchHistory`
- `data.suggestedSections`
- `data.verticalSuggestionLists`
- `data.verticalSearchHomeResults`
- `data.browseHomeFeed`

Observed values:

- `verticalSuggestionLists` included `ALL`, `RESTAURANTS`, `SHOP`, and `ALCOHOL`
- `verticalSearchHomeResults` contained mixed recent searches and store suggestions
- `browseHomeFeed` was an array of browse-home sections

Implementation note:

- This endpoint is the best observed source for empty-query or pre-query search UX.

#### `POST /_p/api/getSearchSuggestionsV1`

Used by:

- `stores search "<query>"`
- `items search "<query>"`

Observed request body:

```json
{
  "userQuery": "gummy bears",
  "date": "",
  "startTime": 0,
  "endTime": 0,
  "vertical": "ALL"
}
```

Observed response contract:

- `data` is an array, not an object

Observed suggestion row types:

- `searchHistoryV2`
- `textV2`
- `searchTextV2`

Implementation note:

- This is the typeahead layer, not the final result feed.

#### `POST /_p/api/getSearchFeedV1`

Used by:

- `stores search "<query>"`
- `items search "<query>"`

Observed request body:

```json
{
  "userQuery": "gummy bears",
  "date": "",
  "startTime": 0,
  "endTime": 0,
  "sortAndFilters": [],
  "vertical": "ALL",
  "searchSource": "SEARCH_SUGGESTION",
  "displayType": "SEARCH_RESULTS",
  "searchType": "GLOBAL_SEARCH",
  "keyName": "",
  "cacheKey": "",
  "recaptchaToken": ""
}
```

Observed response contract:

- `data.favorites`
- `data.feedItems`
- `data.storesMap`
- `data.meta`
- `data.currencyCode`
- `data.title`
- `data.subtitle`
- `data.cacheKey`
- `data.sortAndFilters`
- `data.diningModes`
- `data.searchDisplayConfig`
- `data.availableVerticals`
- `data.feedHeader`

Observed result behavior:

- `feedItems` is heterogeneous
- the first sampled result was `MINI_STORE_WITH_ITEMS`
- `meta` included pagination-like fields such as `offset` and `hasMore`
- `subtitle` carried the result count as human text
- `sortAndFilters` returned structured filter definitions

Implementation notes:

- This endpoint is the canonical search results feed.
- Item search is currently best modeled as a filtered view over this mixed feed, not as a dedicated item-only endpoint.
- `items search --store <store-ref>` should not use this endpoint unless later captures prove a dedicated store-scoped search variant.
- Store-scoped item search is viable today as a local filter over `getStoreV1` catalog items, with dedupe by item UUID because the same item may appear in more than one merchandising placement.

### Store Catalog API

#### `POST /_p/api/getStoreV1`

Used by:

- `stores show <store-ref>`
- `stores menu <store-ref>`

Observed request body:

```json
{
  "storeUuid": "58d90f4b-db74-4e7d-8fa8-c34976758192",
  "diningMode": "DELIVERY",
  "time": {
    "asap": true
  },
  "cbType": "EATER_ENDORSED"
}
```

Observed response contract:

- store identity: `title`, `uuid`, `slug`, `citySlug`
- storefront state: `isOrderable`, `isOpen`, `hours`, `supportedDiningModes`, `isFavorite`
- pricing and delivery: `currencyCode`, `etaRange`, `fareInfo`, `distanceBadge`
- catalog structure: `sections`, `subsectionsMap`, `catalogSectionsMap`, `featuredItemsSections`
- merchandising: `promotion`, `storeBanners`, `exploreMoreStores`, `storeFrontActionPills`
- review and metadata surfaces

Observed section structure:

- section rows include fields such as `uuid`, `title`, `subtitle`, `subsectionUuids`, `isTop`, and `isOnSale`
- catalog item rows under `catalogSectionsMap[*].payload.standardItemsPayload.catalogItems` include fields such as `uuid`, `sectionUuid`, `subsectionUuid`, `title`, `itemDescription`, `price`, `isSoldOut`, and `hasCustomizations`

Implementation note:

- This endpoint is sufficient for store summary and menu-section browsing.
- `stores menu <store-ref>` should render sections first and use `getMenuItemV1` only when item-level expansion is needed.

#### `POST /_p/api/getMenuItemV1`

Used by:

- `stores menu <store-ref>` for item expansion
- `carts create` and other cart flows when item customization detail is needed

Observed request body:

```json
{
  "itemRequestType": "ITEM",
  "storeUuid": "58d90f4b-db74-4e7d-8fa8-c34976758192",
  "sectionUuid": "18759cd0-c287-516d-b54c-f3aea29a0ad7",
  "subsectionUuid": "00770077-f27b-597a-88fc-fa6823166490",
  "menuItemUuid": "1d389bcd-f983-598f-a5ea-3c613934d4be",
  "cbType": "EATER_ENDORSED",
  "includeCheaperAlternatives": false,
  "contextReferences": [
    {
      "type": "GROUP_ITEMS",
      "payload": {
        "type": "groupItemsContextReferencePayload",
        "groupItemsContextReferencePayload": {
          "catalogSectionUUID": ""
        }
      },
      "pageContext": "STORE"
    }
  ]
}
```

Observed response contract:

- base fields: `itemDescription`, `price`, `title`, `uuid`, `isSoldOut`
- media: `imageUrl`, `images`
- customization surface: `customizationsList`, `hasCustomizations`
- product metadata: `productDetailsItems`, `purchaseInfo`, `itemAttributeInfo`
- upsell and cross-sell fields

Observed customization structure:

- customization groups include `title`, `uuid`, `maxPermitted`, `minPermitted`, and `options`
- options include `uuid`, `title`, `price`, `defaultQuantity`, `childCustomizationList`, and `isSoldOut`

Implementation note:

- This endpoint is the authoritative source for menu item options and nested customizations.
- The implementation should use `getStoreV1` for catalog discovery and local filtering, then call `getMenuItemV1` only for items that need full detail or customization metadata.

### Draft Order And Cart API

#### `POST /_p/api/getCartsViewForEaterUuidV1`

Used by:

- `carts list`

Observed request body:

```json
{}
```

Observed response contract:

- `data.cartsView.carts`

Observed cart summary fields:

- `draftOrderUUID`
- `title`
- `tagline1`
- `tagline2`
- `storeImageUrls`
- `itemCount`
- `action`
- `metadata`
- `actions`

Implementation note:

- This is the cart list surface.

#### `POST /_p/api/getDraftOrdersByEaterUuidV1`

Used by:

- `carts show <cart-ref>`
- cart enumeration when the CLI needs richer per-cart state than `getCartsViewForEaterUuidV1`

Observed request body:

```json
{
  "removeAdapters": true
}
```

Observed response contract:

- `data.draftOrders`
- `data.cartsView`

Implementation note:

- This endpoint is a richer collection read than the cart-summary view.

#### `POST /_p/api/getDraftOrderByUuidV1`

Used by:

- `carts show <cart-ref>`
- `carts checkout <cart-ref>`

Observed request body:

```json
{
  "draftOrderUuid": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad"
}
```

Observed response contract:

- draft-order identity and lifecycle fields such as `uuid`, `state`, `createdAt`, `expiresAt`
- `shoppingCart`
- `deliveryAddress`
- `paymentProfileUUID`
- `promotionOptions`
- `interactionType`
- `targetDeliveryTimeRange`
- `diningMode`

Observed shopping-cart fields include:

- `cartUuid`
- `items`
- `groupedItems`
- `lastModifiedTimestamp`
- `currencyCode`
- `isInDraftOrder`
- `isOwner`
- `isActive`
- `shouldPoll`

Implementation note:

- This is the canonical cart-detail endpoint.

#### `POST /_p/api/createDraftOrderV2`

Used by:

- `carts create --from-order <order-ref>`

Observed request body:

- `shoppingCartItems`
- `paymentProfileUUID`
- `deliveryAddress`
- `promotionOptions`
- `deliveryTime`
- `deliveryType`
- `interactionType`
- `useCredits`
- `businessDetails`
- `isMulticart`

Observed response contract:

- `data.draftOrder`
- `data.draftOrderPresentationResponse`
- `data.validationErrors`
- `data.draftOrderMetadata`
- `data.cartsView`

Implementation note:

- This is the first staged-write step for reorder and cart creation.
- A live April 12, 2026 validation confirmed that repeated `createDraftOrderV2` calls with the same seeded item payload and `isMulticart: true` returned distinct `draftOrder.uuid` values, so the CLI may define `carts create` as creating a new draft cart rather than mutating an existing one.
- `carts create --from-order <order-ref>` should derive its seed payload from:
  - item lines in `getPastOrdersV1`
  - the effective default delivery location from `getDeliveryLocationsV2`
  - delivery-instruction context from `getInstructionForLocationV1`
  - the default payment profile from `getProfilesForUserV1`
- The default create behavior should be:
  - current default delivery location
  - current default payment profile
  - ASAP fulfillment
  - no explicit promotion override beyond the observed empty/default structure
- After `createDraftOrderV2`, the implementation may call `updateDraftOrderV2` to reconcile address, delivery mode, or payment defaults, and may call `addItemsToDraftOrderV2` if the source order needs to be rebuilt incrementally.

#### `POST /_p/api/addItemsToDraftOrderV2`

Used by:

- cart growth after an existing draft order is selected or created

Observed request body:

- `draftOrderUUID`
- `cartUUID`
- `items`
- `shouldUpdateDraftOrderMetadata`
- `storeUUID`
- `actionMeta`

Observed response contract:

- `data.addedItems`
- `data.draftOrderUUID`
- `data.draftOrderPresentationResponse`

Implementation note:

- This is the direct add-to-cart endpoint for an existing draft order.

#### `POST /_p/api/updateItemInDraftOrderV2`

Used by:

- item customization edits inside a cart

Observed request body:

- `draftOrderUUID`
- `cartUUID`
- `item`

Observed response contract:

- `data.item`
- `data.draftOrderUUID`
- `data.draftOrderPresentationResponse`

Implementation note:

- This endpoint updates an existing cart line, but it is not a valid remove-item path.
- A live April 12, 2026 validation confirmed that sending `quantity: 0` returns `status: "failure"` with `invalid.request.error` and leaves the cart line unchanged.

#### `POST /_p/api/removeItemsFromDraftOrderV2`

Used by:

- `carts items remove <cart-ref> <cart-item-ref>`

Observed request body:

- `cartUUID`
- `draftOrderUUID`
- `shoppingCartItemUUIDs`
- `storeUUID`
- optional `actionMeta`
- optional `locationType`

Observed response contract:

- `data.shoppingCartItemUUIDs`
- `data.draftOrderUUID`
- `data.draftOrderPresentationResponse`

Implementation note:

- This is the confirmed single-line remove path for normal carts.
- A live April 12, 2026 validation confirmed that this endpoint succeeds for a single cart line when called with the line's `shoppingCartItemUUID`.
- In the last-item removal case, the draft cart disappears from carts view after the mutation. The implementation should refresh via `getCartsViewForEaterUuidV1` or `getDraftOrdersByEaterUuidV1` and treat the cart as removed when the `draftOrderUUID` is no longer present.

#### `POST /_p/api/removeItemsFromGroupDraftOrderV2`

Used by:

- `carts items remove <cart-ref> <cart-item-ref>` when the target cart is a group cart

Observed request body:

- `cartUUID`
- `draftOrderUUID`
- `shoppingCartItemUUIDs`
- `storeUUID`

Observed response contract:

- response shape is expected to mirror `removeItemsFromDraftOrderV2`

Implementation note:

- This endpoint name is present in the observed web bundle.
- The group-cart variant has not yet been live-validated, but the normal-cart path is confirmed and the payload shape is parallel in the bundle callsite.

#### `POST /_p/api/updateDraftOrderV2`

Used by:

- cart-level state updates
- address or delivery-mode updates on a draft order

Observed request body:

- `promotionOptions`
- `useCredits`
- `deliveryType`
- `extraPaymentProfiles`
- `interactionType`
- `deliveryAddress`
- `targetDeliveryTimeRange`
- `diningMode`
- `draftOrderUUID`

Observed response contract:

- `data.draftOrder`
- `data.validationErrors`

#### `POST /_p/api/discardDraftOrdersV1`

Used by:

- cart cleanup or reset flows

Observed request body:

```json
{
  "draftOrderUUIDs": ["04481651-2b83-41df-9fad-84135ad989fb"],
  "storeUUID": "754a8ecc-bc27-552c-9eb7-272ff682018c"
}
```

Observed response contract:

- `data.discardedDraftOrderUUIDs`

Sampled live response:

```json
{
  "status": "success",
  "data": {
    "discardedDraftOrderUUIDs": ["58cd5045-f51a-49af-9152-68b273f91b98"]
  }
}
```

#### `POST /_p/api/getCheckoutPresentationV1`

Used by:

- `carts checkout <cart-ref>` dry run

Observed request body:

- `payloadTypes`
- `draftOrderUUID`
- `isGroupOrder`
- `clientFeaturesData`
- `webGiftingPersonalizationEnabled`

Observed response contract:

- `data.checkoutPayloads`
- `data.validationErrors`
- `data.draftOrderUUID`
- `data.draftOrders`

Observed checkout payload categories include:

- `paymentProfilesEligibility`
- `requestUtensilPayload`
- `cartItems`
- `subtotal`
- `fareBreakdown`
- `eta`
- `orderConfirmations`

Implementation note:

- This is the dry-run checkout read path.
- It should power checkout preview without placing an order.

#### `POST /_p/api/checkoutOrdersByDraftOrdersV1`

Used by:

- `carts checkout <cart-ref> --confirm`

Observed request body:

- `draftOrderUUID`
- `storeInstructions`
- `extraPaymentData`
- `shareCPFWithRestaurant`
- `extraParams`

Observed response contract:

- `data.orders`
- `data.paymentProviderConfirmationUrl`

Observed order fields include:

- `orderInfo`
- `activeOrderOverview`
- `activeOrderStatus`
- `actions`
- `actionButtons`

Implementation note:

- This is the live order-submission step.
- The returned payload already resembles an active-order object.
- The command contract is stable, but this PRD intentionally leaves confirm-time payload expansion less fully specified than the read and staged-write surfaces.

### Address And Session Context API

#### `POST /_p/api/getDeliveryLocationsV2`

Used by:

- `addresses list`
- `addresses show default`
- cart and checkout address context

Observed request body:

```json
{
  "locationTypes": ["SUGGESTED"]
}
```

Observed response contract:

- `data.deliveryLocations`

Observed delivery-location buckets:

- `SAVED`
- `SUGGESTED`
- `TARGET`

Observed location entry fields include:

- `location`
- `deliveryPayload`
- `deliveryConfig`
- `personalPayload`
- `analytics`
- `title`
- `subtitle`

Implementation note:

- This is the best observed source for saved and effective delivery locations.
- The CLI should treat `TARGET` as the current effective location when it exists.

#### `POST /_p/api/getInstructionForLocationV1`

Used by:

- `addresses show default`
- cart delivery-instruction context

Observed request body:

- one serialized location object

Observed response contract:

- `availableInteractionTypes`
- `defaultInteractionType`
- `preferredInteractionType`
- `instructions`
- `selectedInstruction`

Observed instruction fields include:

- `interactionType`
- `notes`
- `aptOrSuite`
- `waypoint`
- `displayString`

Implementation note:

- This endpoint resolves the usable delivery-instruction choices for a specific location.

#### `POST /_p/api/getProfilesForUserV1`

Used by:

- session and payment-profile context for carts and checkout

Observed request body:

```json
{}
```

Observed response contract:

- `data.profiles`
- `data.selectedProfile`
- `data.defaultBusinessProfileUUID`

Observed profile fields include:

- `type`
- `status`
- `uuid`
- `name`
- `defaultPaymentProfileUuid`
- `secondaryPaymentProfileUuid`
- managed-business flags

#### `POST /_p/api/getUserV1`

Used by:

- account-level context and payment profile access

Observed request body:

```json
{
  "shouldGetPointEstimateMetadata": true
}
```

Observed response contract:

- `paymentProfiles`
- `isAdmin`
- `isLoggedIn`
- `eaterXpUuid`
- `firstName`
- `lastName`
- `subscriptionMeta`
- `hashedEmail`

#### `POST /_p/api/getBusinessProfilesV1`

Used by:

- business-profile context when present

Observed request body:

```json
{}
```

Observed response contract:

- top-level `status/data`
- sampled top-level keys were confirmed, but the PRD does not yet lock nested response fields

#### `POST /_p/api/selectProfileV1`

Used by:

- profile switching when the CLI eventually needs it

Observed request body:

```json
{
  "profileUUID": "2ee3815d-db51-4005-bf0d-684add2de1b9",
  "selectProfileSource": "SELECT_PROFILE_SOURCE_CLIENT_PROFILE_SWITCH"
}
```

Observed response contract:

```json
{
  "status": "success",
  "data": {}
}
```

Important distinction:

- `getProfilesForUserV1` and `selectProfileV1` operate on Uber account profiles.
- `getDeliveryLocationsV2` and `getInstructionForLocationV1` operate on delivery-location and instruction state.

## Minimal Curl Examples

This section gives implementation-facing examples for each observed endpoint.

This section is illustrative. If an example here disagrees with the observed contract above, the observed contract above wins.

Shared shell setup for the `curl` examples:

```sh
export UE_BASE='https://www.ubereats.com'
export UE_COOKIE='<browser-session-cookies-from-cli-managed-profile>'
export UE_LAT='40.748198'
export UE_LNG='-73.9746683'
```

All examples below are private, session-bound web requests. They assume a valid browser-authenticated cookie string and the effective delivery-location headers.

### Orders API Recipes

#### `getActiveOrdersV1`

Pseudocode:

```text
POST getActiveOrdersV1
normalize data.orders into active-order rows
poll on the watch interval for active-order watch flows
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getActiveOrdersV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"orderUuid":null,"timezone":"America/New_York","showAppUpsellIllustration":true,"isDirectTracking":false}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "orders": []
  }
}
```

#### `getPastOrdersV1`

Pseudocode:

```text
POST getPastOrdersV1 with limit or cursor
read data.orderUuids and data.ordersMap
emit normalized past-order rows
continue while pagination data indicates more results and the CLI still needs more rows
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getPastOrdersV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"limit":1}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "orderUuids": ["7723a4f6-7cce-4b29-a4d8-fc98f5ee3a6f"],
    "ordersMap": {
      "7723a4f6-7cce-4b29-a4d8-fc98f5ee3a6f": {
        "completedAt": "2026-03-31T00:00:00Z",
        "storeInfo": { "title": "CVS" },
        "fareInfo": { "total": 6551 },
        "shoppingCart": { "items": [] }
      }
    },
    "paginationData": { "nextCursor": "..." },
    "meta": { "hasMore": true }
  }
}
```

#### `getInvoiceStatusV1`

Pseudocode:

```text
POST one order UUID
read data.invoiceStatus
attach invoice metadata to orders show output
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getInvoiceStatusV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"orderUUID":"7723a4f6-7cce-4b29-a4d8-fc98f5ee3a6f"}'
```

#### `getOrderEntitiesV1`

Pseudocode:

```text
POST empty body
inspect data.orderEntities and data.orderEntitiesView
ignore if the payload stays empty
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getOrderEntitiesV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{}'
```

### Discovery API Recipes

#### `getFeedV1`

Pseudocode:

```text
POST the home-feed request shape, with the favorites flag set for the desired view
walk heterogeneous data.feedItems
normalize only store-bearing cards plus the favorites map
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getFeedV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"cacheKey":"/DELIVERY///0/0//JTVCJTVE/undefined///true///HOME////////","feedSessionCount":{"announcementCount":0,"announcementLabel":""},"userQuery":"","date":"","startTime":0,"endTime":0,"carouselId":"","sortAndFilters":[],"billboardUuid":"","feedProvider":"","promotionUuid":"","targetingStoreTag":"","venueUUID":"","selectedSectionUUID":"","favorites":"true","vertical":"","searchSource":"","searchType":"","keyName":"","serializedRequestContext":"","isUserInitiatedRefresh":false}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "cacheKey": "/DELIVERY/...",
    "feedItems": [
      { "type": "SECTION_HEADER", "title": "More favorites" },
      { "type": "MINI_STORE", "uuid": "store-uuid" }
    ],
    "favorites": {
      "store-uuid": true
    }
  }
}
```

#### `getSearchHomeV2`

Pseudocode:

```text
POST an empty-query search-home request
read browseHomeFeed, suggestedSections, and verticalSuggestionLists
use this for zero-query search UX
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getSearchHomeV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"dropPastOrders":true}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "searchHistory": [],
    "suggestedSections": [],
    "verticalSuggestionLists": [
      { "vertical": "ALL" },
      { "vertical": "RESTAURANTS" }
    ],
    "verticalSearchHomeResults": [],
    "browseHomeFeed": []
  }
}
```

#### `getSearchSuggestionsV1`

Pseudocode:

```text
POST a user query and vertical
read data as a suggestion row array
use this as typeahead, not as the final result set
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getSearchSuggestionsV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"userQuery":"gummy bears","date":"","startTime":0,"endTime":0,"vertical":"ALL"}'
```

Sampled response:

```json
{
  "status": "success",
  "data": [
    { "type": "searchHistoryV2" },
    { "type": "textV2", "text": "gummy bears" },
    { "type": "searchTextV2", "text": "gummy bears near me" }
  ]
}
```

#### `getSearchFeedV1`

Pseudocode:

```text
POST a final query plus display and search-source metadata
walk mixed data.feedItems plus data.storesMap
normalize store-bearing cards for stores search and item-bearing cards for items search
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getSearchFeedV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"userQuery":"gummy bears","date":"","startTime":0,"endTime":0,"sortAndFilters":[],"vertical":"ALL","searchSource":"SEARCH_SUGGESTION","displayType":"SEARCH_RESULTS","searchType":"GLOBAL_SEARCH","keyName":"","cacheKey":"","recaptchaToken":""}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "title": "gummy bears",
    "subtitle": "results",
    "feedItems": [
      { "type": "MINI_STORE_WITH_ITEMS", "uuid": "..." }
    ],
    "storesMap": {
      "store-uuid": { "title": "CVS", "uuid": "store-uuid" }
    },
    "meta": { "offset": 0, "hasMore": true },
    "sortAndFilters": []
  }
}
```

### Store Catalog API Recipes

#### `getStoreV1`

Pseudocode:

```text
POST store UUID plus dining mode and time preference
read sections plus catalogSectionsMap
normalize store summary, menu sections, and embedded catalog items
for items search --store, locally filter catalog items by title and itemDescription and dedupe by item UUID
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getStoreV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"storeUuid":"58d90f4b-db74-4e7d-8fa8-c34976758192","diningMode":"DELIVERY","time":{"asap":true},"cbType":"EATER_ENDORSED"}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "title": "Rosa Mexicano",
    "uuid": "58d90f4b-db74-4e7d-8fa8-c34976758192",
    "currencyCode": "USD",
    "isOrderable": true,
    "sections": [
      {
        "uuid": "18759cd0-c287-516d-b54c-f3aea29a0ad7",
        "title": "Menu",
        "subsectionUuids": ["0379a62a-1156-4ddb-a09a-7e1dc45d5dfb"]
      }
    ],
    "catalogSectionsMap": {
      "18759cd0-c287-516d-b54c-f3aea29a0ad7": [
        {
          "payload": {
            "standardItemsPayload": {
              "catalogItems": [
                {
                  "uuid": "2629d29e-bb3b-5622-88e7-0e50e1ea5db8",
                  "sectionUuid": "18759cd0-c287-516d-b54c-f3aea29a0ad7",
                  "subsectionUuid": "0379a62a-1156-4ddb-a09a-7e1dc45d5dfb",
                  "title": "La Tradicional Margarita",
                  "price": 2070,
                  "hasCustomizations": false
                }
              ]
            }
          }
        }
      ]
    }
  }
}
```

#### `getMenuItemV1`

Pseudocode:

```text
POST store UUID plus section UUID, subsection UUID, and menu-item UUID
read data.title, data.price, and data.customizationsList
emit one item-detail object
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getMenuItemV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"itemRequestType":"ITEM","storeUuid":"58d90f4b-db74-4e7d-8fa8-c34976758192","sectionUuid":"18759cd0-c287-516d-b54c-f3aea29a0ad7","subsectionUuid":"00770077-f27b-597a-88fc-fa6823166490","menuItemUuid":"1d389bcd-f983-598f-a5ea-3c613934d4be","cbType":"EATER_ENDORSED","includeCheaperAlternatives":false,"contextReferences":[{"type":"GROUP_ITEMS","payload":{"type":"groupItemsContextReferencePayload","groupItemsContextReferencePayload":{"catalogSectionUUID":""}},"pageContext":"STORE"}]}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "uuid": "1d389bcd-f983-598f-a5ea-3c613934d4be",
    "title": ".Taquitos Ahi Tuna",
    "sectionUuid": "18759cd0-c287-516d-b54c-f3aea29a0ad7",
    "subsectionUuid": "404711ae-fd4d-5802-b0ab-a4e83ec11242",
    "price": 2530,
    "hasCustomizations": true,
    "customizationsList": [
      {
        "uuid": "b7aacdd3-df41-5194-9827-6a0b9f114c55",
        "title": ".+Add Dips",
        "minPermitted": 0,
        "maxPermitted": 11,
        "options": [
          {
            "uuid": "b4dbe360-c975-5bb3-bbd6-cf7d5dfd5561",
            "title": "Caesar Dressing 4oz",
            "price": 200
          }
        ]
      }
    ]
  }
}
```

### Draft Order And Cart API Recipes

#### `getCartsViewForEaterUuidV1`

Pseudocode:

```text
POST an empty body
read data.cartsView.carts
emit lightweight cart summaries
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getCartsViewForEaterUuidV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{}'
```

#### `getDraftOrdersByEaterUuidV1`

Pseudocode:

```text
POST the richer draft-order collection request
read data.draftOrders and data.cartsView
use this when cart-list output needs more than the lightweight summary view
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getDraftOrdersByEaterUuidV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"removeAdapters":true}'
```

#### `getDraftOrderByUuidV1`

Pseudocode:

```text
POST one draft-order UUID
read data.shoppingCart, data.deliveryAddress, data.deliveryType, and data.interactionType
emit one cart object
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getDraftOrderByUuidV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"draftOrderUuid":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad"}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "uuid": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad",
    "state": "UNORDERED",
    "storeUuid": "58d90f4b-db74-4e7d-8fa8-c34976758192",
    "deliveryType": "ASAP",
    "interactionType": "leave_at_door",
    "shoppingCart": {
      "cartUuid": "dc7fec40-7709-4300-960f-0730ec216c65",
      "items": [
        {
          "shoppingCartItemUuid": "97d15bc9-d973-42a4-bdd8-a831a5b2a299",
          "uuid": "2629d29e-bb3b-5622-88e7-0e50e1ea5db8",
          "title": "La Tradicional Margarita",
          "quantity": 1
        }
      ]
    }
  }
}
```

#### `createDraftOrderV2`

Pseudocode:

```text
POST one or more shoppingCartItems plus delivery, payment, and delivery-type context
read data.draftOrder and data.validationErrors
observe the returned draftOrder UUID as a newly created draft cart
use this for carts create from order and carts create from store plus item
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/createDraftOrderV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"isMulticart":true,"shoppingCartItems":[{"uuid":"1d389bcd-f983-598f-a5ea-3c613934d4be","shoppingCartItemUuid":"232d4b79-feb0-4722-afac-8cc72f532491","storeUuid":"58d90f4b-db74-4e7d-8fa8-c34976758192","sectionUuid":"18759cd0-c287-516d-b54c-f3aea29a0ad7","subsectionUuid":"404711ae-fd4d-5802-b0ab-a4e83ec11242","price":2530,"title":".Taquitos Ahi Tuna","quantity":1,"customizations":{},"imageURL":"https://tb-static.uber.com/prod/image-proc/processed_images/371c0c181eb527a0ca4274617af726e1/0fb376d1da56c05644450062d25c5c84.jpeg","specialInstructions":"","itemId":null}],"useCredits":true,"extraPaymentProfiles":[],"promotionOptions":{"autoApplyPromotionUUIDs":[],"selectedPromotionInstanceUUIDs":[],"skipApplyingPromotion":false},"deliveryTime":{"asap":true},"deliveryType":"ASAP","currencyCode":"USD","interactionType":"leave_at_door","paymentProfileUUID":"3b6a5739-d556-5edd-94c6-b42a7d94f9ca","checkMultipleDraftOrdersCap":true,"actionMeta":{"isQuickAdd":false,"numClicks":0},"businessDetails":{}}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "draftOrder": {
      "uuid": "4a60ea9f-4244-44fc-9728-c4e396759f9c",
      "state": "UNORDERED",
      "storeUuid": "58d90f4b-db74-4e7d-8fa8-c34976758192",
      "deliveryType": "ASAP"
    },
    "validationErrors": null,
    "draftOrderMetadata": {},
    "cartsView": {}
  }
}
```

#### `addItemsToDraftOrderV2`

Pseudocode:

```text
POST draftOrderUUID, cartUUID, and one or more items
read data.addedItems and the draft-order presentation response
emit the updated cart
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/addItemsToDraftOrderV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"draftOrderUUID":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad","cartUUID":"dc7fec40-7709-4300-960f-0730ec216c65","items":[{"uuid":"abb6b8d8-0da3-53e4-a53d-b5ddd3c11b2f","shoppingCartItemUuid":"f66e6f54-11ce-4ca3-8c5a-5fa72b7e0379","storeUuid":"58d90f4b-db74-4e7d-8fa8-c34976758192","sectionUuid":"18759cd0-c287-516d-b54c-f3aea29a0ad7","subsectionUuid":"404711ae-fd4d-5802-b0ab-a4e83ec11242","price":1495,"title":".Queso Dip","quantity":1,"customizations":{"c55225de-d585-5116-ba40-f7ee8a3cbe4b+1":[{"uuid":"bc96a7c2-0bdc-583b-9635-f10bae05ded5","price":400,"quantity":1,"title":"Cilantro Chimichurri 4oz","defaultQuantity":0,"customizationMeta":{"title":".+Add Salsa","isPickOne":false}}]},"imageURL":"https://tb-static.uber.com/prod/image-proc/processed_images/33eb78856365ae7b56e3e7eec1a16163/0fb376d1da56c05644450062d25c5c84.jpeg","specialInstructions":"","itemId":null}],"shouldUpdateDraftOrderMetadata":false,"storeUUID":"58d90f4b-db74-4e7d-8fa8-c34976758192","actionMeta":{"isQuickAdd":false,"numClicks":1}}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "addedItems": [
      {
        "uuid": "abb6b8d8-0da3-53e4-a53d-b5ddd3c11b2f",
        "shoppingCartItemUuid": "f66e6f54-11ce-4ca3-8c5a-5fa72b7e0379"
      }
    ],
    "draftOrderUUID": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad",
    "draftOrderPresentationResponse": {}
  }
}
```

#### `updateItemInDraftOrderV2`

Pseudocode:

```text
POST draftOrderUUID, cartUUID, and a full replacement item line
read data.item and the draft-order presentation response
emit the updated cart
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/updateItemInDraftOrderV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"draftOrderUUID":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad","cartUUID":"dc7fec40-7709-4300-960f-0730ec216c65","item":{"uuid":"abb6b8d8-0da3-53e4-a53d-b5ddd3c11b2f","shoppingCartItemUuid":"f66e6f54-11ce-4ca3-8c5a-5fa72b7e0379","storeUuid":"58d90f4b-db74-4e7d-8fa8-c34976758192","sectionUuid":"18759cd0-c287-516d-b54c-f3aea29a0ad7","subsectionUuid":"404711ae-fd4d-5802-b0ab-a4e83ec11242","price":1495,"title":".Queso Dip","quantity":1,"customizations":{"c55225de-d585-5116-ba40-f7ee8a3cbe4b+1":[{"uuid":"bc96a7c2-0bdc-583b-9635-f10bae05ded5","price":400,"quantity":1,"title":"Cilantro Chimichurri 4oz","defaultQuantity":0,"customizationMeta":{"title":".+Add Salsa","isPickOne":false}},{"uuid":"2600b157-9f04-5407-a3f6-ee889148828c","price":400,"quantity":1,"title":"Salsa Verde Cruda 4oz","defaultQuantity":0,"customizationMeta":{"title":".+Add Salsa","isPickOne":false}}]},"specialInstructions":"","imageURL":"https://tb-static.uber.com/prod/image-proc/processed_images/33eb78856365ae7b56e3e7eec1a16163/0fb376d1da56c05644450062d25c5c84.jpeg","fulfillmentIssueAction":{}}}'
```

Sampled success response:

```json
{
  "status": "success",
  "data": {
    "item": {
      "shoppingCartItemUuid": "f66e6f54-11ce-4ca3-8c5a-5fa72b7e0379",
      "uuid": "abb6b8d8-0da3-53e4-a53d-b5ddd3c11b2f",
      "title": ".Queso Dip",
      "quantity": 1
    },
    "draftOrderUUID": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad",
    "draftOrderPresentationResponse": {}
  }
}
```

Sampled quantity-zero failure response:

```json
{
  "status": "failure",
  "data": {
    "message": "status code error",
    "code": "400",
    "meta": {
      "statusCode": "400",
      "body": {
        "code": "invalid.request.error",
        "message": "InvalidOrMissingArguments{Info: ErrorInfo{Message: Item/Option quantity must be positive}}"
      }
    }
  }
}
```

#### `removeItemsFromDraftOrderV2`

Pseudocode:

```text
POST cartUUID, draftOrderUUID, storeUUID, and one or more shoppingCartItemUUIDs
read data.shoppingCartItemUUIDs and data.draftOrderUUID
refresh carts view after the mutation
if the draft still exists, emit the updated cart
if the draft disappears because the last line was removed, emit a cart-removed state
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/removeItemsFromDraftOrderV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"cartUUID":"2d87efff-4ba7-4c41-ad5d-7947ea87eaea","draftOrderUUID":"562a2132-07d5-4d17-8b16-545de34a2986","shoppingCartItemUUIDs":["232d4b79-feb0-4722-afac-8cc72f532491"],"storeUUID":"58d90f4b-db74-4e7d-8fa8-c34976758192"}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "shoppingCartItemUUIDs": [
      "232d4b79-feb0-4722-afac-8cc72f532491"
    ],
    "draftOrderUUID": "562a2132-07d5-4d17-8b16-545de34a2986",
    "draftOrderPresentationResponse": {}
  }
}
```

Observed postcondition:

```json
{
  "cartsViewContainsDraftOrder": false,
  "note": "When the removed line is the last line in the cart, the draft cart disappears from carts view."
}
```

#### `updateDraftOrderV2`

Pseudocode:

```text
POST cart-level fields such as deliveryType, interactionType, deliveryAddress, payment profile, or promotions
read data.draftOrder and data.validationErrors
emit the updated cart
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/updateDraftOrderV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -H "x-uber-device-location-latitude: $UE_LAT" \
  -H "x-uber-device-location-longitude: $UE_LNG" \
  -b "$UE_COOKIE" \
  --data '{"promotionOptions":{"autoApplyPromotionUUIDs":[],"selectedPromotionInstanceUUIDs":[],"skipApplyingPromotion":false},"useCredits":true,"deliveryType":"PREMIUM_DELIVERY","extraPaymentProfiles":[],"interactionType":"leave_at_door","businessDetails":{"profileUUID":"2ee3815d-db51-4005-bf0d-684add2de1b9","profileType":"Personal","policyUUID":null,"policyVersion":null},"cartLockOptions":null,"deliveryAddress":{"address":{"address1":"222 E 39th St","address2":"New York, NY","aptOrSuite":"","eaterFormattedAddress":"222 E 39th St, New York, NY 10016-2754, US","subtitle":"New York, NY","title":"222 E 39th St","uuid":"","label":"Home"},"latitude":40.748198,"longitude":-73.9746683,"reference":"7ae1b58c-5731-dbd2-1988-5ea90e060950","referenceType":"uber_places","type":"uber_places","addressComponents":{"city":"New York","countryCode":"US","firstLevelSubdivisionCode":"NY","postalCode":"10016-2754"},"categories":["MULTI_UNIT","RESIDENCE","AREAS_AND_BUILDINGS","LANDMARK","address_point"],"originType":"user_autocomplete"},"targetDeliveryTimeRange":{"asap":true},"diningMode":"DELIVERY","draftOrderUUID":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad"}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "draftOrder": {
      "uuid": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad",
      "deliveryType": "PREMIUM_DELIVERY",
      "interactionType": "leave_at_door"
    },
    "validationErrors": null
  }
}
```

#### `discardDraftOrdersV1`

Pseudocode:

```text
POST one or more draft-order UUIDs plus the store UUID
discard the target draft carts
read data.discardedDraftOrderUUIDs when present
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/discardDraftOrdersV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"draftOrderUUIDs":["04481651-2b83-41df-9fad-84135ad989fb"],"storeUUID":"754a8ecc-bc27-552c-9eb7-272ff682018c"}'
```

#### `getCheckoutPresentationV1`

Pseudocode:

```text
POST one draft-order UUID plus the preview payload types
read data.checkoutPayloads and data.validationErrors
emit the dry-run checkout preview without placing an order
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getCheckoutPresentationV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"payloadTypes":["paymentProfilesEligibility","cartItems","subtotal","fareBreakdown","eta","orderConfirmations"],"draftOrderUUID":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad","isGroupOrder":false,"webGiftingPersonalizationEnabled":false}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "draftOrderUUID": "fc734499-20bb-41c4-b3ed-1d67df0ce9ad",
    "checkoutPayloads": [
      { "type": "cartItems" },
      { "type": "subtotal" },
      { "type": "fareBreakdown" }
    ],
    "validationErrors": []
  }
}
```

#### `checkoutOrdersByDraftOrdersV1`

Pseudocode:

```text
POST one draft-order UUID plus checkout-confirm context
read data.orders and any payment-provider confirmation URL
emit the resulting order state
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/checkoutOrdersByDraftOrdersV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"draftOrderUUID":"fc734499-20bb-41c4-b3ed-1d67df0ce9ad","storeInstructions":"","extraPaymentData":"","shareCPFWithRestaurant":false,"extraParams":{"timezone":"America/New_York"}}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "orders": [
      {
        "orderInfo": {},
        "activeOrderOverview": {},
        "activeOrderStatus": {},
        "actions": [],
        "actionButtons": []
      }
    ],
    "paymentProviderConfirmationUrl": null
  }
}
```

### Address And Session Context API Recipes

#### `getDeliveryLocationsV2`

Pseudocode:

```text
POST the location-type selector
read SAVED, SUGGESTED, and TARGET delivery locations
resolve the effective default location for later requests
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getDeliveryLocationsV2" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"locationTypes":["SUGGESTED"]}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "deliveryLocations": {
      "SAVED": [
        {
          "title": "222 E 39th St, 21B",
          "subtitle": "New York, NY",
          "location": {
            "coordinate": {
              "latitude": 40.748198,
              "longitude": -73.9746683
            }
          }
        }
      ],
      "TARGET": [
        {
          "title": "222 E 39th St, 21B",
          "subtitle": "New York, NY"
        }
      ]
    }
  }
}
```

#### `getInstructionForLocationV1`

Pseudocode:

```text
POST one serialized delivery location
read availableInteractionTypes, defaultInteractionType, and selectedInstruction
use this to populate cart interaction-type choices
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getInstructionForLocationV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"location":{"address":{"address1":"222 E 39th St","address2":"New York, NY","aptOrSuite":"","eaterFormattedAddress":"222 E 39th St, New York, NY 10016-2754, US","subtitle":"New York, NY","title":"222 E 39th St","uuid":"","label":"Home"},"latitude":40.748198,"longitude":-73.9746683,"reference":"7ae1b58c-5731-dbd2-1988-5ea90e060950","referenceType":"uber_places","type":"uber_places","addressComponents":{"city":"New York","countryCode":"US","firstLevelSubdivisionCode":"NY","postalCode":"10016-2754"},"categories":["MULTI_UNIT","RESIDENCE","AREAS_AND_BUILDINGS","LANDMARK","address_point"],"originType":"user_autocomplete"}}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "availableInteractionTypes": [
      "door_to_door",
      "curbside",
      "leave_at_door",
      "meet_in_lobby",
      "leave_in_lobby"
    ],
    "defaultInteractionType": "door_to_door",
    "preferredInteractionType": "leave_at_door",
    "selectedInstruction": {
      "interactionType": "leave_at_door"
    }
  }
}
```

#### `getProfilesForUserV1`

Pseudocode:

```text
POST an empty body
read selectedProfile and payment profile UUIDs
use this to seed draft-order creation
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getProfilesForUserV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "profiles": [
      {
        "uuid": "2ee3815d-db51-4005-bf0d-684add2de1b9",
        "type": "Personal",
        "defaultPaymentProfileUuid": "3b6a5739-d556-5edd-94c6-b42a7d94f9ca"
      }
    ],
    "selectedProfile": {
      "uuid": "2ee3815d-db51-4005-bf0d-684add2de1b9",
      "defaultPaymentProfileUuid": "3b6a5739-d556-5edd-94c6-b42a7d94f9ca"
    }
  }
}
```

#### `getUserV1`

Pseudocode:

```text
POST the logged-in user request
read isLoggedIn and paymentProfiles
use this to validate login success
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getUserV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"shouldGetPointEstimateMetadata":true}'
```

Sampled response:

```json
{
  "status": "success",
  "data": {
    "isLoggedIn": true,
    "eaterXpUuid": "eater-uuid",
    "firstName": "Prateek",
    "paymentProfiles": []
  }
}
```

#### `getBusinessProfilesV1`

Pseudocode:

```text
POST an empty body
read any business-profile context the session exposes
use this only when the provider needs business-profile switching or policy context
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/getBusinessProfilesV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{}'
```

#### `selectProfileV1`

Pseudocode:

```text
POST the target profile UUID plus the profile-switch source enum
switch the active Uber account profile
```

Minimal curl:

```sh
curl "$UE_BASE/_p/api/selectProfileV1" \
  -X POST \
  -H 'content-type: application/json' \
  -H 'x-csrf-token: x' \
  -b "$UE_COOKIE" \
  --data '{"profileUUID":"2ee3815d-db51-4005-bf0d-684add2de1b9","selectProfileSource":"SELECT_PROFILE_SOURCE_CLIENT_PROFILE_SWITCH"}'
```

### Remaining Unknowns

- `getInvoiceStatusV1` needs more captures with non-empty invoice states.
- `getBusinessProfilesV1` is observed, but its nested response shape is still less fully characterized than the other core endpoints.
- `removeItemsFromGroupDraftOrderV2` is named in the web bundle, but still needs a live group-cart validation pass.

## Canonical Reference Formats

This PRD defines canonical internal reference formats so the CLI is implementable without guessing.

- `order-ref` means the Uber order UUID
- `cart-ref` means the Uber `draftOrderUUID`
- `cart-item-ref` means the Uber `shoppingCartItemUuid`
- `store-ref` means the Uber `storeUuid`
- `item-ref` means the Uber menu-item UUID carried through store catalog payloads and `getMenuItemV1`
- `group-ref` means the Uber customization-group UUID from `getMenuItemV1`
- `option-ref` means the Uber customization-option UUID from `getMenuItemV1`

Human-readable output and `--json` output should include these canonical references explicitly.

Optional convenience parsing from copied Uber Eats URLs may be added later, but it is not required for the base implementation.

## Output Model

Human-readable output is the default.

All resource commands that return structured data must support `--json`.

### Output Mode Contract

- Human-readable output is the default and goes to stdout.
- `--json` switches stdout to machine-oriented JSON only.
- Diagnostics, warnings, and trace output go to stderr only.
- `--trace` must never contaminate stdout.
- List commands should optimize for fast scanning by humans.
- Show commands should optimize for full inspection by humans.
- JSON output should optimize for stability and scripting, not visual prettiness.

Examples:

```sh
ordercli ubereats orders list --json
ordercli ubereats stores show <store-ref> --json
ordercli ubereats items search "spicy tuna roll" --json
ordercli ubereats items show <item-ref> --store <store-ref> --json
ordercli ubereats carts show <cart-ref> --json
```

### JSON Contract

Under `--json`:

- JSON goes to stdout only.
- Diagnostics go to stderr.
- Empty results are successful commands.
- Secrets and session material must never appear in output.
- List commands return arrays under `data.items`.
- Show commands return single objects under `data.item`.
- All entity objects should include their canonical refs explicitly.
- All timestamps should be RFC 3339 strings.
- All money values should use a normalized money object instead of a bare string or bare integer.

Recommended success shape:

```json
{
  "ok": true,
  "data": {
    "items": []
  },
  "meta": {
    "provider": "ubereats"
  }
}
```

Recommended error shape:

```json
{
  "ok": false,
  "error": {
    "code": "not_logged_in",
    "message": "Run `ordercli ubereats login`."
  }
}
```

If a list command can return more results, `meta` should include the closest honest pagination signal available.

### Normalized Field Shapes

Money fields in JSON should use this normalized shape:

```json
{
  "currency_code": "USD",
  "amount_minor": 1495,
  "display": "$14.95"
}
```

A list row should use normalized refs, timestamps, and money fields rather than provider-specific raw fields.

Example order row:

```json
{
  "ref": "7723a4f6-7cce-4b29-a4d8-fc98f5ee3a6f",
  "kind": "order",
  "merchant": {
    "ref": "store-uuid",
    "title": "CVS"
  },
  "state": "COMPLETED",
  "occurred_at": "2026-03-31T00:00:00Z",
  "total": {
    "currency_code": "USD",
    "amount_minor": 6551,
    "display": "$65.51"
  }
}
```

Example cart row:

```json
{
  "ref": "4a60ea9f-4244-44fc-9728-c4e396759f9c",
  "kind": "cart",
  "merchant": {
    "ref": "58d90f4b-db74-4e7d-8fa8-c34976758192",
    "title": "Rosa Mexicano"
  },
  "delivery_type": "ASAP",
  "interaction_type": "leave_at_door",
  "item_count": 1
}
```

Example session context:

```json
{
  "location": {
    "ref": "7ae1b58c-5731-dbd2-1988-5ea90e060950",
    "source": "TARGET",
    "full_address": "222 E 39th St, New York, NY 10016-2754, US"
  },
  "profile": {
    "type": "personal"
  },
  "payment_profile_ref": "3b6a5739-d556-5edd-94c6-b42a7d94f9ca"
}
```

Example item row:

```json
{
  "ref": "2629d29e-bb3b-5622-88e7-0e50e1ea5db8",
  "kind": "item",
  "store_ref": "58d90f4b-db74-4e7d-8fa8-c34976758192",
  "section_ref": "18759cd0-c287-516d-b54c-f3aea29a0ad7",
  "subsection_ref": "0379a62a-1156-4ddb-a09a-7e1dc45d5dfb",
  "title": "La Tradicional Margarita",
  "price": {
    "currency_code": "USD",
    "amount_minor": 2070,
    "display": "$20.70"
  }
}
```

Baseline error codes for this provider should include:

- `not_logged_in`
- `unsupported_host`
- `invalid_ref`
- `invalid_cart_item_ref`
- `not_found`
- `not_same_store`
- `not_checkout_ready`
- `upstream_error`

### Human-Readable Output Conventions

- Human-readable list output should be one resource per line.
- Human-readable show output should be a small block of labeled fields followed by item lines where relevant.
- Currency should render with symbol and code when needed for clarity.
- Human-readable money should come from the same normalized money data used by `--json`.
- Human-readable output should expose canonical refs on show commands and omit noisy refs on list commands unless they are needed to act on the result.
- `orders list` should show one order per line with merchant, state, occurred time, and total when available.
- `orders show` should show merchant, state, timestamps, cart items, pricing summary, store context, and invoice status when available.
- `stores list` and `stores search` should show store title plus the most decision-useful metadata available from the feed, such as ETA, fee, rating, and favorite state.
- `stores show` should show store identity, orderability, hours, rating, fee/ETA context, and high-level catalog structure.
- `stores menu` should show section headings and item titles, with prices where available.
- `items search` should show item title, store title, section context, and price when available, with enough context to make the next command obvious.
- `items show` should show title, description, price, availability, and customization groups/options.
- `carts list` should show cart title, item count, subtotal summary, delivery-address summary, and delivery type when available.
- `carts show` and checkout dry run should show merchant, delivery address, item lines, subtotal, fees, delivery type, interaction type, the current checkout-ready state, and the effective session context that would govern a live checkout.

## Debug Tracing

Debug tracing is a required part of the Uber Eats CLI surface.

The CLI must support redacted tracing of JSON interactions so the raw request and response flow is visible during debugging.

### Debug Config State

The provider config must support:

- `debug`

### Debug Flags

Every command that talks to Uber Eats should support:

```sh
--trace
```

### Debug Semantics

- `debug` in config enables redacted JSON tracing to stderr by default.
- `--trace` enables redacted JSON tracing for the current command.
- Command flags override config defaults.
- Redaction is mandatory, not optional.
- Trace output belongs on stderr.

### Trace Contents

The trace should include enough information to reconstruct the command's request and response flow.

At minimum, each trace entry should capture:

- timestamp
- command name
- interaction type
- request URL
- request method
- redacted request headers
- redacted request body
- response status
- redacted response headers
- response body or parsed JSON body

List and search commands may generate multiple traced interactions while satisfying `--limit`.

If the command is driven by browser navigation and captured network traffic rather than direct API calls, the trace should still preserve the same conceptual request and response sequence.

## Backend Implications

This interface implies the following backend capabilities:

- session-backed order listing for active and past orders
- single-order lookup by explicit ref
- store discovery for an effective address
- favorite-store listing
- store search by query
- cross-store item search
- merchant summary and menu extraction
- cart listing and cart detail lookup
- cart creation from a prior order
- cart checkout with explicit confirmation
- address listing and default-address inspection
- redacted debug tracing across all JSON interactions

## Security Requirements

- The managed browser profile is the real secret store.
- Config stores pointers and defaults, not live session secrets.
- Host validation must constrain Uber Eats config to supported HTTPS hosts.
- Redaction must cover cookies, tokens, auth headers, CSRF values, and other session-bearing fields.
- Debug trace output must be safe to view, redirect, or capture.

## CLI Examples

These examples are illustrative. They are meant to show how the final interface should feel in real use.

### Example: Inspect Active Orders

```sh
ordercli ubereats orders list
```

Example stdout:

```text
Burrito Affirmation  COMPLETED  2026-04-11T18:42:00Z  $27.08
```

### Example: Watch One Order

```sh
ordercli ubereats orders show e90fbdc4-8775-4a05-a1b1-ef801a904b53 --watch
```

Example stdout shape:

```text
merchant: Burrito Affirmation
state: OUT_FOR_DELIVERY
occurred_at: 2026-04-11T18:42:00Z
total: $27.08
items:
- Fresh AF x1
```

### Example: Find A Store And Browse Its Menu

```sh
ordercli ubereats stores search "mexican"
ordercli ubereats stores show 58d90f4b-db74-4e7d-8fa8-c34976758192
ordercli ubereats stores menu 58d90f4b-db74-4e7d-8fa8-c34976758192
```

Example stdout shape:

```text
Rosa Mexicano  ETA 25-35 min  Fee $0.00  Rating 4.6
```

### Example: Search Items Within One Store

```sh
ordercli ubereats items search "queso" --store 58d90f4b-db74-4e7d-8fa8-c34976758192
```

Example stdout shape:

```text
.Queso Dip  Rosa Mexicano  Starters  $14.95
```

### Example: Create And Edit A Cart

```sh
ordercli ubereats carts create --store 58d90f4b-db74-4e7d-8fa8-c34976758192 --item 1d389bcd-f983-598f-a5ea-3c613934d4be
ordercli ubereats carts items add 562a2132-07d5-4d17-8b16-545de34a2986 --item abb6b8d8-0da3-53e4-a53d-b5ddd3c11b2f --note "extra salsa"
ordercli ubereats carts items remove 562a2132-07d5-4d17-8b16-545de34a2986 232d4b79-feb0-4722-afac-8cc72f532491
```

Example stdout shape:

```text
cart: 562a2132-07d5-4d17-8b16-545de34a2986
merchant: Rosa Mexicano
item_count: 1
subtotal: $14.95
delivery_type: ASAP
interaction_type: leave_at_door
location_source: TARGET
location_ref: 7ae1b58c-5731-dbd2-1988-5ea90e060950
profile: personal
```

### Example: Dry-Run Checkout Then Confirm

```sh
ordercli ubereats carts checkout 562a2132-07d5-4d17-8b16-545de34a2986
ordercli ubereats carts checkout 562a2132-07d5-4d17-8b16-545de34a2986 --confirm
```

Dry-run stdout shape:

```text
merchant: Rosa Mexicano
delivery_address: 222 E 39th St, New York, NY 10016-2754, US
location_source: TARGET
location_ref: 7ae1b58c-5731-dbd2-1988-5ea90e060950
profile: personal
subtotal: $25.30
fees: $4.32
taxes: $1.84
tip: not set
delivery_type: ASAP
interaction_type: leave_at_door
checkout_ready: true
```

### Example: Trace A Failing Flow

```sh
ordercli ubereats carts show 562a2132-07d5-4d17-8b16-545de34a2986 --trace
```

Example stderr shape:

```text
2026-04-12T22:00:00Z command=ubereats carts show interaction=request method=POST url=https://www.ubereats.com/_p/api/getDraftOrderByUuidV1
2026-04-12T22:00:00Z command=ubereats carts show interaction=response status=200
```

## Draft Skill.md

This section is a draft of the skill file an agent could use to operate the final Uber Eats CLI safely and consistently.

```md
---
name: ubereats-cli-operator
description: Use the Uber Eats portion of ordercli whenever the user wants to inspect Uber Eats orders, browse stores, search menu items, inspect saved delivery addresses, manage Uber Eats carts, or run a staged checkout. Use this skill for both read-only Uber Eats tasks and write-adjacent cart operations. Prefer this skill whenever the user mentions Uber Eats, Uber Eats orders, food delivery through Uber Eats, carts, menu items, restaurant search, checkout, or adding or removing items from an Uber Eats cart, even if they do not explicitly ask to use ordercli.
---

# Uber Eats CLI Operator

Use the `ordercli ubereats` CLI surface defined by the Uber Eats PRD.
Use the PRD as the source of truth for endpoint and transport details rather than duplicating those details inside the extracted skill.

## Command model

- Use `config show` and `config set` only when the user is configuring the provider or when the CLI cannot proceed without local settings.
- Use `login` when the Uber Eats session is missing or expired.
- Use `logout` only when the user explicitly wants to clear the local Uber Eats session.
- Use `orders list` and `orders show` for order inspection.
- Use `stores list`, `stores search`, `stores show`, and `stores menu` for merchant discovery.
- Use `items search` and `items show` for menu-item discovery.
- Use `carts list`, `carts show`, `carts create`, `carts items add`, `carts items update`, `carts items remove`, `carts update`, `carts discard`, and `carts checkout` for draft-cart work.
- Use `addresses list` and `addresses show default` for address inspection.

## Ref resolution rules

- Do not guess refs.
- If the user names an order but does not provide an `order-ref`, use `orders list` first, then `orders show`.
- If the user wants a store but does not provide a `store-ref`, use `stores search` or `stores list`.
- If the user wants an item in one known store, use `items search "<query>" --store <store-ref>` to resolve the `item-ref`.
- If the user wants an item but the store is not known, search stores first, then narrow to store-scoped item search.
- If the user wants to inspect or modify a cart but does not provide a `cart-ref`, use `carts list`, then `carts show`.
- If multiple candidate refs remain, present the shortlist instead of choosing silently.

## Safety rules

- Treat `login` as the only interactive visible-browser step.
- Reuse the CLI-managed browser profile for all later commands.
- Do not geocode raw addresses during normal operation. Use Uber-provided location context.
- Use `carts checkout <cart-ref>` as a dry run by default.
- Only run `carts checkout <cart-ref> --confirm` when the user explicitly asks to place the order.
- Do not remove cart items by setting quantity to zero. Use the cart-item removal command path.
- Do not add an item to an existing cart until the store match is known.
- Treat address inspection as read-only.

## Output rules

- Prefer `--json` when the result will be parsed or chained.
- Keep stdout clean.
- Treat stderr as the channel for diagnostics and `--trace`.
- Use `--trace` when debugging upstream request/response behavior.

## Location rules

- Resolve delivery coordinates from Uber session state.
- Prefer `getDeliveryLocationsV2` output.
- Fall back to the managed-session location cookie only when the endpoint is unavailable.

## Interaction examples

- To inspect a current order:
  1. run `ordercli ubereats orders list`
  2. identify the target `order-ref`
  3. run `ordercli ubereats orders show <order-ref>`
- To add an item to a cart when the user names a store and item:
  1. resolve the `store-ref` with `stores search` or `stores show`
  2. resolve the `item-ref` with `items search "<query>" --store <store-ref>`
  3. resolve the target `cart-ref` with `carts list` if needed
  4. run `carts items add <cart-ref> --item <item-ref>`
- To preview checkout:
  1. run `ordercli ubereats carts show <cart-ref>`
  2. run `ordercli ubereats carts checkout <cart-ref>`
  3. only run `--confirm` if the user explicitly wants to place the order

## Trigger examples

- "Show my Uber Eats orders"
- "Find sushi on Uber Eats"
- "What is in my Uber Eats cart?"
- "Add this item to my Uber Eats cart"
- "Preview my Uber Eats checkout"
```
