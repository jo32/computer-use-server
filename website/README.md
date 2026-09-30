# ReadyRig 官网

与 ReadyRig 桌面控制台保持相同的灰白 / 深色主题、分段导航、细边框和列表样式。独立的 React + TypeScript + Vite 项目，输出静态网站，可在 Vercel 发布。

## 本地开发

需要 Node.js 22.12+（推荐当前 LTS）。

```sh
cd website
npm ci
npm run dev
```

打开 http://127.0.0.1:5173。主区域包含可交互的 App 预览，所有预览数据为示例，不连接本机服务，也不执行真实工具调用。

```sh
npm run check     # TypeScript 检查
npm run build     # 同步图标、检查类型、构建到 dist/
npm run preview   # 预览发布构建：http://127.0.0.1:4173
```

## 在 Vercel 发布

正式官网：https://readyrig.vercel.app 。Vercel 项目为 `jo32s-projects/readyrig`，已连接此仓库，生产分支为 `main`，根目录为 `website`。推送官网更新后自动部署。

1. 导入此 Git 仓库，设置 **Root Directory** 为 `website`。
2. Framework Preset 选择 **Vite**，Node.js 选择 **24.x**。
3. Build Command 为 `npm run build`，Output Directory 为 `dist`，Install Command 为 `npm ci`。
4. 如需公开下载，配置下方的下载地址，然后发布。

建议开启 **Include source files outside of the Root Directory in the Build Step**，让构建可以读取 App 的图标源文件；如希望 App 图标变更也自动触发官网部署，需确保 Ignored Build Step 不跳过此类提交。即使未开启，官网也能使用已提交的 `public/brand/` 资源独立构建。

目录内的 `vercel.json` 已包含构建设置和静态资源缓存规则。无需 Go 后端、数据库或服务端凭据，也不需要改动桌面 App 的发布流程。

参考：[Vercel 的 Vite 部署文档](https://vercel.com/docs/frameworks/frontend/vite)。

## 下载与链接

复制 `.env.example` 为 `.env.local`，或在 Vercel 项目中配置同名变量：

| 变量 | 用途 |
| --- | --- |
| `VITE_DOWNLOAD_MAC_ARM64` | Apple Silicon 的公开安装包地址 |
| `VITE_DOWNLOAD_MAC_AMD64` | Intel 的公开安装包地址 |
| `VITE_REPOSITORY_URL` | 仓库地址 |
| `VITE_RELEASES_URL` | 版本发布页面 |
| `VITE_DOCS_URL` | 接入文档 |
| `VITE_RELEASES_REQUIRE_ACCESS` | 默认为 `false`；仅私有版本页面设为 `true` |

仓库已公开。未配置安装包地址时，按钮打开公开的 GitHub Releases。配好安装包地址后，对应处理器的按钮自动切换为「下载 macOS 版」。`VITE_*` 都会打包进网页，只填写公开 URL，不能放 token 等凭据。修改后需要重新构建。

## 图标同步

官网使用 App 的同一张源图：`../internal/brand/assets/readyrig-app-icon.png`。

- `npm run dev` 与 `npm run build` 会自动生成优化后的官网图标、favicon、Apple Touch Icon 与社交分享图。
- 开发服务器会监听 App 图标源文件，变更时自动同步并刷新页面。
- 单独更新资源可执行 `npm run sync:brand`。
- `public/brand/` 应一并提交。若单独复制 `website/`，构建会继续使用已打包资源。

后续更换 App 图标无需修改 React 组件。线上官网需要提交生成后的资源并重新部署；本地文件变更不会直接修改线上网站。

产品名称统一为 **ReadyRig**，官网包名为 `readyrig-website`。外观偏好使用 `readyrig-site-theme`，并读取改名前的 `readrig-site-theme` 作为回退，保留已有用户的外观设置。

## 内容与外观

- `src/App.tsx`：介绍、功能、上手步骤、FAQ、下载和页脚。
- `src/components/UseCases.tsx`：本机资料、开发环境、浏览器与桌面、临时分享四个交互场景。
- `src/components/RemoteAccess.tsx`：云端到本机的连接示意、平台交互演示入口和分享权限边界。
- `src/components/AppPreview.tsx`：交互预览与 Prompt / MCP / REST 接入示例。
- `src/config.ts`：链接与下载配置。
- `src/styles.css`：App 配色、布局、响应式与深浅色外观。
- `index.html`：标题、搜索 / 分享元数据、图标和首次外观设置。

外观首次跟随系统，可手动切换并保存。所有示例连接地址明确使用占位符，实际地址应从 App 的「连接」页面复制。发布到正式域名时，建议把 `og:image` 和 `twitter:image` 改成该域名下的完整 URL。

GitHub 仓库链接使用 [GitHub 官方品牌资源](https://brand.github.com/foundations/logo)中的 Invertocat。`public/brand/github-invertocat-black.svg` 与 `github-invertocat-white.svg` 直接来自[官方素材包](https://brand.github.com/GitHub_Logos.zip)，保持原始 SVG，分别用于浅色与深色外观。

## 场景与接入说明

官网首先介绍「让线上 Agent 使用本机工具」和「将已开放的工具临时分享给可信任的人」，再展示控制台与工具清单。场景里的任务与步骤均为示例，并不在网页上执行。

产品介绍依据官方来源：

- [Cue](https://cue.im/)：个人 Agent 有自己的身份、电脑与工具。
- [Gemini Spark](https://blog.google/innovation-and-ai/products/gemini-app/next-evolution-gemini-app/)：[自定义 MCP 接入文档](https://support.google.com/gemini/answer/17209137)列出了 OAuth / DCR 流程。
- [WorkBuddy 云端 Agent](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/CloudAgent)：独立云端沙箱运行任务；[网页版入口](https://www.codebuddy.cn/work/)。

接入说明按产品作者的实测反馈编写：Cue 和 WorkBuddy 网页版直接粘贴 App 生成的 Prompt；Gemini Spark 需要配置远程 MCP。`src/components/PlatformDemo.tsx` 提供三个平台的接入、执行、结果演示，使用示例数据，不连接外部服务，也不是第三方产品真实界面的录屏。

分享说明应始终与实际权限一致：完整链接就是访问凭据，持有者可调用已启用工具并查看日志 / 截图；多个接入者共用同一套权限，没有按用户隔离。终端使用当前用户权限，浏览器与桌面可触及已登录应用；文件工具的项目目录限制不是系统沙箱。执行记录保存在本机不意味着工具结果不传到云端。关闭公网入口不会自动取消已运行的命令。
