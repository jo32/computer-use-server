import { AppIcon, Icon } from './Icon'

import { PlatformDemo } from './PlatformDemo'

export function CloudConnection() {
  return <div className="cloud-connection">
    <div className="connection-map" aria-label="云端 Agent 通过 ReadyRig 连接你的电脑">
      <div className="map-node"><Icon name="link" width="23" height="23" /><div><strong>云端 Agent</strong><span>对话、规划、发起任务</span></div></div>
      <div className="map-route"><span>公网连接</span><i aria-hidden="true" /></div>
      <div className="map-node map-readyrig"><AppIcon width="43" height="43" /><div><strong>ReadyRig</strong><span>连接、权限、执行记录</span></div></div>
      <div className="map-route"><span>本机执行</span><i aria-hidden="true" /></div>
      <div className="map-node"><Icon name="monitor" width="24" height="24" /><div><strong>你的电脑</strong><span>文件、终端、浏览器、桌面</span></div></div>
    </div>
    <div className="platform-intro"><span className="eyebrow">为这类线上 Agent 扩展本机能力</span><p>Agent 有自己的云端沙箱。你的资料、开发环境和应用，也可以成为它的工具。</p></div>
    <PlatformDemo />
  </div>
}

export function SharingSection() {
  return <section className="sharing-section section-width" id="sharing" aria-labelledby="sharing-title">
    <div className="sharing-card">
      <div className="sharing-copy"><span className="eyebrow">也可以分享给别人</span><h2 id="sharing-title">把这台电脑，<br />临时分享给可信任的人。</h2><p>把一次性公网接入信息交给朋友或协作者。他们的 Agent 就能调用你开放的工具，在你的电脑上处理任务。权限开关始终由你在本机控制。</p><a className="text-link" href="#getting-started">看看如何开启分享 <Icon name="chevron" width="14" height="14" /></a></div>
      <div className="sharing-controls"><div className="sharing-control-heading"><Icon name="shield" width="19" height="19" /><strong>分享前，明确这些边界</strong></div><ul>
        <li><strong>链接就是访问凭据</strong><p>拿到完整链接的人可以调用已开放的工具，并查看执行日志和截图。不要公开发布。</p></li>
        <li><strong>接入者共用同一套权限</strong><p>目前没有按用户隔离权限。终端、浏览器和桌面可能触及账户内的其他文件与已登录应用。</p></li>
        <li><strong>按需开放，用完收回</strong><p>只开放任务所需的目录与工具。结束时取消仍在执行的任务，再关闭公网分享；关闭链接不会自动中止已运行的命令。</p></li>
      </ul></div>
    </div>
    <p className="sharing-footnote">任务期间，电脑需要保持开机、联网，ReadyRig 持续运行。工具结果会返回接入的 Agent，请确认你和对方使用的云端服务适合处理这些资料。</p>
  </section>
}
