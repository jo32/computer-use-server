const assert = require('node:assert/strict');
const { test } = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const assets = path.join(root, 'internal/server/assets');
const englishScript = fs.readFileSync(path.join(assets, 'locales/en.js'), 'utf8');
const catalog = vm.runInNewContext(englishScript + '\nreadyRigEnglish;');

function harness({ saved = null, languages = ['en-US'], blockedStorage = false, search = '' } = {}) {
  const events = new Map();
  const node = { data:'活动日志', isConnected:true, parentElement:{closest:()=>null} };
  const select = {value:'auto'};
  let walked = false;
  const context = {
    URL, URLSearchParams, navigator:{languages}, location:{search,href:'http://localhost:7331/'+search+'#key=test'}, NodeFilter:{SHOW_TEXT:4},
    history:{replaceState:(_state,_title,url)=>{context.location.href=String(url);context.location.search=new URL(url).search}},
    CustomEvent:class { constructor(type, options) { this.type=type; this.detail=options.detail; } },
    localStorage:{getItem:()=>{if(blockedStorage)throw Error('blocked');return saved}, setItem:(key,value)=>{if(blockedStorage)throw Error('blocked');saved=value}},
    document:{body:{},documentElement:{},createTreeWalker:()=>({nextNode:()=>{if(walked)return false;walked=true;return true},get currentNode(){return node}}),querySelectorAll:()=>[],getElementById:()=>select},
    window:{addEventListener:(name,fn)=>events.set(name,fn),dispatchEvent:event=>events.get(event.type)?.(event)},
  };
  vm.createContext(context);
  vm.runInContext(englishScript + '\n' + fs.readFileSync(path.join(assets,'i18n.js'),'utf8')+'\nglobalThis.language=readyRigI18n;',context);
  return {context,node,select,get saved(){return saved}};
}

test('app switches both ways, persists selection, and leaves user content untouched', async()=>{
  const h=harness();
  assert.equal(h.node.data,'Activity');
  await h.context.language.setPreference('zh-CN');
  assert.equal(h.node.data,'活动日志');
  assert.equal(h.saved,'zh-CN');
  h.node.data='用户自己的活动日志';
  await h.context.language.setPreference('en');
  assert.equal(h.node.data,'用户自己的活动日志');
  assert.equal(harness({saved:h.saved}).context.document.documentElement.lang,'en');
  assert.equal(h.context.language.t('unknown'), 'unknown');
  assert.equal(h.context.language.t('输入 {0}',{0:'$& {0}'}), 'Input $& {0}');
  assert.equal(h.context.language.t('在 Chrome 144+ 中打开 '),'In Chrome 144+, open ');
});

test('a manual app selection replaces the initial URL language override on reload',async()=>{
  const h=harness({saved:'zh-CN',search:'?lang=en&view=tools'});
  assert.equal(h.context.document.documentElement.lang,'en');
  await h.context.language.setPreference('zh-CN');
  assert.equal(h.context.location.href,'http://localhost:7331/?view=tools#key=test');
  assert.equal(harness({saved:h.saved,search:h.context.location.search}).context.document.documentElement.lang,'zh-CN');
});

test('app language selection works with blocked storage and unsupported languages',async()=>{
  const h=harness({blockedStorage:true,languages:['fr-FR','zh-HK']});
  assert.equal(h.context.document.documentElement.lang,'zh-CN');
  await h.context.language.setPreference('en');
  assert.equal(h.context.document.documentElement.lang,'en');
  await h.context.language.setPreference('invalid');
  assert.equal(h.context.document.documentElement.lang,'en');
  assert.equal(harness({languages:['ja-JP']}).context.document.documentElement.lang,'en');
});

test('known diagnostic prefixes retain their technical cause',()=>{
  const {language}=harness().context;
  assert.equal(language.t('无法读取 Chrome 调试入口：permission denied'),'Cannot read the Chrome debugging file: permission denied');
});

test('every interface string has English copy, with matching numbered parameters',()=>{
  const siteCatalog=JSON.parse(fs.readFileSync(path.join(root,'website/src/locales/en.json'),'utf8'));
  const files=['app.js','window.js','cloud.js'].map(name=>[path.join(assets,name),catalog]);
  for(const name of ['App.tsx','i18n.tsx','CloudConsole.tsx','CloudPromptButton.tsx','cloud-api.ts',...fs.readdirSync(path.join(root,'website/src/components')).filter(name=>name.endsWith('.tsx')).map(name=>'components/'+name)]) files.push([path.join(root,'website/src',name),siteCatalog]);
  const chinese=/[\u3400-\u9fff]/;
  for(const [file,dictionary] of files){
    const source=fs.readFileSync(file,'utf8');
    for(const match of source.matchAll(/(?:"(?:[^"\\\r\n]|\\.)*"|'(?:[^'\\\r\n]|\\.)*')/g)){
      if(!chinese.test(match[0]))continue;
      const message=vm.runInNewContext(match[0]);
      assert.ok(Object.hasOwn(dictionary,message),`${path.relative(root,file)}: missing ${message}`);
    }
  }
  const html=fs.readFileSync(path.join(assets,'index.html'),'utf8');
  for(const match of html.matchAll(/>([^<>]*[\u3400-\u9fff][^<>]*)<|(?:aria-label|title|placeholder)="([^"<>]*[\u3400-\u9fff][^"<>]*)"/g)) assert.ok(Object.hasOwn(catalog,(match[1]||match[2]).trim()),`Static app copy: ${match[1]||match[2]}`);
  for(const dictionary of [catalog,siteCatalog])for(const [key,value] of Object.entries(dictionary)){
    const parameters=text=>[...text.matchAll(/\{(\d+)\}/g)].map(match=>match[1]).sort();
    assert.deepEqual(parameters(value),parameters(key),`Parameter mismatch in ${key}`);
  }
  const native=JSON.parse(fs.readFileSync(path.join(root,'internal/i18n/en.json'),'utf8'));
  for(const [key,value] of Object.entries(native))assert.equal(value,catalog[key],`Native and app translation differ: ${key}`);
});

test('localized connection prompts keep real URLs and tool routes',async()=>{
  const {language}=harness().context;
  const source=fs.readFileSync(path.join(assets,'app.js'),'utf8');
  const start=source.indexOf('function connectionPrompt(');
  const end=source.indexOf('\nfunction renderConnection()',start);
  const context={t:language.t,PUBLIC_VIEW:false};vm.createContext(context);vm.runInContext(source.slice(start,end),context);
  const url='http://127.0.0.1:7332/AbC123xy';
  const prompt=context.connectionPrompt(url,'local');
  for(const route of ['/api/v1/tools/help','/api/v1/tools/list_projects','/mcp'])assert.ok(prompt.includes(url+route));
  assert.ok(prompt.includes('Please connect to ReadyRig'));
  assert.ok(prompt.includes('"Public"')||prompt.includes('“Public”'));
  await language.setPreference('zh-CN');
  assert.ok(context.connectionPrompt(url,'local').includes('请连接我电脑上的 ReadyRig'));
});

function localPromptContext(overrides={}) {
  const {language}=harness().context;
  const source=fs.readFileSync(path.join(assets,'app.js'),'utf8');
  const start=source.indexOf('function shellArgument(');
  const end=source.indexOf('\nfunction renderSettings()',start);
  const context={t:language.t,PUBLIC_VIEW:false,...overrides};
  vm.createContext(context);vm.runInContext(source.slice(start,end),context);
  return {context,language};
}

test('local setup prompts target the current instance and describe its lifecycle in both languages',async()=>{
  const {context,language}=localPromptContext();
  const instance={command:"/Applications/ReadyRig O'Reilly.app/Contents/Helpers/readyrig",data_dir:"/Users/me/private data'$(touch unintended)`echo x`",mode:'desktop'};
  const prefix=context.shellArgument(instance.command)+' --data-dir '+context.shellArgument(instance.data_dir);
  const prompt=context.localConfigurationPrompt(instance);
  for(const command of ['version','help','status','config show','projects list','tools'])assert.ok(prompt.includes(prefix+' '+command));
  assert.ok(prompt.includes('The ReadyRig app owns this instance'));
  assert.ok(prompt.includes('The app reads the saved startup settings'));
  assert.ok(prompt.includes('--token-stdin'));
  assert.ok(context.localConfigurationPrompt({...instance,mode:'daemon'}).includes('background daemon'));
  assert.ok(context.localConfigurationPrompt({...instance,mode:'foreground'}).includes('original process management method'));
  await language.setPreference('zh-CN');
  assert.ok(context.localConfigurationPrompt(instance).includes('请帮我配置这台电脑上的 ReadyRig'));
  assert.ok(context.localConfigurationPrompt(instance).includes('App 会读取保存的启动设置'));
  assert.equal(context.localConfigurationPrompt({command:instance.command}), '');
  context.PUBLIC_VIEW=true;
  assert.equal(context.localConfigurationPrompt(instance), '');
});

test('local prompt paths remain literal POSIX shell arguments',()=>{
  const {context}=localPromptContext();
  for(const value of ["/Users/项目 O'Reilly/$(printf unintended)`printf unintended`",'/data/line\nbreak\tand space','/a/\\backslash']){
    const result=spawnSync('/bin/sh',['-c',"printf '%s' "+context.shellArgument(value)],{encoding:'utf8'});
    assert.equal(result.status,0,result.stderr);
    assert.equal(result.stdout,value);
  }
});

test('copy setup prompt refreshes instance details and exposes a manual copy fallback',async()=>{
  const attributes=new Map(),button={disabled:false,setAttribute:(key,value)=>attributes.set(key,value),removeAttribute:key=>attributes.delete(key)};
  const textarea={value:'',focus(){this.focused=true},select(){this.selected=true}},details={open:false};
  const nodes={'copy-local-config-prompt':button,'local-config-details':details,'local-config-prompt-text':textarea};
  const fresh={local_cli:{command:'/matching/readyrig',data_dir:'/actual/custom/data',mode:'desktop'},gateway:'private-agent-credential',cloud:{token:'private-cloud-token'}};
  let requests=0,copied='',failure='';
  const {context}=localPromptContext({
    $:id=>nodes[id],state:{data:{local_cli:{command:'/old/readyrig',data_dir:'/old/data'}}},
    api:async path=>{assert.equal(path,'/api/state');requests++;return fresh},
    renderCLI:()=>{button.disabled=attributes.has('aria-busy')},
    copy:async text=>{copied=text;return false},toast:text=>{failure=text}
  });
  await context.copyLocalConfiguration();
  assert.equal(requests,1);assert.equal(failure,'');assert.equal(button.disabled,false);
  assert.ok(copied.includes("'/matching/readyrig' --data-dir '/actual/custom/data'"));
  for(const secret of ['/old/readyrig','private-agent-credential','private-cloud-token'])assert.ok(!copied.includes(secret));
  assert.equal(details.open,true);assert.equal(textarea.focused,true);assert.equal(textarea.selected,true);
  context.api=async()=>({local_cli:null});copied='';
  await context.copyLocalConfiguration();
  assert.equal(copied,'');assert.ok(failure.includes('unavailable'));assert.equal(button.disabled,false);
  context.PUBLIC_VIEW=true;context.api=async()=>{throw Error('public view must not fetch local configuration')};
  await context.copyLocalConfiguration();
});

test('a denied clipboard and unsupported legacy copy still allow manual copying',async()=>{
  const source=fs.readFileSync(path.join(assets,'app.js'),'utf8');
  const start=source.indexOf('async function copy(text)');
  const end=source.indexOf('\nfunction theme()',start);
  const textarea={value:'',select(){},remove(){this.removed=true}};
  let message='';
  const context={
    t:value=>value,toast:value=>{message=value},
    navigator:{clipboard:{writeText:async()=>{throw Error('denied')}}},
    document:{createElement:()=>textarea,body:{appendChild(){}},execCommand:()=>{throw Error('unsupported')}}
  };
  vm.createContext(context);vm.runInContext(source.slice(start,end),context);
  assert.equal(await context.copy('setup prompt'),false);
  assert.equal(textarea.value,'setup prompt');assert.equal(textarea.removed,true);
  assert.ok(message.includes('手动复制'));
});
