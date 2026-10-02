# Changelog

Notable changes to ReadyRig are recorded here. For setup and current behavior, see the [README](README.md).

## 0.6.9

### Added

- Cloud MCP with OAuth authorization, automatic client registration, revocable access, and tool discovery and execution through an owned computer's public connection.
- A Connect MCP dialog in the cloud console for connection details and client management.

### Fixed

- Adding a project folder in the macOS app now uses the system folder picker attached to the main window. Cancelling leaves project access unchanged and allows the picker to be opened again.

## 0.6.8

### Added

- Unapproved Screen Recording and Accessibility permissions in the macOS app now have shortcuts that open the corresponding System Settings pane.

### Fixed

- The update help text now correctly says capability switches restore their last selection after restarting.

## 0.6.7

### Fixed

- File, terminal, desktop, and Chrome capability switches now save the last selection and restore it on restart, including changes through the local dashboard, CLI/TUI, and bound cloud account. Update restarts retain current switches instead of replaying stale capability flags.

## 0.6.6

### Fixed

- The macOS app now relaunches through LaunchServices after an in-app update. This starts a fresh app process and preserves launch arguments, preventing macOS from rejecting its menu bar icon and window activation as an exiting process.
- If an older version has already restarted with a missing menu bar icon, fully quit ReadyRig and reopen it from Applications once to restore the icon.

## 0.6.5

Version 0.6.4 did not publish installation packages. This release includes all changes prepared for that version.

### Added

- An interactive setup guide that opens automatically after curl installation. Reinstalling preserves existing settings; unattended installation can skip the guide.
- A terminal dashboard opened by running `readyrig` without a command. It manages service state, capabilities, sharing, projects, tools, and recent activity; exiting keeps the service running.
- A CLI bundled with the macOS app, registered in the user's command path on first launch and updated with the app. Separately installed CLI binaries are preserved.
- A local setup prompt in the app's Connection page, with English and Chinese copy, the matching CLI executable, and the current data directory. Local agents can inspect, configure, and verify ReadyRig through CLI commands.

### Changed

- `serve` and `web` start a detached daemon on macOS/Linux, wait for readiness, and return to the terminal. Added `stop` and `restart`; `--foreground` remains available for supervisors and debugging, including systemd units.
- The app reads saved CLI startup settings on launch. Project management and runtime switches work while the app is open; startup settings are changed while the instance is stopped.
- Website installation instructions describe the guide and default terminal dashboard. Removed obsolete documentation screenshots.

### Fixed

- Pasting long or Unicode project paths into the terminal dashboard no longer redraws the entire screen for every character, keeping input responsive on slower terminals.
- Chrome approval-mode debugging can be detected through its local server before reading protected profile files. Background detection uses a rejected probe path to avoid opening approval dialogs; browser operations still require Chrome approval.

## 0.6.3

### Added

- A checksum-verified curl installer for the macOS/Linux CLI on amd64 and arm64, plus a copyable installation command on the website.
- Saved CLI startup settings, local status and connection discovery, project and capability management, tool invocation with stable sessions and JSON output, public sharing, and cloud device binding from a headless VM.
- Linux systemd user service installation and lifecycle commands. CLI administration on macOS/Linux uses a private Unix socket shared with the app's existing backend, without writing dashboard credentials to disk.
- Local project menus can open folders with the system file manager or, on macOS, a compatible installed application selected from its name, icon, and default association. The shared local endpoints also support opening files within authorized project boundaries.
- A cloud connection prompt with a Bearer credential tied to the current login session. Agents can list bound computers and public links, change capability switches, start or stop sharing, and pause or resume control, with asynchronous command receipts. Signing out invalidates the cloud credential; direct computer tools continue to use public links without a token.

## 0.6.2

### Fixed

- Chrome DevTools MCP can connect when Finder launches ReadyRig with an incompatible Node.js version on PATH. ReadyRig checks installed Node versions, skips unsupported versions, and uses a supported Homebrew, Volta, mise, or nvm installation for the MCP component and its subprocesses.
- Chrome runtime diagnostics report the detected Node.js version and path when no supported installation is available.

## 0.6.1

### Fixed

- Mac releases require Developer ID Application signing, hardened runtime, secure timestamps, and Apple notarization. Both architectures and legacy compatibility App packages receive stapled tickets and pass Gatekeeper assessment before packaging.
- Release builds stop when signing or notarization credentials are missing; CI does not overwrite an existing release.

Moving from the ad-hoc signatures in 0.6.0 and earlier to Developer ID requires one manual installation. Subsequent updates retain the same signing team and bundle identifier.

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
