# ReadyRig Website

The React, TypeScript, and Vite website shares ReadyRig's light/dark themes, segmented navigation, borders, and list styles. It is deployed with the `/console` device console on Cloudflare Workers; D1 stores accounts, heartbeats, and commands. See the [cloud guide](../cloud/README.md) for deployment and Google sign-in setup, and the [changelog](../CHANGELOG.md) for project history.

## Local development

Requires Node.js 22.12+; the current LTS release is recommended.

```sh
cd website
npm ci
npm run dev
```

Open [the local development site](http://127.0.0.1:5173). The main page includes an interactive app preview with example data. It does not connect to a local service or execute real tool calls.

```sh
npm run check     # Check TypeScript types.
npm run build     # Sync icons, check types, and build into dist/.
npm run preview   # Preview the production build at http://127.0.0.1:4173.
```

## Production hosting

The [website](https://readyrig.getmegaportal.com/) and [device console](https://readyrig.getmegaportal.com/console) use the `readyrig-cloud` Worker. Cloudflare serves static assets, Google sign-in, device management APIs, and D1 storage.

Deploy both from the cloud directory:

```sh
cd ../cloud
npm run deploy
```

The former Vercel project `readyrig` is paused, automatic Git deployments are disabled, and its custom domain binding has been removed. The project and deployment history remain available for recovery; `vercel.json` is a legacy configuration unused by current deployments. See the [cloud migration and recovery notes](../cloud/README.md#production-domain-and-recovery).

Web requests to the former `https://readyrig-cloud.megaportal.workers.dev` address redirect to the production domain. Its device endpoints continue accepting heartbeats and receipts from already bound apps.

## Downloads and links

Copy `.env.example` to `.env.local` and configure the build variables:

| Variable | Purpose |
| --- | --- |
| `VITE_DOWNLOAD_MAC_ARM64` | Public Apple Silicon download URL |
| `VITE_DOWNLOAD_MAC_AMD64` | Public Intel download URL |
| `VITE_REPOSITORY_URL` | Repository URL |
| `VITE_RELEASES_URL` | Releases page |
| `VITE_DOCS_URL` | Connection documentation |
| `VITE_RELEASES_REQUIRE_ACCESS` | Defaults to `false`; use `true` only for private release pages |

The repository is public. Default downloads point to the latest stable GitHub Release packages for Apple Silicon and Intel; variables can override them with other public package URLs. All `VITE_*` values are embedded in the website. Use public URLs only and never include tokens or other credentials. Rebuild after changing them.

## Shared icon assets

The website uses the app's source image: `../internal/brand/assets/readyrig-app-icon.png`.

- `npm run dev` and `npm run build` generate optimized website icons, favicons, an Apple Touch Icon, and social sharing images.
- The development server watches the source icon and refreshes the page when it changes.
- Run `npm run sync:brand` to update assets independently.
- Commit `public/brand/` with the source changes. If `website/` is copied alone, the build uses the bundled assets.

Replacing the app icon requires no React component changes. Commit generated assets and redeploy to update the production website; local edits do not update the deployed site.

The product is named **ReadyRig**, and the package is `readyrig-website`. Theme preferences use `readyrig-site-theme`, with the former `readrig-site-theme` key as a fallback to preserve existing preferences.

## Content and appearance

- `src/App.tsx`: introduction, features, getting started, FAQ, downloads, and footer.
- `src/components/UseCases.tsx`: interactive examples for multiple computers, local files, development environments, browser/desktop operations, and temporary sharing.
- `src/components/RemoteAccess.tsx`: one-agent-to-many-computers diagram, cloud prompt onboarding, platform demo links, and sharing permission boundaries.
- `src/components/AppPreview.tsx`: interactive preview and prompt, MCP, and REST examples.
- `src/config.ts`: link and download configuration.
- `public/install.sh`: checksum-verified curl installer for the ReadyRig CLI on macOS/Linux (amd64/arm64). The download section includes a copyable installation command and links to the CLI/VM guide. Override its URL with `VITE_CLI_INSTALL_URL` if needed. Deploy the website and publish CLI-capable release binaries before promoting the installation link; the installer rejects older binaries without the CLI commands.
- `src/styles.css`: app colors, layout, responsive behavior, and light/dark appearance.
- `index.html`: title, search/social metadata, icons, and initial appearance selection.

Appearance initially follows the system and can be changed and saved manually. Example connection URLs use placeholders; copy real URLs from the app's Connection page. For production, use absolute URLs on the production domain for `og:image` and `twitter:image`.

Repository links use the Invertocat from [GitHub's official brand resources](https://brand.github.com/foundations/logo). `public/brand/github-invertocat-black.svg` and `github-invertocat-white.svg` come directly from the [official asset archive](https://brand.github.com/GitHub_Logos.zip), preserving the original SVGs for light and dark appearance.

## Scenarios and agent connection guidance

The website leads with one agent managing multiple computers bound to the same account. The cloud prompt lets agents with HTTP access discover machines, choose a target, and manage tool switches, sharing, and pause controls. Project folders, Full Access, and system permissions are configured locally on each machine; computers must stay online and run ReadyRig. Single-computer public-link and MCP connections remain available. Scenario tasks and steps are examples that do not execute on the page.

Product descriptions are based on official sources:

- [Cue](https://cue.im/): personal agents with their own identity, computer, and tools.
- [Gemini Spark](https://blog.google/innovation-and-ai/products/gemini-app/next-evolution-gemini-app/): the [custom MCP connection documentation](https://support.google.com/gemini/answer/17209137) describes OAuth and DCR flows.
- [WorkBuddy cloud agents](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/CloudAgent): tasks run in independent cloud sandboxes; see the [web application](https://www.codebuddy.cn/work/).

Connection guidance follows the product author's hands-on feedback: Cue and WorkBuddy's web application accept the prompt generated by ReadyRig, while Gemini Spark requires remote MCP configuration. `src/components/PlatformDemo.tsx` illustrates connection, execution, and results for all three platforms using example data. It does not connect to external services or show recordings of the real third-party interfaces.

Sharing descriptions must match actual permissions. The full connection URL is a credential: holders can call enabled tools and view logs/screenshots. All connected clients share the same permissions without per-user isolation. Terminal commands run as the current user; browser and desktop tools may reach signed-in applications. File-tool project restrictions do not sandbox the host. Keeping execution records locally does not prevent tool results from reaching cloud agents. Stopping public sharing does not automatically cancel already running commands.

## Languages

The website, interactive demos, and cloud device console support English and Simplified Chinese. Choose Follow system, Simplified Chinese, or English in the top-right corner. Manual selection is saved in the browser across refreshes and pages. The initial choice follows supported languages in browser preference order, falling back to English. Use `?lang=en` or `?lang=zh-CN` to request a language explicitly.

Language detection and interpolation live in `src/locale.ts`; React state and page metadata live in `src/i18n.tsx`. English strings are in `src/locales/en.json`, using the original Chinese strings as translation keys. Example data follows language selection; user content is preserved. Add new interface strings to the dictionary.

Run `npm test` to verify detection, saved preferences, placeholders, translation coverage, and app connection prompts. The app dictionary is `../internal/server/assets/locales/en.js`; native menu/dialog translations are in `../internal/i18n/en.json`. Tests check consistency across them.
