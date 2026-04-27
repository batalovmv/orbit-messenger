# UI Audit: Orbit vs Telegram Web A

## Metadata

- Date: 2026-04-05
- Auditor: Codex
- Orbit URL: `http://localhost:3000/#5f45385a-37e1-4341-936b-2f34dbeb7add`
- Telegram URL: `https://web.telegram.org/a/#938427394`
- Viewport: `1440x900`
- Goal: compare Orbit against live Telegram Web A and record every visible mismatch on the audited screen and its reachable menus, without making code changes.

## Scope And Caveats

- Comparison is done against two live, stateful pages under real accounts.
- The provided Orbit screen is a group chat (`QA Burst 0404`), while the provided Telegram screen is a direct chat (`Mihail Batalov`).
- Because of that, some differences are content-type driven, not automatically defects:
  - member count vs last seen
  - group-specific composer affordances
  - message avatar behavior
  - chat action availability in the top bar
- This audit still records those differences because they affect 1:1 visual parity on the actual compared screens.
- Findings below are split into:
  - confirmed mismatch
  - behavior bug
  - non-comparable / needs re-check on same chat type

## Artifacts

- Main Telegram screen: `D:\job\orbit\artifacts\telegram-main.png`
- Main Orbit screen: `D:\job\orbit\artifacts\orbit-main.png`
- Telegram side menu: `D:\job\orbit\artifacts\telegram-side-menu.png`
- Orbit side menu: `D:\job\orbit\artifacts\orbit-side-menu-open.png`
- Telegram more actions: `D:\job\orbit\artifacts\telegram-more-actions.png`
- Orbit more actions attempt: `D:\job\orbit\artifacts\orbit-more-actions.png`
- Telegram attachment menu: `D:\job\orbit\artifacts\telegram-attachment.png`
- Orbit attachment menu: `D:\job\orbit\artifacts\orbit-attachment.png`
- Telegram chat search overlay: `D:\job\orbit\artifacts\telegram-chat-search.png`
- Orbit chat search overlay: `D:\job\orbit\artifacts\orbit-chat-search.png`
- Telegram new message FAB menu: `D:\job\orbit\artifacts\telegram-new-message.png`
- Orbit new message FAB menu: `D:\job\orbit\artifacts\orbit-new-message.png`
- Telegram emoji picker: `D:\job\orbit\artifacts\telegram-emoji-picker.png`
- Orbit emoji picker: `D:\job\orbit\artifacts\orbit-emoji-picker.png`
- Telegram side menu -> More submenu: `D:\job\orbit\artifacts\telegram-side-more.png`

## Measurement Samples

### Shared shell metrics

Telegram:

- Search field rect: `x=101 y=7 w=193 h=40`
- Composer input rect: `x=601 y=832 w=541 h=48`
- More actions button rect: `x=1387 y=8 w=40 h=40`
- New message FAB rect: `x=296 y=916 w=48 h=48`
- Active chat row rect: `x=8 y=272 w=342 h=72`

Orbit:

- Search field rect: `x=101 y=7 w=145 h=40`
- Composer input rect: `x=601 y=832 w=541 h=48`
- More actions button rect: `x=1387 y=8 w=40 h=40`
- New message FAB rect: `x=296 y=916 w=48 h=48`
- Active chat row rect: `x=8 y=106 w=342 h=72`

Confirmed from measurement:

- Search input width is narrower in Orbit by `48px`.
- Composer input size matches Telegram on this viewport.
- Top-right action button sizing matches Telegram on this viewport.
- Active row width and height match; content structure and vertical placement differ.

## Findings Log

### F-001 Critical: Orbit throws a runtime alert during normal inspection

Type: behavior bug

Observed on Orbit while resizing/working with the page:

> Shoot! Something went wrong, please see the error details in Dev Tools Console.
> Cannot read properties of undefined (reading 'map')

Impact:

- The audited page is not stable under basic interaction.
- Any visual parity work is blocked by an app-level state bug because menus and overlays can fail or become unreliable during inspection.
- This is not a cosmetic gap. It is a product reliability issue visible to the user.

Status:

- Confirmed
- Needs reproduction by a developer with app logs and stack trace in source maps

### F-002 High: Orbit is not being compared against the same chat type as Telegram

Type: non-comparable / methodology risk

Observed:

- Orbit page is a group chat with `5 members`.
- Telegram page is a direct chat with `last seen today at 01:00`.

Impact:

- Some visible differences are expected because Telegram renders direct chats and group chats differently.
- A second pass should be run later on a Telegram group and an Orbit direct chat, or on strictly matched chat types.

Status:

- Confirmed
- Does not invalidate the audit, but it lowers confidence for top-bar and composer parity conclusions

### F-003 High: Sidebar top area does not match Telegram

Type: confirmed mismatch

Observed:

- Orbit search field is narrower (`145px` vs `193px`).
- Orbit shows an extra blue star icon to the right of the search field.
- Telegram does not show that star on the compared screen.

Impact:

- The first screen impression is visibly different before entering any chat content.
- This breaks pixel parity immediately in the highest-traffic part of the app.

Status:

- Confirmed from screenshots and DOM measurements

### F-004 Medium: Orbit chat list has materially different density and visual rhythm

Type: confirmed mismatch

Observed:

- Orbit list contains many more visible rows in the same viewport because the content block feels denser and more compressed.
- Telegram list on the compared screen shows a larger top promo block, more whitespace, and fewer rows visible at once.
- Orbit avatars are almost entirely initial-based solid circles on the audited screen.
- Telegram on the compared screen mixes photo avatars and richer visual identity.

Impact:

- Even where row dimensions are similar, the screen reads differently because the list carries a heavier, denser QA-tool feel instead of Telegram’s more spacious messenger feel.

Notes:

- Part of this is data/content driven.
- Part of it is still styling and spacing.

Status:

- Confirmed

### F-005 Medium: Active chat row geometry is close, but content composition is different

Type: confirmed mismatch

Observed:

- Active row size matches Telegram (`342x72`).
- Telegram active row contains avatar/photo, title, date, message preview and selected-state treatment.
- Orbit active row contains the same broad structure, but with a stronger “QA/test data” feel due to initials avatars and denser preview text.
- Orbit active row appears much higher in the list (`y=106` vs `y=272`) because the left column has different preceding content and no Telegram promo card.

Impact:

- This is not the biggest CSS miss, but it contributes to the overall feeling that the app is “Telegram-inspired” rather than “Telegram-identical”.

Status:

- Confirmed

### F-006 High: Orbit left side menu is structurally incomplete relative to Telegram

Type: confirmed mismatch

Telegram side menu contains:

- account block with avatar/name
- `Add Account`
- `My Profile`
- `Saved Messages`
- `Contacts`
- `Wallet`
- `Settings`
- `More`

Orbit side menu contains:

- `My Profile`
- `Saved Messages`
- `Contacts`
- `Settings`
- `More`

Missing in Orbit:

- account header block
- `Add Account`
- `Wallet`

Impact:

- This is not a tiny style drift. It is a different information architecture and a different menu silhouette.
- The menu’s top section is one of Telegram Web A’s most recognizable surfaces.

Status:

- Confirmed from `telegram-side-menu.png` and `orbit-side-menu-open.png`

### F-007 Medium: Orbit left side menu visual mass differs from Telegram

Type: confirmed mismatch

Observed:

- Telegram menu is taller, richer, and visually anchored by the profile block at the top.
- Orbit menu starts abruptly with plain items, so it feels shorter and lighter.
- Telegram menu reads as a full account drawer.
- Orbit menu reads as a compact utility popup.

Impact:

- Even if item styling is close, the missing top block changes the perceived product identity immediately.

Status:

- Confirmed

### F-008 High: Telegram more-actions menu opens; Orbit more-actions menu did not open during audit

Type: behavior bug

Observed:

- Telegram `More actions` button opens a full menu with:
  - `Edit`
  - `Video Call`
  - `Mute...`
  - `Select messages`
  - `Send a Gift`
  - `Disable Sharing`
  - `Block user`
  - `Delete Chat`
- In Orbit, the `More actions` icon visually reacted, but no menu became visible in the captured state.

Impact:

- This blocks parity review for one of the main top-bar action surfaces.
- It is either a behavior bug or the menu is missing for this chat type / state.

Status:

- Confirmed as an audit blocker
- Needs targeted retest in a stable Orbit session

### F-009 High: Attachment menu content does not match Telegram

Type: confirmed mismatch

Telegram attachment menu on the audited screen:

- `Photo or Video`
- `File`
- `Checklist`
- `Wallet`

Orbit attachment menu on the audited screen:

- `Photo or Video`
- `File`
- `Poll`

Differences:

- Orbit is missing `Checklist`
- Orbit is missing `Wallet`
- Orbit has `Poll`, which is not present in Telegram’s menu on this audited screen

Impact:

- This is a direct IA divergence in a primary composer menu.
- A user familiar with Telegram would immediately notice a different menu contract.

Status:

- Confirmed from `telegram-attachment.png` and `orbit-attachment.png`

### F-010 Medium: Attachment menu positioning and sizing are close but not identical

Type: confirmed mismatch

Observed:

- Both menus are bottom-right anchored above the composer.
- Telegram menu sits slightly higher and feels a bit taller because it contains four rows.
- Orbit menu is shorter and appears visually lighter because it has only three rows.

Impact:

- Less important than the content mismatch, but still breaks strict 1:1 parity.

Status:

- Confirmed

### F-011 Medium: Orbit header shell is Telegram-like, but the visible action set is not validated for parity

Type: non-comparable / needs re-check

Observed:

- Orbit top bar reproduces Telegram’s broad shell: avatar, title, subtitle, search, more actions.
- Telegram audited screen also shows call and a pinned-message area in the top bar.
- Orbit audited screen does not present the same top-bar combination because the compared chat type is different.

Impact:

- Layout is directionally close.
- Exact parity on the right-side controls cannot be signed off from this specific pair of screens.

Status:

- Needs a same-type chat comparison

### F-012 Medium: Orbit composer container is very close in geometry, but the surrounding affordances differ

Type: confirmed mismatch

Observed:

- Input size matches Telegram at this viewport (`541x48`).
- Orbit shows `Send message as...` above the composer on this audited screen.
- Telegram direct chat composer does not show that label.

Impact:

- Good news: the base composer size is close.
- Bad news: the overall composer area still reads differently because the affordance stack is not the same.

Status:

- Confirmed
- Needs re-check against a Telegram group/channel if the target is group parity rather than direct-chat parity

### F-013 Medium: Main content mood is not 1:1 even where components are similar

Type: confirmed mismatch

Observed:

- Telegram audited screen feels calmer, more photo-driven, and less dense.
- Orbit audited screen feels like a QA sandbox: many synthetic titles, repeated initials avatars, visible test artifacts, and a harder visual rhythm.
- Some of this is content, not CSS, but from a reviewer’s eye the product still does not read as indistinguishable from Telegram.

Impact:

- If the business goal is “user opens Orbit and cannot tell it apart from Telegram Web A”, current state is not there yet.

Status:

- Confirmed

### F-014 High: Orbit header is missing Telegram's story-list entry point

Type: confirmed mismatch

Observed:

- Telegram sidebar header includes `Open Story List`.
- Orbit sidebar header does not expose an equivalent story button.
- Orbit instead shows an extra blue star icon near the sidebar search area.

Impact:

- Top-left interaction map differs immediately.
- Users familiar with Telegram will notice that one control is missing and another unrelated one is present.

Status:

- Confirmed

### F-015 High: In-chat search overlay diverges significantly from Telegram

Type: confirmed mismatch

Telegram in-chat search overlay:

- large search field in the top bar
- close icon at the right end
- date jump button on the far right
- no visible filter tabs

Orbit in-chat search overlay:

- large search field in the top bar
- close icon at the right end
- extra person/profile icon in the top-right action area
- date button
- visible filter chips directly below search:
  - `Photos`
  - `Videos`
  - `Files`
  - `Links`

Impact:

- This is not a micro-difference.
- The whole search surface reads like a different product flow.

Status:

- Confirmed from `telegram-chat-search.png` and `orbit-chat-search.png`

### F-016 Medium: Orbit search overlay is richer, but not Telegram-identical

Type: confirmed mismatch

Observed:

- Orbit exposes media-category chips immediately.
- Telegram keeps the search overlay visually cleaner on the audited screen.

Impact:

- Even if Orbit is arguably more functional, it fails the stated goal of 1:1 parity.

Status:

- Confirmed

### F-017 Low: New message FAB menu is one of the closest-matching surfaces

Type: parity success

Observed:

- Telegram and Orbit both open a bottom-left popover from the FAB.
- Both show:
  - `New Channel`
  - `New Group`
  - `New Message`
- Overall position, container shape, and relation to the FAB are very close.

Impact:

- This is a positive parity area.
- It should be preserved while fixing surrounding shell mismatches.

Status:

- Confirmed from `telegram-new-message.png` and `orbit-new-message.png`

### F-018 Low: Emoji picker shell is visually very close to Telegram

Type: parity success

Observed:

- Orbit emoji panel matches Telegram closely in:
  - overall size
  - rounded card geometry
  - category rail
  - recent emoji block
  - bottom tab row
  - placement above the composer

Impact:

- This is another strong parity area.
- It suggests the fork still carries a lot of upstream Telegram UI structure here.

Status:

- Confirmed from `telegram-emoji-picker.png` and `orbit-emoji-picker.png`

### F-019 Medium: Orbit leaks raw internal ids in emoji-picker accessibility labels

Type: confirmed mismatch

Observed in Orbit snapshot:

- one tab exposes `StickersList.EmojiItem`
- one tab exposes `GifsTab`

Telegram equivalents expose user-facing labels:

- `Custom Emoji`
- `GIFS`

Impact:

- This may not be visible in the painted UI, but it indicates incomplete productization and weak parity in semantics / accessibility.
- It is exactly the kind of detail QA and assistive-tech users will catch.

Status:

- Confirmed

### F-020 High: Orbit left-menu `More` interaction is unstable / possibly click-through broken

Type: behavior bug

Observed:

- Telegram side menu `More` opens a second-level submenu with:
  - `Night Mode`
  - `UI Features`
  - `Telegram Features`
  - `Report a Bug`
  - `Switch to K Version`
  - `Install App`
  - version footer
- On Orbit, attempting to click `More` did not yield a comparable submenu during the audit.
- In one fresh pass, the click sequence changed the current Orbit hash to another chat instead of exposing an isolated `More` surface, which suggests click leakage or wrong layering.

Impact:

- This blocks parity for an entire navigation branch.
- It is also a real interaction-quality problem, not just a style issue.

Status:

- Confirmed as unstable
- Needs isolated reproduction on a clean session

### F-021 Medium: Telegram has richer secondary navigation states in the left drawer

Type: confirmed mismatch

Observed:

- Telegram side drawer supports a visible nested submenu.
- Orbit drawer, in the audited session, behaves as a flatter popup and did not reliably expose equivalent nested navigation.

Impact:

- The drawer interaction model feels simpler and less complete than Telegram Web A.

Status:

- Confirmed

### F-022 High: Orbit appears to leak or persist dirty overlay state across fresh tabs

Type: behavior bug

Observed:

- Fresh Orbit tabs repeatedly surfaced with non-baseline UI state already present in the accessibility tree:
  - left drawer items
  - new message menu items
  - attachment menu items
  - auxiliary floating menu items near jump controls
- Fresh Telegram tab returned to a cleaner baseline state much more reliably.

Impact:

- This contaminates visual comparison because screenshots can start from a polluted UI state.
- It also points to weak state reset / overlay teardown in Orbit itself.

Status:

- Confirmed during repeated fresh-tab checks

### F-023 Medium: Orbit floating unread/mention controls expose extra menu state not seen on Telegram baseline

Type: confirmed mismatch / possible state bug

Observed:

- Orbit snapshots repeatedly exposed `Mark All as Read` menuitems next to unread-reaction and mention-jump controls.
- Telegram baseline showed only the floating buttons themselves on the comparable screen, without those extra surfaced menu items.

Impact:

- Either Orbit intentionally diverges here, or hidden menu state is leaking into the active UI tree.
- In both cases this hurts 1:1 parity.

Status:

- Confirmed in multiple Orbit snapshots

### F-024 Medium: Audit revealed several areas where Orbit is close visually but not behaviorally stable

Type: synthesized finding

Examples:

- `More actions` top-right button reacts visually but did not produce a menu in Orbit.
- left drawer `More` could not be cleanly opened and appears unstable.
- runtime alert occurred during normal inspection.

Impact:

- Orbit is not far from Telegram in static styling on several surfaces.
- The bigger parity gap now is interactive stability and menu reliability.

Status:

- Confirmed

## Interaction Coverage

Covered and documented:

- main shell / default screen
- left sidebar top area
- active chat row
- left drawer
- left drawer `More` submenu on Telegram
- top-right `More actions` on Telegram
- top-right `More actions` attempt on Orbit
- attachment menu
- in-chat search overlay
- new message FAB menu
- emoji picker

Partially covered / blocked:

- voice recording behavior by click only
- exact parity of top-bar controls across chat types

Not reliably covered with current tool/state:

- message context menu by right click
- long-press / hold interactions
- hover-only states for every row and icon
- every deep submenu behind non-deterministic Orbit states

## Visible Control Inventory

### Telegram main-screen controls observed

- `Open menu`
- `Open Story List`
- left search input
- birthday promo card
- visible chat rows in sidebar
- `New Message` FAB
- header title block
- `Unpin message`
- `Search this chat`
- `Call`
- `More actions`
- pinned-message inline button
- `Go to next unread reactions`
- `Go to next mention`
- `Go to bottom`
- `Choose emoji, sticker or GIF`
- composer textbox
- `Add an attachment`
- `Record voice message`

### Orbit main-screen controls observed

- `Open menu`
- left search input
- extra blue star icon in sidebar header
- visible chat rows in sidebar
- `New Message` FAB
- header title block
- `Search`
- `More actions`
- reaction / mention / bottom floating controls
- `Choose emoji, sticker or GIF`
- composer textbox
- `Add an attachment`
- `Record voice message`
- `Send message as...` label above composer

### Telegram overlays / menus observed

- left drawer
- left drawer `More` submenu
- top-right `More actions` menu
- attachment menu
- chat-search overlay
- new-message FAB menu
- emoji picker

### Orbit overlays / menus observed

- left drawer
- left drawer `More` submenu
- top-right `More actions` menu
- attachment menu
- chat-search overlay
- new-message FAB menu
- emoji picker

## Open Items For Next Pass

- Re-run the audit on matched chat types:
  - Telegram group vs Orbit group
  - Telegram direct chat vs Orbit direct chat
- Reproduce the runtime alert with source-mapped stack trace in Orbit logs / console.
- Re-check why Orbit attachment menu is non-deterministic between clean tabs (`2-item` vs `3-item with Poll`).
- Open and compare:
  - message context menu on bubble
  - pinned message state on both sides
  - unread / mention / reaction jump controls with active badges
- Capture hover and pressed states for:
  - top-left burger
  - search input
  - chat row
  - paperclip
  - microphone
  - top-right more actions

## Second Pass Validation

Date: `2026-04-05`

Method:

- Second pass was run on fresh tabs to separate real visual mismatches from hidden DOM / a11y-tree residue.
- Visual screenshots were treated as source of truth whenever the accessibility tree exposed hidden containers.

Second-pass artifacts:

- Orbit baseline: `D:\job\orbit\artifacts\pass2\orbit-baseline-pass2.png`
- Telegram baseline: `D:\job\orbit\artifacts\pass2\telegram-baseline-pass2.png`
- Orbit drawer: `D:\job\orbit\artifacts\pass2\orbit-menu-pass2.png`
- Telegram drawer: `D:\job\orbit\artifacts\pass2\telegram-menu-pass2.png`
- Orbit drawer `More`: `D:\job\orbit\artifacts\pass2\orbit-side-more-pass2.png`
- Telegram drawer `More`: `D:\job\orbit\artifacts\pass2\telegram-side-more-pass2.png`
- Orbit header `More actions`: `D:\job\orbit\artifacts\pass2\orbit-header-more-pass2.png`
- Telegram header `More actions`: `D:\job\orbit\artifacts\pass2\telegram-header-more-pass2.png`
- Orbit search overlay: `D:\job\orbit\artifacts\pass2\orbit-search-pass2.png`
- Telegram search overlay: `D:\job\orbit\artifacts\pass2\telegram-search-pass2-clean.png`
- Orbit attachment menu: `D:\job\orbit\artifacts\pass2\orbit-attachment-pass2.png`
- Orbit attachment menu variant with `Poll`: `D:\job\orbit\artifacts\pass2\orbit-attachment-pass2-poll.png`
- Telegram attachment menu: `D:\job\orbit\artifacts\pass2\telegram-attachment-pass2.png`
- Orbit new message menu: `D:\job\orbit\artifacts\pass2\orbit-new-message-pass2.png`
- Telegram new message menu: `D:\job\orbit\artifacts\pass2\telegram-new-message-pass2.png`
- Orbit emoji picker: `D:\job\orbit\artifacts\pass2\orbit-emoji-pass2.png`
- Telegram emoji picker: `D:\job\orbit\artifacts\pass2\telegram-emoji-pass2.png`

Status review of prior findings:

- `F-003` confirmed again. Orbit still has the narrower sidebar search and the extra blue star instead of Telegram’s story entry point.
- `F-006` and `F-007` confirmed again. Orbit drawer is still structurally lighter and still lacks Telegram’s account block, `Add Account`, and `Wallet`.
- `F-008` refuted as a clean-session blocker. Orbit header `More actions` opens on fresh tabs. The first-pass failure was real in that session, but it is not reproducible enough to keep as “menu does not open”.
- `F-009` narrowed. Telegram attachment menu stayed stable with `Photo or Video`, `File`, `Checklist`, `Wallet`. Orbit reproduced in two variants:
  - `Photo or Video`, `File`
  - `Photo or Video`, `File`, `Poll`
- `F-010` confirmed again. Even when Orbit shows `Poll`, menu height and information architecture still differ from Telegram.
- `F-015` and `F-016` confirmed again. Orbit search overlay still adds visible chips and an extra top-right profile-like button; Telegram stays visually cleaner.
- `F-017` confirmed again. New message FAB menu remains one of the closest-matching surfaces.
- `F-018` confirmed again. Emoji picker shell remains visually close.
- `F-019` confirmed again. Orbit still exposes raw labels such as `StickersList.EmojiItem` and `GifsTab`, while Telegram exposes `Custom Emoji` and `GIFS`.
- `F-020` refuted as a broken interaction. Orbit drawer `More` submenu opens on fresh tabs.
- `F-021` confirmed, but re-scoped. The mismatch is now clearly content-level, not “submenu missing”.
- `F-022` narrowed. Second-pass clean screenshots did not visually reproduce dirty overlay leakage. Hidden overlay containers exist in both Telegram and Orbit DOM, so the first-pass wording was too strong.
- `F-023` narrowed. `Mark All as Read` items were present in Orbit’s accessibility tree, but second-pass inspection showed hidden / zero-opacity states rather than a guaranteed visible UI leak on baseline.

New findings from pass 2:

### F-025 High: Orbit drawer `More` submenu is incomplete relative to Telegram

Type: confirmed mismatch

Observed:

- Telegram `More` submenu contains:
  - `Night Mode`
  - `UI Features`
  - `Telegram Features`
  - `Report a Bug`
  - `Switch to K Version`
  - `Install App`
  - version footer `Telegram Web A 12.0.21`
- Orbit `More` submenu contains:
  - `Night Mode`
  - `UI Features`
  - `Telegram Features`
  - `Report a Bug`
  - version footer `Orbit Messenger 12.0.20`

Differences:

- Orbit is missing `Switch to K Version`
- Orbit is missing `Install App`
- footer branding/version is different

Impact:

- The submenu exists, but it is still not Telegram-identical in either IA or footer identity.

Status:

- Confirmed from `orbit-side-more-pass2.png` and `telegram-side-more-pass2.png`

### F-026 High: Orbit header `More actions` leaks raw internal labels

Type: confirmed mismatch

Observed in Orbit:

- `BlockUser`
- `DeleteChatUser`

Telegram equivalents on the compared screen:

- `Block user`
- `Delete Chat`

Impact:

- This is a productization / i18n leak visible directly in the UI.
- Even if the action set were otherwise correct, these labels immediately break parity and polish.

Status:

- Confirmed from `orbit-header-more-pass2.png`

### F-027 Medium: Orbit header `More actions` is content-mismatched even when it opens

Type: confirmed mismatch with chat-type caveat

Observed:

- Telegram direct-chat menu:
  - `Edit`
  - `Video Call`
  - `Mute...`
  - `Select messages`
  - `Send a Gift`
  - `Disable Sharing`
  - `Block user`
  - `Delete Chat`
- Orbit group-chat menu:
  - `Edit`
  - `Mute...`
  - `Start Video chat`
  - `Select messages`
  - `BlockUser`
  - `DeleteChatUser`

Interpretation:

- Some differences may be driven by group vs direct chat.
- Even with that caveat, Orbit still diverges visibly in naming, action count, and destructive-action labeling.

Status:

- Confirmed from `orbit-header-more-pass2.png` and `telegram-header-more-pass2.png`

### F-028 Medium: Orbit attachment menu is non-deterministic across clean tabs

Type: behavior / state bug

Observed:

- One clean Orbit tab exposed only:
  - `Photo or Video`
  - `File`
- Another clean Orbit tab exposed:
  - `Photo or Video`
  - `File`
  - `Poll`

Impact:

- This makes parity work harder because the same surface is not stable between runs.
- It also weakens confidence in earlier menu comparisons unless screenshots are captured on the exact same session.

Status:

- Confirmed from `orbit-attachment-pass2.png` and `orbit-attachment-pass2-poll.png`

## Current Verdict

- Orbit already borrows Telegram’s base layout strongly.
- It is not yet 1:1.
- Biggest audited gaps:
- incomplete left drawer structure
- drawer / header menus with different content and raw internal labels
- non-matching and non-deterministic attachment menu
- in-chat search overlay mismatch
- missing story-list entry point in the sidebar header
- sidebar top area mismatch
- runtime instability was observed at least once and still needs root-cause reproduction
- first-pass claims that Orbit drawer `More` and header `More actions` were completely broken do not hold on a clean second pass
- Even before deeper micro-comparison, the screen is still recognizably “a Telegram-like fork” rather than a visually indistinguishable Telegram Web A clone.
