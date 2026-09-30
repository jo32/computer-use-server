# Changelog

Notable changes to ReadyRig are recorded here. For setup and current behavior, see the [README](README.md).

## Unreleased

### Changed

- Reorganized the project READMEs in English and moved version history into this document.

## 0.6.0

### Added

- Google account binding and cloud device management through a Cloudflare Worker and D1. Bound devices report heartbeats and receive sharing, capability, and pause/resume commands, with expiry and execution receipts.
- Persistent project folders and default selection, a `list_projects` tool, optional project selection for file and terminal tools, and a Full Access switch for the current run.
- English and Simplified Chinese across the website, desktop app, browser console, cloud console, native menus, authorization dialogs, and connection prompts, with saved preferences.
- Temporary Cloudflare Quick Tunnel sharing, fixed links through named tunnels, and a public read-only console, with credential storage, route validation, and connection diagnostics.
- A connection prompt that checks `help` and `list_projects` before an agent begins work.
- A menu bar quick panel and animated computer icon, including execution and pause states and Reduce Motion support.
- A local macOS file authorization dialog for Chrome's `DevToolsActivePort`.

### Changed

- Standardized the product name, app bundle, commands, website, and shared icon as ReadyRig, retaining legacy data directories, signing identity, update variables, and release assets for upgrade compatibility.
- Moved the website and cloud console to `readyrig.getmegaportal.com` on Cloudflare Workers. Legacy `workers.dev` web requests redirect to the production domain while existing device API credentials remain usable.
- Chrome tool definitions now load before the debugging connection is ready. Tools stay listed while waiting for Chrome or debugging file authorization, and calls return the relevant connection status.
## 0.5.1

### Fixed

- Preserved macOS bundle signatures when creating and extracting update archives, with regression coverage for signature metadata.

## 0.5.0

### Added

- Automatic updates modeled on Magpie's lifecycle: release builds check five seconds after startup and every six hours, download in the background, and install on restart or normal exit.
- Update progress, release notes, and restart/install controls in the menu bar and Connection page, plus `version` and `update` commands.
- GitHub private-release authentication through local `gh` credentials or a read-only update token, with credentials restricted to GitHub API requests.
- Platform, architecture, and build-specific asset selection, size and SHA-256 verification, and macOS signature, signing-team, and bundle-ID checks.
- Download retries, duplicate-check merging, retention of completed downloads after network failures, rollback on replacement failure, and protection against concurrent installation updates.
- Manual update guidance for development builds, read-only installations, and Homebrew-managed installations.
- Local-only update status, check, and restart endpoints.
- Configurable release repositories and Magpie-compatible update feeds, with HTTPS required except on loopback addresses.
- A GitHub release workflow for macOS desktop packages and macOS/Linux/Windows browser binaries, plus a local release build command and optional macOS signing identity.
- An integration check for updating a real binary from 0.4.0 to 0.5.0 while retaining logs and workspace data.

## 0.4

### Added

- A Go bridge to the official Chrome DevTools MCP, exposing dynamically discovered browser tools through REST, MCP, OpenAPI, and the Tools page.
- Local Chrome debugging discovery, configurable debugging endpoints and profile directories, and support for an installed MCP executable or the pinned npm package.
- Browser call logging with preserved upstream schemas, annotations, text, images, and structured results. Browser screenshots appear in call details and JSON logs.
- Serial browser execution with a two-minute limit, cancellation and reconnection, and local capability controls.
- Local-only debugging address validation and default disabling of upstream usage statistics and CrUX queries.

## 0.3

### Changed

- Rebuilt the console using Magpie's theme and components: centered segmented navigation, 48-pixel tool rows, connected statistics, gray surfaces, rounded lists, and inline input/result details.
- Replaced the sidebar, promotional heading, and separate detail panel with the inline layout, using Magpie's light and dark theme values while retaining the product's tool capabilities.

## 0.2

### Added

- Live command output saved when stdout/stderr changes, with console updates that do not consume unread agent `write_stdin` output.
- Per-call cancellation of the selected task and process group, retaining output and marking the call `cancelled`.
- Readable terminal output, file contents, directory tables, and search results, with full-result copying, expandable raw JSON, and stacked details in narrow windows.
- Desktop action replay using saved same-session reference frames, click/drag markers, before/after views, timeline seeking, and playback speed controls. Missing frames are identified rather than inferred.
- Summary-only call lists with full details loaded on demand, preserving expanded state and reading position while avoiding repeated transfer of long output.
- Capability isolation: disabling one category cancels only its in-flight calls.
- Local-only call detail and cancellation endpoints: `GET /api/calls/{id}` and `POST /api/calls/{id}/cancel`.
