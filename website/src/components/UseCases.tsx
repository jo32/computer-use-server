import { useState } from 'react'
import { Icon } from './Icon'
import type { IconName } from './Icon'
import { Tabs } from './Tabs'

type UseCase = 'files' | 'development' | 'apps' | 'sharing'

const cases: { value: UseCase; label: string; icon: IconName; title: string; body: string; prompt: string; steps: string[]; tools: string[]; note: string }[] = [
  {
    value: 'files', label: '本机资料', icon: 'folder', title: '资料留在电脑里，Agent 直接来处理。',
    body: '让云端 Agent 读取已授权的文件夹，整理文档、分析表格，再把结果写回本机。省去反复上传和下载文件的步骤。',
    prompt: '整理 research 文件夹里的访谈记录，归纳共同问题，把报告保存到同一目录。',
    steps: ['读取你选定的资料目录', '分析内容，生成整理结果', '将报告写回本机文件夹'],
    tools: ['项目文件', '文件搜索', '读取与写入'],
    note: 'Agent 读取到的内容会传给接入它的云端服务。只开放这次任务需要的资料。',
  },
  {
    value: 'development', label: '开发环境', icon: 'terminal', title: '在你的环境里改代码、跑项目。',
    body: '项目依赖、开发工具和运行环境已经装在你的电脑上。让线上 Agent 使用它们修改代码、运行测试和构建，结果直接留在工作目录。',
    prompt: '修复 website 项目的手机布局，在这台电脑上运行构建，确认没有报错后告诉我改了什么。',
    steps: ['查看本机项目与代码', '修改文件，运行测试或构建', '返回命令输出与改动结果'],
    tools: ['项目文件', '本机终端', '实时输出'],
    note: '终端使用你当前账户的权限执行。文件工具的项目限制并不限制终端命令。',
  },
  {
    value: 'apps', label: '浏览器与桌面', icon: 'browser', title: '接着用你已登录的网页和应用。',
    body: '需要浏览器里的工作页面，或只能在本机打开的应用？按需开放 Chrome 和 macOS 桌面，让 Agent 截图、点击、输入，把网页与文件操作接起来。',
    prompt: '从我已打开的工作后台导出本月数据，保存到 reports 文件夹，再整理成一份月报。',
    steps: ['查看当前网页或桌面', '操作页面，导出需要的资料', '整理本机文件并返回结果'],
    tools: ['Chrome', 'macOS 桌面', '项目文件'],
    note: '浏览器和桌面可能触及已登录账号。涉及发送、付款或删除等动作，应先明确授权。',
  },
  {
    value: 'sharing', label: '临时分享', icon: 'link', title: '让可信任的人，用上这台电脑的工具。',
    body: '朋友或协作者也可以把分享地址交给自己的 Agent，使用你开放的项目、环境与应用。你在本机查看调用记录，决定何时暂停或结束分享。',
    prompt: '我把这个项目的 ReadyRig 接入信息发给你了。用你的 Agent 在我的电脑上构建项目，把结果保存到项目目录。',
    steps: ['你选择目录与工具，开启公网分享', '对方将接入信息交给自己的 Agent', '任务在你的电脑上执行，你查看记录'],
    tools: ['一次性公网链接', '本机权限', '执行记录'],
    note: '持有链接的人共用你开放的权限，也能查看日志与截图。仅分享给可信任的人。',
  },
]

export function UseCases() {
  const [selected, setSelected] = useState<UseCase>('files')
  const current = cases.find((item) => item.value === selected)!

  return <section className="use-cases section-width" id="use-cases" aria-labelledby="use-cases-title">
    <div className="section-heading"><span className="eyebrow">把它用在下一件事上</span><h2 id="use-cases-title">任务在云端安排。<br /><span>事情在你的电脑上完成。</span></h2><p>从自己的资料与环境开始，也能把开放的工具临时分享给别人。</p></div>
    <Tabs label="使用场景" options={cases.map(({ value, label }) => ({ value, label }))} value={selected} onChange={setSelected} className="use-case-tabs">
      <article className="use-case-panel">
        <div className="use-case-copy"><span className="feature-icon"><Icon name={current.icon} width="25" height="25" /></span><h3>{current.title}</h3><p>{current.body}</p><div className="feature-tags">{current.tools.map((tool) => <span key={tool}>{tool}</span>)}</div></div>
        <div className="task-example"><span className="task-label">你可以这样交代 · 场景示例</span><blockquote>{current.prompt}</blockquote><ol>{current.steps.map((step, index) => <li key={step}><span>{index + 1}</span>{step}</li>)}</ol></div>
        <div className="use-case-note"><Icon name="shield" width="16" height="16" /><p>{current.note}</p></div>
      </article>
    </Tabs>
  </section>
}
