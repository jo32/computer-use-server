# ReadyRig 云端账号与设备控制

官网、设备控制台和 API 共用一个 Cloudflare Worker。D1 保存 Google 用户、网页会话、设备绑定、心跳和命令回执；无需 Vercel。

- 官网：<https://readyrig.getmegaportal.com/>
- 控制台：<https://readyrig.getmegaportal.com/console>
- D1：`readyrig-cloud`，配置见 `wrangler.jsonc`。
- 原 `workers.dev` 地址的网页访问会跳转到正式域名；旧版 app 的设备接口继续可用，已绑定凭证无需迁移。

## 正式域名迁移

`readyrig.getmegaportal.com` 使用 Cloudflare 代理记录 `AAAA 100::`，所有路径由 `readyrig-cloud` Worker 的 `readyrig.getmegaportal.com/*` 路由处理。该地址不再依赖 Vercel 源站。Google OAuth 客户端已加入正式域名回调；app 的默认云端地址也改为正式域名。

原 Vercel 项目 `readyrig`（`prj_eV8ONBF48n8ynWrOHvIbdkg0Iux2`，团队 `team_c4my9iL2mRllE300soc8NtBD`）已暂停；预览部署关闭，Git 与 deploy-hook 的自动部署策略禁用，原自定义域名绑定移除。项目和历史部署保留，`readyrig.vercel.app` 返回 `503 DEPLOYMENT_PAUSED`。

恢复原静态官网时，需在 Vercel 恢复服务并重新添加该域名，再将 Cloudflare DNS 改回 `CNAME readyrig → 47d77d4c7476c439.vercel-dns-016.com`（DNS only、TTL Auto），移除该 Worker 路由。若需要恢复自动发布，再调整 Vercel 的部署策略与预览开关。原静态官网无法提供现有云端设备 API，应先评估正在连接的 app。

## 配置 Google 登录

现有部署已在 Google 项目 `readyrig-510216` 中创建 `ReadyRig Web` 客户端，并完成真实 Google 登录验证。Client ID 配置在 `wrangler.jsonc`，Client Secret 已保存为 Cloudflare Worker Secret。

自己部署时，在 [Google Cloud Console](https://console.cloud.google.com/auth/clients) 创建 **Web application** OAuth 客户端，配置应用品牌和 External 受众。本项目只申请 `openid email profile`；根据 [Google 受众规则](https://support.google.com/cloud/answer/15549945)，这类基本身份请求在 Testing 状态下也无需加入测试用户名单，不会显示未验证应用警告。若以后添加其他权限，需要重新配置受众与审核。

现有 Google 应用仍为 Testing，品牌尚未验证，所以 Google 授权页显示应用域名。正式展示 ReadyRig 名称和图标，需要补齐首页、隐私政策、服务条款并完成品牌验证；这不影响当前基本身份登录。

将以下地址加入 Authorized redirect URIs：

```text
https://readyrig.getmegaportal.com/auth/callback
```

在 `wrangler.jsonc` 的 `vars.GOOGLE_CLIENT_ID` 填入 Client ID。Client Secret 只存入 Worker Secret：

```sh
cd cloud
npm ci
npx wrangler secret put GOOGLE_CLIENT_SECRET
npm run deploy
```

Secret 命令会在终端提示输入，不要把 Client Secret 写入源码、`VITE_*` 或聊天。Google 授权使用系统浏览器、state、PKCE 和 nonce；服务端通过 Google JWKS 验证签名、issuer、audience、有效期、nonce 和已验证邮箱。不请求 Drive、Gmail 等数据权限，不保存 Google access token 或 refresh token。

检查 `GET /api/health`：`google_configured: true` 表示两个配置项已填写；真实登录仍需 Google 的回调和受众配置正确。

## 使用

1. 打开 ReadyRig → 连接 → 云端账号。官方云端地址默认填好，也可填自己的部署地址。
2. 点击「使用 Google 登录」，在系统浏览器登录，核对 app 中显示的六位验证码并确认绑定。
3. 网页登录同一个账号，即可查看电脑状态、开启/关闭公网、选择临时或固定隧道、重命名电脑、调整文件/终端/浏览器/桌面开关、暂停/恢复控制。

固定域名和 Tunnel Token 仍在本机配置；不会上传到命令数据库。项目目录、Full Access 和 macOS 系统权限保持本机管理。账号绑定代表授权此 Google 账号管理上述开关；持有 Agent 地址的人依然不能访问账号或管理路由。

app 每 15 秒发送一次心跳并领取一条命令。60 秒没有心跳显示离线，网页禁止向离线电脑发送新命令。网络失败会退避重试，最长 60 秒。关闭公网隧道不影响心跳；退出、休眠或断网后无法执行。这里的心跳检测不能远程唤醒关机或休眠的电脑。

命令在 D1 中排队，5 分钟未领取就过期。领取后 90 秒未确认会标记结果未知，不自动重复执行。收到回执后显示成功或失败；晚到的有效回执可以补齐结果。隧道启动命令成功表示 app 接受启动请求，是否已经连接还要看设备上报的隧道状态。断电后 app 使用执行前保存的回执报告未确认结果。

设备凭证仅存本机私有数据目录的 `cloud/cloud.json`，权限为 `0600`；云端只保存 SHA-256。凭证不会跟随 HTTP 重定向，也不会因修改启动参数的云端地址而发送给其他服务。凭证可以持续使用直到解绑；网页会话 7 天后过期。解绑会撤销凭证和未完成命令，已经开启的隧道需另行关闭。云端只保存定义好的设备状态，不上传本机日志、截图、项目路径或 Tunnel Token；公网 Agent 地址会向设备所有者显示。完成命令保留最多 30 天。

## 自己部署

```sh
cd website && npm ci
cd ../cloud && npm ci
npx wrangler login
npx wrangler d1 create my-readyrig-cloud
```

在 `wrangler.jsonc` 更换 Worker 名称、`account_id`、`database_name`、`database_id`、`routes` 和 `PUBLIC_ORIGIN`；更换或移除 `LEGACY_ORIGIN`，配置 Google Client ID、Secret 及回调地址后：

```sh
npm run db:remote
npm test
npm run deploy
```

app 可通过「云端网站地址」、`--cloud-url https://your-domain`、`READYRIG_CLOUD_URL` 或构建值 `computer-use-server/internal/buildinfo.CloudURL` 指向自己的部署。已绑定设备使用原服务，切换服务需先断开账号。

## 本地开发与验证

Node.js 22.12+，集成测试建议 Node.js 24+。本地 D1 与生产 D1 分开：

```sh
cd cloud
npm run build
npm run db:local
npm run dev
```

本地网站为 `http://localhost:8787`。本地 Google Client ID 可通过 `.dev.vars` 配置；Client Secret 参照 `.dev.vars.example`，回调加入 `http://localhost:8787/auth/callback`。`npm run dev` 覆盖本地 `PUBLIC_ORIGIN`。接口拒绝与 `PUBLIC_ORIGIN` 不一致的域名。

```sh
npm test                         # SQLite + 模拟签名 Google 身份，账号隔离/命令/撤销测试
npm run check                    # Worker 类型检查
cd ..
go test -race -tags nogui ./...   # 本机凭证、回执、重定向及公开路由隔离
node scripts/test-cloud.mjs       # 真实本地 Worker/D1 → Go app → 无害隧道进程 → 回执
```

集成测试创建临时目录和本地测试用户；不连接生产 D1，不创建真实公网隧道，也不会绕过生产 Google 登录。需本机端口 18787、18789、17431、17432 空闲。加 `--keep` 可保留测试页面供视觉验证，停止后自动清理。

依据：[Cloudflare Workers 静态资源](https://developers.cloudflare.com/workers/static-assets/)、[D1](https://developers.cloudflare.com/d1/)、[Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect)。
