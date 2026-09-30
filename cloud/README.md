# ReadyRig Cloud Accounts and Device Control

The website, device console, and API share one Cloudflare Worker. D1 stores Google users, web sessions, device bindings, heartbeats, and command receipts. The service runs entirely on Cloudflare.

- [Website](https://readyrig.getmegaportal.com/)
- [Device console](https://readyrig.getmegaportal.com/console)
- D1 database: `readyrig-cloud`, configured in `wrangler.jsonc`.
- Web requests to the former `workers.dev` address redirect to the production domain. Device endpoints remain available to older apps; bound credentials require no migration.

See the [project README](../README.md) for the local app and the [changelog](../CHANGELOG.md) for version history.

## Production domain and recovery

`readyrig.getmegaportal.com` uses a proxied Cloudflare `AAAA 100::` record. The `readyrig-cloud` Worker's `readyrig.getmegaportal.com/*` route handles every path without a Vercel origin. The Google OAuth client includes the production callback, and the app's default cloud URL uses this domain.

The former Vercel project `readyrig` (`prj_eV8ONBF48n8ynWrOHvIbdkg0Iux2`, team `team_c4my9iL2mRllE300soc8NtBD`) is paused. Preview deployments and automatic Git/deploy-hook deployments are disabled, and the custom domain binding is removed. The project and deployment history are retained; `readyrig.vercel.app` returns `503 DEPLOYMENT_PAUSED`.

To restore the former static website, resume the Vercel service, re-add the domain, change Cloudflare DNS to `CNAME readyrig → 47d77d4c7476c439.vercel-dns-016.com` with DNS only and TTL Auto, and remove the Worker route. Adjust Vercel deployment policies and preview settings if automatic deployments are also needed. The former static website cannot provide the current device APIs; assess connected apps before restoring it.

## Configure Google sign-in

The existing deployment has a `ReadyRig Web` client in Google project `readyrig-510216`, verified through a real Google sign-in. Its Client ID is in `wrangler.jsonc`, and its Client Secret is stored as a Cloudflare Worker Secret.

For your own deployment, create a **Web application** OAuth client in [Google Cloud Console](https://console.cloud.google.com/auth/clients), configure the app branding, and select an External audience. This project requests only `openid email profile`. Under [Google's audience rules](https://support.google.com/cloud/answer/15549945), these basic identity requests do not require a test-user list or display an unverified-app warning while in Testing. Adding other scopes requires reviewing the audience and verification configuration.

The existing Google app remains in Testing with unverified branding, so the consent page displays the app domain. Showing the ReadyRig name and icon requires a homepage, privacy policy, terms of service, and brand verification. Basic identity sign-in currently works without that branding verification.

Add this Authorized redirect URI:

```text
https://readyrig.getmegaportal.com/auth/callback
```

Set `vars.GOOGLE_CLIENT_ID` in `wrangler.jsonc`. Store the Client Secret only as a Worker Secret:

```sh
cd cloud
npm ci
npx wrangler secret put GOOGLE_CLIENT_SECRET
npm run deploy
```

The secret command prompts in the terminal. Keep the Client Secret out of source code, `VITE_*` values, and chat. Google authorization uses the system browser, state, PKCE, and nonce. The server verifies the signature through Google JWKS, issuer, audience, expiry, nonce, and verified email. It requests no Drive or Gmail data scopes and stores no Google access or refresh tokens.

Check `GET /api/health`: `google_configured: true` means both configuration values are present. Successful sign-in also requires correct Google callback and audience settings.

## Use the cloud console

1. Open **ReadyRig → Connection → Cloud account**. The official URL is prefilled; you can enter your own deployment.
2. Click **Sign in with Google**, sign in through the system browser, verify the six-digit code shown in the app, and confirm device binding.
3. Sign in to the web console with the same account to view computer status, start/stop public sharing, choose temporary or fixed tunnels, rename devices, change file/terminal/browser/desktop switches, and pause/resume control.

Fixed domains and Tunnel Tokens remain locally configured and are not uploaded to the command database. Project folders, Full Access, and macOS permissions are managed locally. Binding authorizes that Google account to manage the supported switches; holders of the agent URL still cannot access account or management routes.

The app sends a heartbeat and retrieves one command every 15 seconds. Devices appear offline after 60 seconds without a heartbeat, and the website rejects new commands for offline devices. Network failures retry with backoff up to 60 seconds. Stopping the public tunnel leaves heartbeats active. Devices cannot execute commands after quitting, sleeping, or losing network access; heartbeat monitoring cannot remotely wake a sleeping or powered-off computer.

Commands queue in D1 and expire if not retrieved within five minutes. A retrieved command without confirmation after 90 seconds is marked as having an unknown result and is not automatically repeated. Receipts show success or failure; valid late receipts can complete unknown results. A successful tunnel-start command means the app accepted the request; connection readiness is reported separately in device status. After power loss, the app uses receipts saved before execution to report unconfirmed outcomes.

Device credentials are stored only in the private local data directory at `cloud/cloud.json` with `0600` permissions; the cloud stores only their SHA-256 hashes. Credentials do not follow HTTP redirects or get sent to a different service when the launch cloud URL changes. They remain valid until unbinding; web sessions expire after seven days. Unbinding revokes credentials and unfinished commands, but an already active tunnel must be stopped separately. The cloud stores defined device status without uploading local logs, screenshots, project paths, or Tunnel Tokens. Public agent URLs are visible to the device owner. Completed commands are retained for up to 30 days.

## Deploy your own service

```sh
cd website && npm ci
cd ../cloud && npm ci
npx wrangler login
npx wrangler d1 create my-readyrig-cloud
```

Replace the Worker name, `account_id`, `database_name`, `database_id`, `routes`, and `PUBLIC_ORIGIN` in `wrangler.jsonc`. Replace or remove `LEGACY_ORIGIN`, configure the Google Client ID, secret, and callback, then run:

```sh
npm run db:remote
npm test
npm run deploy
```

Point the app at your deployment through the cloud website field, `--cloud-url https://your-domain`, `READYRIG_CLOUD_URL`, or the build value `computer-use-server/internal/buildinfo.CloudURL`. Already bound devices continue using their original service; disconnect the account before switching services.

## Local development and verification

Requires Node.js 22.12+; Node.js 24+ is recommended for integration tests. Local and production D1 databases are separate.

```sh
cd cloud
npm run build
npm run db:local
npm run dev
```

Open [the local site](http://localhost:8787). Configure a local Google Client ID in `.dev.vars`, use `.dev.vars.example` for the Client Secret, and add `http://localhost:8787/auth/callback` to the OAuth client. `npm run dev` overrides `PUBLIC_ORIGIN` locally. The API rejects hostnames that do not match `PUBLIC_ORIGIN`.

```sh
npm test                         # SQLite and signed mock Google identities; account isolation, commands, revocation.
npm run check                    # Worker type checks.
cd ..
go test -race -tags nogui ./...   # Local credentials, receipts, redirects, and public route isolation.
node scripts/test-cloud.mjs       # Local Worker/D1 → Go app → harmless tunnel process → receipt.
```

Integration tests create temporary directories and local test users. They do not connect to production D1, open real public tunnels, or bypass production Google sign-in. Local ports 18787, 18789, 17431, and 17432 must be available. Add `--keep` to retain test pages for visual verification; cleanup occurs when the test stops.

References: [Cloudflare Workers static assets](https://developers.cloudflare.com/workers/static-assets/), [D1](https://developers.cloudflare.com/d1/), and [Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect).
