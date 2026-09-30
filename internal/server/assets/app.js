'use strict';
const $=id=>document.getElementById(id), esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const state={page:'activity',data:null,calls:[],total:0,selected:null,session:'',category:'',query:'',status:'',offset:0,frames:[],frame:0,tool:null,connected:false,detail:null,detailVersion:0,replayDetail:null,replaySide:'after',replayVersion:0,frameSignature:'',framesVersion:0};
const labels={success:'成功',error:'失败',denied:'已拦截',running:'运行中',interrupted:'已中断',cancelled:'已取消'}, icons={files:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="M3 6h7l2 2h9v12H3Z"/><path d="M3 8V4h7l2 2h8v2"/></svg>',terminal:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="m7 9 3 3-3 3m6 0h4"/></svg>',computer:'<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="3" y="3" width="18" height="13" rx="2"/><path d="M8 21h8M12 16v5"/></svg>'};
icons.browser='<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3" y="3" width="18" height="18" rx="4"/><path d="M3 8h18M7 5.5h.01M10 5.5h.01"/></svg>';
icons.system='<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-1 .4-1 1-1 2.2M12 16h.01"/></svg>';
const descriptions={help:'查看当前开放的工具、完整参数定义和启用状态。',read_file:'读取工作区文本或二进制文件，支持按行查看。',write_file:'在工作区内原子写入文件，自动创建上级目录。',list_directory:'列出目录中的文件、大小和最后修改时间。',search_files:'在工作区内搜索文本，返回文件路径和行号。',exec_command:'运行终端命令，可获取执行状态和后续输出。',write_stdin:'向运行中的命令输入内容、获取结果或停止进程。',computer_screenshot:'拍摄主屏幕快照，自动缩放并记录坐标映射。',computer_action:'点击、输入、滚动或拖拽，并获取操作后的截图。'};
const examples={help:{},read_file:{path:'README.md',start_line:1,end_line:20},write_file:{path:'notes/hello.txt',content:'Hello from Relay'},list_directory:{path:'.'},search_files:{path:'.',query:'TODO'},exec_command:{command:'pwd',cwd:'.',timeout:30,yield_time_ms:1000},write_stdin:{session_id:'填写进程 session_id',chars:'',yield_time_ms:1000},computer_screenshot:{},computer_action:{action:'left_click',frame_id:'填写刚获取的 frame_id',coordinate:[100,100],capture_after:true}};
let refreshing=false,again=false,toastTimer,searchTimer,replayTimer,eventStream;
const uiSession='console-'+(sessionStorage.getItem('relay-session')||crypto.randomUUID());sessionStorage.setItem('relay-session',uiSession.replace(/^console-/,''));
async function api(path,body){const res=await fetch(path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json','X-Session-ID':uiSession,'X-Client-Name':'Local console'},body:body===undefined?undefined:JSON.stringify(body)});const data=await res.json();if(!res.ok){const e=new Error(data.error||'请求失败');e.data=data;throw e}return data}
function toast(message){$('toast').textContent=message;$('toast').classList.remove('hidden');clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').classList.add('hidden'),3500)}
function error(message){$('error-banner').textContent=message;$('error-banner').classList.toggle('hidden',!message)}
function badge(status){return `<span class="badge ${esc(status)}">${status==='success'?'✓ ':status==='running'?'◌ ':''}${esc(labels[status]||status)}</span>`}
function time(date){return new Date(date).toLocaleTimeString('zh-CN',{hour12:false})}
function duration(ms){return ms<1000?`${ms} ms`:`${(ms/1000).toFixed(2)} s`}
function empty(glyph,title,copy,button=''){return `<div class="empty"><div class="empty-glyph">${glyph}</div><h3>${esc(title)}</h3><p>${esc(copy)}</p>${button}</div>`}
function params(){return new URLSearchParams({q:state.query,session:state.session,category:state.category,status:state.status,limit:'40',offset:String(state.offset),view:'summary'})}
async function refresh(){if(refreshing){again=true;return}refreshing=true;try{const [data,list]=await Promise.all([api('/api/state'),api('/api/calls?'+params())]);state.data=data;state.calls=list.calls;state.total=list.total;state.connected=true;renderGlobal();renderCalls();await loadDetail();if(state.page==='tools')renderTools();if(state.page==='settings')renderSettings();if(state.page==='replay')await loadFrames();error('')}catch(e){state.connected=false;relayWindow.setActivity({connected:false});$('live').classList.add('off');$('live').textContent='连接已断开';error(e.message)}finally{refreshing=false;if(again){again=false;void refresh()}}}
function renderGlobal(){const d=state.data;relayWindow.setActivity({connected:state.connected,paused:d.paused,running:d.summary.running});renderUpdate(d.update);$('app-version').textContent=d.version;const s=d.summary;$('platform').textContent=`${d.permissions.platform} · 设备在线`;$('stat-total').textContent=s.total;$('stat-running').textContent=s.running;$('stat-latency').innerHTML=s.total?`${s.avg_ms>=1000?(s.avg_ms/1000).toFixed(1):s.avg_ms}<em>${s.avg_ms>=1000?'s':'ms'}</em>`:'—';$('stat-screens').textContent=s.screenshots;$('pause').textContent=d.paused?'恢复控制':'暂停控制';$('pause').classList.toggle('danger',d.paused);$('pause-banner').classList.toggle('hidden',!d.paused);$('live').classList.toggle('off',d.paused);$('live').innerHTML=`<i></i>${d.paused?'控制已暂停':'服务已连接'}`;$('footer-status').textContent=`${d.tools.length} 个工具 · 本地日志 · ${s.failed} 次异常`;
 const sessionOptions='<option value="">全部会话</option>'+d.sessions.map(s=>`<option value="${esc(s.id)}">${esc(s.client||s.id)} · ${esc(s.id.slice(0,12))}</option>`).join('');if($('session-filter').innerHTML!==sessionOptions){$('session-filter').innerHTML=sessionOptions;$('session-filter').value=state.session}
}

function callError(c){return c.status==='cancelled'?'调用已取消，已产生的输出仍然保留。':c.error}
function summary(call){const a=call.arguments||{};if(call.error&&call.status!=='cancelled')return call.error;return a.command||a.path||a.name||a.url||a.action||a.query||(call.tool==='computer_screenshot'?'捕获主屏幕 · JPEG':'查看调用参数与结果')}
function renderCalls(){
 const focused=state.detail?.call;const calls=focused?.id===state.selected&&!state.calls.some(c=>c.id===state.selected)?[focused,...state.calls]:state.calls;
 $('result-count').textContent=state.total;

 const callsHTML=calls.length?calls.map(c=>`<div class="call-item ${c.id===state.selected?'open':''} ${c.status==='error'||c.status==='denied'?'bad':''}"><button class="call" data-call="${esc(c.id)}" aria-expanded="${c.id===state.selected}" aria-label="${esc(c.tool+' '+labels[c.status]+' '+time(c.started))}"><span class="when"><i class="call-chev">›</i>${time(c.started)}</span><span class="m">${esc(c.tool)}</span><span class="p">${esc(summary(c))}</span><span class="a">${esc(c.client)}</span><span class="st">${badge(c.status)}</span><span class="duration">${c.status==='running'?'执行中':duration(c.duration_ms)}</span></button>${c.id===state.selected?'<div id="detail"></div>':''}</div>`).join(''):empty('','还没有调用记录','连接 Agent 后，工具调用和执行结果会出现在这里。','<button class="button" data-test-files>测试读取目录</button>');
 const html=callsHTML+(calls.some(c=>c.id===state.selected)?'':'<div id="detail" class="hidden"></div>');
 if($('calls')._html!==html){
  const old=$('detail');
  $('calls').innerHTML=html;$('calls')._html=html;
  // Retain a selected expansion and its reading position when other rows change.
  if(old?.dataset.call===state.selected&&!$('detail').classList.contains('hidden'))$('detail').replaceWith(old);
 }
 $('pagination-info').textContent=state.total?`${state.offset+1}–${Math.min(state.offset+40,state.total)} / ${state.total} 条调用`:'日志仅保存在本机';
 $('previous').disabled=state.offset===0;$('next').disabled=state.offset+40>=state.total;
 if(!state.selected)renderDetail(null);
}
async function loadDetail(){
 const id=state.selected,version=++state.detailVersion;
 if(!id){state.detail=null;renderDetail(null);return}
 try {const data=await api('/api/calls/'+encodeURIComponent(id));if(version!==state.detailVersion||id!==state.selected)return;state.detail=data;renderCalls();renderDetail(data.call)}
 catch(e){if(version===state.detailVersion){state.detail=null;renderDetail(null);toast(e.message)}}
}
const pretty=v=>esc(JSON.stringify(v,null,2)??'null');
function outputView(c){
 const r=c.result||{};
 if(c.category==='browser'&&Array.isArray(r.content))return `<div class="browser-output">${r.content.map((part,i)=>part.type==='text'?`<pre data-scroll="browser-${i}">${esc(part.text)}</pre>`:part.type==='image'&&/^image\/(png|jpeg|webp)$/.test(part.mimeType)&&/^[A-Za-z0-9+/=\r\n]+$/.test(part.data)?`<img src="data:${esc(part.mimeType)};base64,${esc(part.data)}" alt="Chrome 返回的页面截图">`:'').join('')}</div>`;

 if(c.category==='terminal')return `<div class="terminal-output"><div class="terminal-heading"><span>›_ ${r.running||c.status==='running'?'正在执行':'执行输出'}</span><span>${r.exit_code!=null?'exit '+esc(r.exit_code):'LIVE'}</span></div>${c.arguments?.command?`<pre class="terminal-command">$ ${esc(c.arguments.command)}</pre>`:''}<pre data-scroll="stdout" class="stdout">${esc(r.stdout|| (c.status==='running'?'等待输出…':'（无标准输出）'))}</pre>${r.stderr?`<div class="stream-label">标准错误</div><pre data-scroll="stderr" class="stderr">${esc(r.stderr)}</pre>`:''}</div>${r.truncated?'<p class="result-note">输出已达到 1 MiB 上限，后续内容未保存。</p>':''}`;
 if(c.tool==='read_file'&&typeof r.content==='string')return `<div class="detail-section"><h3>${esc(c.arguments?.path)} <span>${esc(r.encoding||'utf-8')} · ${esc(r.size)} B</span></h3><pre class="file-content" data-scroll="file">${esc(r.content)}</pre>${r.truncated?'<p class="result-note">文件内容已截断。</p>':''}</div>`;
 if(Array.isArray(r.entries))return `<div class="detail-section"><h3>目录内容 <span>${r.entries.length} 项</span></h3><div class="result-table" data-scroll="entries"><table><thead><tr><th>名称</th><th>大小</th></tr></thead><tbody>${r.entries.map(e=>`<tr><td>${e.directory?'▱':'▤'} ${esc(e.name)}${e.symlink?' ↗':''}</td><td>${e.directory?'—':esc(e.size)+' B'}</td></tr>`).join('')}</tbody></table></div></div>`;
 if(Array.isArray(r.matches))return `<div class="detail-section"><h3>搜索结果 <span>${r.matches.length} 处</span></h3><div class="search-results" data-scroll="matches">${r.matches.map(m=>`<div><strong>${esc(m.path)}:${esc(m.line)}</strong><pre>${esc(m.text)}</pre></div>`).join('')||'<p class="muted">没有匹配内容</p>'}</div></div>`;
 if(c.tool==='write_file'&&r.bytes_written!=null)return `<div class="saved-result">✓ 已写入 ${esc(r.path)}<small>${esc(r.bytes_written)} 字节 · 原子写入</small></div>`;
 return '';
}
function renderDetail(c){
 const rich=c?outputView(c):'';
 const html=c?`<div class="call-details"><section class="call-body"><div class="call-body-head"><span class="call-body-label">输入参数</span><span class="grow"></span><span class="note">JSON</span></div><pre data-scroll="args">${pretty(c.arguments)}</pre></section><section class="call-body"><div class="call-body-head"><span class="call-body-label">执行结果</span><span class="grow"></span><button class="button subtle" data-copy-result>复制</button></div><div class="body-content">${c.error?`<div class="detail-error">${esc(callError(c))}</div>`:''}${rich}${c.screenshot?`<div class="detail-section"><h3>屏幕快照 <button data-view-replay="${esc(c.id)}">查看回放 ↗</button></h3><a href="/api/screenshots/${encodeURIComponent(c.screenshot)}" target="_blank" rel="noopener"><img src="/api/screenshots/${encodeURIComponent(c.screenshot)}" alt="${esc(c.tool)} 执行后的屏幕快照"></a></div>`:''}${rich||c.screenshot?`<details class="raw-data" data-preserve="result"><summary>原始响应 <span>JSON</span></summary><pre data-scroll="raw">${pretty(c.result)}</pre></details>`:`<pre data-scroll="raw">${pretty(c.result)}</pre>`}</div></section></div><div class="call-meta-bar"><span>会话 <code>${esc(c.session)}</code></span><span>调用 <code>${esc(c.id)}</code></span><span>${new Date(c.started).toLocaleString('zh-CN',{hour12:false})}</span>${c.status==='running'?`<button class="button danger" data-cancel="${esc(c.id)}">停止这次调用</button>`:''}</div>`:'';
 for(const target of [$('detail')]){
  if(target.dataset.call===c?.id&&target._html===html)continue;
  const same=target.dataset.call===c?.id;
  const opened=same?[...target.querySelectorAll('details[open]')].map(d=>d.dataset.preserve):[];
  const scrolls=same?[...target.querySelectorAll('[data-scroll]')].map(e=>({key:e.dataset.scroll,top:e.scrollTop,bottom:e.scrollHeight-e.scrollTop-e.clientHeight<20})):[];
  target.innerHTML=html;target.dataset.call=c?.id||'';target._html=html;
  target.querySelectorAll('details').forEach(d=>d.open=opened.includes(d.dataset.preserve));
  target.querySelectorAll('[data-scroll]').forEach(e=>{const old=scrolls.find(s=>s.key===e.dataset.scroll);e.scrollTop=old?(old.bottom?e.scrollHeight:old.top):0});
 }
}
function showPage(page){
 state.page=page;stopReplay();document.querySelector('.view').scrollTop=0;
 document.querySelectorAll('.page').forEach(p=>p.classList.toggle('hidden',p.id!==page+'-page'));
 document.querySelectorAll('.nav').forEach(b=>b.classList.toggle('active',b.dataset.page===page));
 if(!state.data)return;
 if(page==='settings')renderSettings();
 if(page==='tools')renderTools();
 if(page==='replay')loadFrames().catch(e=>error(e.message));
}
function renderTools(){renderChrome();$('tools').innerHTML=state.data.tools.map(t=>`<button class="row tool-row" data-tool="${esc(t.name)}"><span class="tool-icon">${icons[t.category]}</span><span class="who"><span class="name">${esc(t.name)}</span><span class="sub">${esc(descriptions[t.name]||t.description)}</span></span><span class="tags"><span class="tag">${t.mutating?'读写':'只读'}</span><span class="tag">${t.parallel?'并发':'串行'}</span><span class="tag">${state.data.enabled[t.category]?'已启用':'未启用'}</span></span><span class="chev">›</span></button>`).join('')}
function renderSettings(){renderChrome();const d=state.data;const names={files:['文件系统','读取、写入和搜索指定工作区内的文件。'],terminal:['终端执行','允许执行宿主机命令。工作目录限制不是系统沙箱；命令拥有当前用户的权限。'],computer:['桌面操作','允许截图、鼠标与键盘操作。此驱动使用真实鼠标，移动到屏幕角落可停止输入。'],browser:['Chrome 浏览器','检测已开启的 Chrome 远程调试，通过官方 MCP 开放浏览器工具。所有调用均记录日志。']};$('capabilities').innerHTML=Object.entries(names).map(([k,[title,description]])=>`<div class="capability"><span class="tool-icon ${k}">${icons[k]}</span><div class="capability-copy"><strong>${title}</strong><p>${description}</p></div><button class="toggle ${d.enabled[k]?'on':''}" role="switch" aria-checked="${d.enabled[k]}" aria-label="${title}" data-capability="${k}"></button></div>`).join('');const p=d.permissions;$('permissions').innerHTML=p.supported?`<div class="permission-row"><span>屏幕录制</span><span class="badge ${p.screen?'success':'denied'}">${p.screen?'已授权':'未授权'}</span></div><div class="permission-row"><span>辅助功能</span><span class="badge ${p.accessibility?'success':'denied'}">${p.accessibility?'已授权':'未授权'}</span></div>`:'<p>此构建不支持原生桌面操作。需 macOS + CGO 构建。</p>';$('gateway-address').textContent=d.gateway;$('workspace-path').textContent=d.workspace;$('connection-config').textContent=JSON.stringify({mcpServers:{relay:{url:d.gateway+'/mcp'}}},null,2);$('tunnel-command').textContent=`cloudflared tunnel --url ${d.gateway_origin}`}
function renderChrome(){
 const c=state.data?.chrome||{state:'waiting',message:'等待检测 Chrome'};
 const names={waiting:'等待 Chrome',connecting:'正在连接',ready:'已接入',disabled:'已关闭',unavailable:'需要配置',error:'连接失败'};
 const status=state.data?.paused?'已暂停':!state.data?.enabled.browser?'已关闭':names[c.state]||c.state;
 for(const el of document.querySelectorAll('[data-chrome-status]'))el.textContent=status;
 for(const el of document.querySelectorAll('[data-chrome-message]'))el.textContent=c.message+(c.tools?` · ${c.tools} 个工具`:'');
 for(const el of document.querySelectorAll('[data-chrome-indicator]'))el.classList.toggle('ready',c.state==='ready'&&!state.data?.paused&&state.data?.enabled.browser);
}
async function loadFrames(force=false){
 const signature=[state.session,state.data?.summary.total,state.data?.summary.running].join(':');
 if(!force&&signature===state.frameSignature)return;
 const version=++state.framesVersion;let frames=[],offset=0;
 while(true){const list=await api('/api/calls?'+new URLSearchParams({category:'computer',session:state.session,limit:'500',offset:String(offset),view:'summary'}));frames.push(...list.calls.filter(c=>c.screenshot||c.tool==='computer_action'));if(list.calls.length<500||offset>=4500)break;offset+=500}
 if(version!==state.framesVersion)return;
 const old=state.replayTarget||state.frames[state.frame]?.id;state.replayTarget=null;state.frameSignature=signature;state.frames=frames.reverse();const found=state.frames.findIndex(f=>f.id===old);state.frame=found>=0?found:Math.max(0,state.frames.length-1);await renderFrame();
}
async function renderFrame(){
 const frames=state.frames,c=frames[state.frame],version=++state.replayVersion;
 $('frame-caption').textContent=c?`${state.frame+1} / ${frames.length} · ${time(c.started)} · ${c.tool}`:'暂无快照';
 $('frame-prev').disabled=state.frame<=0;$('frame-next').disabled=state.frame>=frames.length-1;$('play').disabled=!frames.length;
 $('replay-position').max=Math.max(0,frames.length-1);$('replay-position').value=state.frame;$('replay-position').disabled=!frames.length;
 $('filmstrip').innerHTML=frames.map((c,i)=>`<button class="frame ${i===state.frame?'selected':''}" data-frame="${i}" aria-label="查看 ${time(c.started)} 的记录">${c.screenshot?`<img src="/api/screenshots/${encodeURIComponent(c.screenshot)}" alt="" loading="lazy">`:'<div class="frame-placeholder">⌖</div>'}<span>${time(c.started)} · ${esc(labels[c.status])}</span></button>`).join('');
 if(!c){state.replayDetail=null;$('replay-screen').innerHTML=empty('▣','还没有屏幕快照','在连接与权限中启用桌面操作并授予系统权限，随后点击「拍摄快照」。');$('replay-action').innerHTML='';return}
 $('replay-screen').innerHTML='<div class="empty">正在加载此步骤…</div>';
 try{const detail=await api('/api/calls/'+encodeURIComponent(c.id));if(version!==state.replayVersion)return;state.replayDetail=detail;if(!detail.call.screenshot&&detail.before)state.replaySide='before';else if(!detail.before&&detail.call.screenshot)state.replaySide='after';paintReplay()}
 catch(e){if(version===state.replayVersion){$('replay-screen').textContent=e.message}}
}
function paintReplay(){
 const detail=state.replayDetail;if(!detail)return;
 const c=detail.call,before=detail.before,a=c.arguments||{},side=state.replaySide;
 const frame=side==='before'?before:c;
 const size=frame?.result?.image_size;
 const point=a.coordinate;
 let overlay='';
 if(side==='before'&&size?.length===2&&point?.length===2&&[...size,...point].every(Number.isFinite)){
  const to=a.to,drag=to?.length===2&&to.every(Number.isFinite);
  overlay=`<svg class="action-overlay" viewBox="0 0 ${size[0]} ${size[1]}" aria-label="动作目标坐标"><circle cx="${point[0]}" cy="${point[1]}" r="18"/><circle class="target-dot" cx="${point[0]}" cy="${point[1]}" r="4"/>${drag?`<line x1="${point[0]}" y1="${point[1]}" x2="${to[0]}" y2="${to[1]}"/><circle cx="${to[0]}" cy="${to[1]}" r="12"/>`:''}</svg>`;
 }
 $('replay-screen').innerHTML=frame?.screenshot?`<div class="replay-image"><img src="/api/screenshots/${encodeURIComponent(frame.screenshot)}" alt="${side==='before'?'动作使用的参考截图':'执行后截图'}">${overlay}</div>`:empty('▣','这一步没有保存'+(side==='before'?'参考':'执行后')+'截图',c.error||'该动作可能关闭了执行后截图，或没有引用 frame_id。');
 $('replay-action').innerHTML=`<div class="replay-operation"><div><span class="eyebrow">${c.tool==='computer_action'?'COMPUTER ACTION':'OBSERVATION'}</span><h3>${esc(a.action||'拍摄屏幕')} ${badge(c.status)}</h3><p>${point?`目标 (${esc(point.join(', '))}) · `:''}${esc(c.client)} · ${duration(c.duration_ms)}</p>${a.keys?`<p>组合键 ${esc(a.keys.join(' + '))}</p>`:''}${a.text?`<p class="typed-text">输入 ${esc(a.text)}</p>`:''}${c.error?`<p class="error-text">${esc(c.error)}</p>`:''}</div><div class="segmented"><button data-replay-side="before" class="${side==='before'?'active':''}" ${before?'':'disabled'}>参考画面</button><button data-replay-side="after" class="${side==='after'?'active':''}" ${c.screenshot?'':'disabled'}>执行之后</button></div></div><p class="replay-note">${side==='before'?'标记位置来自调用参数，显示在 Agent 使用的参考截图上。':'展示本次调用保存的画面。'} <button data-open-call="${esc(c.id)}">查看调用详情 ↗</button></p>`;
}
function openTool(name){const t=state.data?.tools.find(t=>t.name===name);if(!t)return;state.tool=t;$('tool-title').textContent=name;$('tool-description').textContent=t.description;$('tool-schema').textContent=JSON.stringify(t.inputSchema,null,2);$('tool-arguments').value=JSON.stringify(examples[name]||{},null,2);$('tool-warning').textContent=state.data.enabled[t.category]?(t.mutating?'运行后会实际修改文件或操作电脑。':'调用和结果将保存到执行日志。'):'此能力尚未启用，请先在连接与权限中开启。';$('run-tool').disabled=!state.data.enabled[t.category]||(state.data.paused&&t.name!=='help');$('tool-result').classList.add('hidden');$('tool-dialog').showModal()}
async function copy(text){try{await navigator.clipboard.writeText(text);toast('已复制')}catch{const el=document.createElement('textarea');el.value=text;document.body.appendChild(el);el.select();const ok=document.execCommand('copy');el.remove();toast(ok?'已复制':'无法访问剪贴板，请手动复制')}}
function theme(){const dark=document.documentElement.dataset.theme!=='dark';document.documentElement.dataset.theme=dark?'dark':'light';localStorage.setItem('relay-theme',dark?'dark':'light')}
document.documentElement.dataset.theme=localStorage.getItem('relay-theme')||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light');
document.addEventListener('click',async e=>{const b=e.target.closest('button');if(!b)return;try{if(b.dataset.page)showPage(b.dataset.page);if('chromeRefresh'in b.dataset){await api('/api/chrome/refresh',{});toast('正在重新检测 Chrome');await refresh()}if('category'in b.dataset){state.category=b.dataset.category;state.offset=0;state.selected=null;document.querySelectorAll('[data-category]').forEach(t=>t.classList.toggle('active',t===b));await refresh()}if(b.dataset.call){state.selected=state.selected===b.dataset.call?null:b.dataset.call;renderCalls();await loadDetail()}if(b.dataset.session){state.session=b.dataset.session;state.offset=0;state.selected=null;showPage('activity');await refresh()}if(b.dataset.tool)openTool(b.dataset.tool);if('testFiles'in b.dataset)openTool('list_directory');if('viewReplay'in b.dataset){state.replayTarget=b.dataset.viewReplay;state.frameSignature='';showPage('replay')}if(b.dataset.replaySide){state.replaySide=b.dataset.replaySide;paintReplay()}if(b.dataset.openCall){state.selected=b.dataset.openCall;showPage('activity');await loadDetail()}if(b.dataset.cancel){b.disabled=true;await api('/api/calls/'+encodeURIComponent(b.dataset.cancel)+'/cancel',{});toast('已请求停止，等待进程退出');await refresh()}if('copyResult'in b.dataset&&state.detail)await copy(JSON.stringify(state.detail.call.result,null,2));if(b.dataset.capability){await api('/api/capability',{category:b.dataset.capability,enabled:!state.data.enabled[b.dataset.capability]});await refresh()}if('frame'in b.dataset){stopReplay();state.frame=Number(b.dataset.frame);await renderFrame()}}catch(e){toast(e.message)}});
$('refresh').onclick=()=>refresh();$('session-filter').onchange=()=>{state.session=$('session-filter').value;state.offset=0;state.selected=null;state.frameSignature='';refresh()};
$('connect').onclick=()=>showPage('settings');$('theme').onclick=theme;$('pause').onclick=async()=>{try{await api('/api/pause',{paused:!state.data.paused});await refresh()}catch(e){error(e.message)}};$('resume').onclick=()=>{$('pause').click()};$('search').oninput=()=>{clearTimeout(searchTimer);searchTimer=setTimeout(()=>{state.query=$('search').value;state.offset=0;state.selected=null;refresh()},250)};$('status-filter').onchange=()=>{state.status=$('status-filter').value;state.offset=0;state.selected=null;refresh()};$('previous').onclick=()=>{state.offset=Math.max(0,state.offset-40);state.selected=null;refresh()};$('next').onclick=()=>{state.offset+=40;state.selected=null;refresh()};$('close-tool').onclick=()=>$('tool-dialog').close();$('tool-dialog').addEventListener('click',e=>{if(e.target===$('tool-dialog')){const rect=e.target.getBoundingClientRect();if(e.clientX<rect.left||e.clientX>rect.right||e.clientY<rect.top||e.clientY>rect.bottom)e.target.close()}});
$('run-tool').onclick=async()=>{const b=$('run-tool');b.disabled=true;b.textContent='执行中…';try{const args=JSON.parse($('tool-arguments').value);const result=await api('/api/tools/'+state.tool.name,args);if(result.result?.screenshot)result.result.screenshot='[快照已保存，可在桌面回放中查看]';$('tool-result').textContent=JSON.stringify(result,null,2);$('tool-result').classList.remove('hidden');state.selected=result.call_id;await refresh()}catch(e){$('tool-result').textContent=JSON.stringify(e.data||{error:e.message},null,2);$('tool-result').classList.remove('hidden');await refresh()}finally{b.disabled=(state.data.paused&&state.tool.name!=='help')||!state.data.enabled[state.tool.category];b.textContent='运行工具 →'}};
$('capture').onclick=async()=>{const b=$('capture');b.disabled=true;try{await api('/api/tools/computer_screenshot',{});await refresh();toast('快照已保存')}catch(e){toast(e.message)}finally{b.disabled=false}};function stopReplay(){clearTimeout(replayTimer);replayTimer=null;$('play').textContent='▶ 播放'}
$('frame-prev').onclick=()=>{stopReplay();state.frame--;void renderFrame()};
$('frame-next').onclick=()=>{stopReplay();state.frame++;void renderFrame()};
$('replay-position').oninput=()=>{stopReplay();state.frame=Number($('replay-position').value);void renderFrame()};
$('play').onclick=async()=>{
 if(replayTimer){stopReplay();return}
 if(state.frame>=state.frames.length-1)state.frame=0;
 $('play').textContent='Ⅱ 暂停';
 const tick=async()=>{await renderFrame();if(!replayTimer)return;if(state.frame>=state.frames.length-1){stopReplay();return}replayTimer=setTimeout(()=>{state.frame++;void tick()},1400/Number($('replay-speed').value))};
 replayTimer=-1;await tick();
};
$('copy-address').onclick=async()=>{try{const d=await api('/api/connection');await copy(d.gateway)}catch(e){toast(e.message)}};$('copy-config').onclick=async()=>{try{const d=await api('/api/connection');await copy(JSON.stringify({mcpServers:{relay:{url:d.gateway+'/mcp'}}},null,2))}catch(e){toast(e.message)}};$('copy-tunnel').onclick=()=>copy($('tunnel-command').textContent);
$('export').onclick=async()=>{try{const res=await fetch('/api/export?'+params());if(!res.ok)throw new Error('导出失败');const url=URL.createObjectURL(await res.blob());const a=document.createElement('a');a.href=url;a.download='relay-calls.ndjson';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);toast('日志已导出')}catch(e){toast(e.message)}};
document.addEventListener('keydown',e=>{if((e.metaKey||e.ctrlKey)&&e.key.toLowerCase()==='k'){e.preventDefault();showPage('activity');$('search').focus()}if((e.metaKey||e.ctrlKey)&&e.key.toLowerCase()==='j'){e.preventDefault();theme()}});
async function connect(){try{const key=new URLSearchParams(location.hash.slice(1)).get('key');if(key){await api('/api/login',{key});history.replaceState(null,'',location.pathname)}await refresh();if(state.connected&&!eventStream){eventStream=new EventSource('/api/events');eventStream.onmessage=()=>refresh();eventStream.onerror=()=>{$('live').classList.add('off')}}}catch(e){error(e.message)}}
window.addEventListener('hashchange',connect);
setInterval(()=>{if(!document.hidden)refresh()},5000);
void connect();

function renderUpdate(u){
 if(!u)return;
 const names={idle:'等待检查',checking:'正在检查更新…',latest:'已是最新版本',downloading:'正在下载更新…',ready:'更新已准备好',available:'有新版本可用',source:'开发构建',disabled:'自动更新未启用',error:'更新失败',restarting:'正在重启…'};
 $('update-version').textContent=u.current;
 $('update-status').textContent=names[u.state]||u.state;
 $('update-message').textContent=u.reason||(u.latest?`新版本 ${u.latest} · 当前 ${u.current}`:'启动后检查，此后每 6 小时检查一次。');
 $('update-check').disabled=!u.can_check||['checking','downloading','restarting'].includes(u.state);
 $('update-restart').classList.toggle('hidden',!u.can_restart);
 $('update-banner').classList.toggle('hidden',!u.can_restart&&u.state!=='restarting');
 $('update-banner-text').textContent=u.state==='restarting'?'正在重启 Relay…':`Relay ${u.latest} 已下载，下次退出时安装。`;
 $('update-banner-restart').disabled=!u.can_restart;
 $('update-progress').classList.toggle('hidden',u.state!=='downloading');
 if(u.total>0){$('update-progress').max=u.total;$('update-progress').value=u.done}else{$('update-progress').removeAttribute('value')}
 $('update-error').classList.toggle('hidden',!u.error);$('update-error').textContent=u.error||'';
 $('update-notes-box').classList.toggle('hidden',!u.notes);$('update-notes').textContent=u.notes||'';
 let releaseURL='';try{const url=new URL(u.url);if(['https:','http:'].includes(url.protocol))releaseURL=url.href}catch{}
 $('update-release').classList.toggle('hidden',!releaseURL);if(releaseURL)$('update-release').href=releaseURL;else $('update-release').removeAttribute('href');
}
$('update-check').onclick=async()=>{try{renderUpdate(await api('/api/update/check',{}))}catch(e){toast(e.message)}};
async function restartToUpdate(){
 try{await api('/api/update/restart',{});toast('正在重启；浏览器控制台请使用终端输出的新链接。');renderUpdate({...state.data.update,state:'restarting',can_restart:false})}catch(e){toast(e.message)}
}
$('update-restart').onclick=restartToUpdate;$('update-banner-restart').onclick=restartToUpdate;
