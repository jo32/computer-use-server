const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname,'../internal/server/assets/app.js'),'utf8');
function harness(extra={}) {
 const elements = new Map();
 const $ = id => {if(!elements.has(id))elements.set(id,{hidden:false,showModal(){},close(){},addEventListener(){},removeAttribute(){}});return elements.get(id)};
 const context={$,PUBLIC_VIEW:false,location:{search:''},URLSearchParams,AbortController,DOMException,clearTimeout,setTimeout,t:(s,v)=>v?s.replace(/\{(\d+)\}/g,(_,k)=>v[k]):s,route:s=>s,params:()=>new URLSearchParams(),toast(){},window:{},...extra};
 vm.createContext(context);
 vm.runInContext(source.slice(source.indexOf('const nativeExport='),source.indexOf("if(nativeExport)exportRequest()")),context);
 return {context,$,run:s=>vm.runInContext(s,context)};
}
test('browser export writes incrementally, pauses, resumes and only then commits',async()=>{
 const writes=[];let commits=0,reads=0,release;
 const h=harness({window:{showSaveFilePicker:async()=>({name:'calls.ndjson',createWritable:async()=>({async write(b){writes.push(b);if(writes.length===1)await new Promise(r=>release=r)},async close(){commits++},async abort(){throw Error('unexpected abort')}})})},fetch:async()=>({ok:true,headers:{get:()=> '2'},body:{getReader:()=>({async read(){reads++;return reads<=2?{done:false,value:new Uint8Array([123,125,10])}:{done:true}},async cancel(){}})}})});
 const finished=h.$('export').onclick();
 while(!release)await new Promise(r=>setImmediate(r));
 await h.$('export-toggle').onclick();release();
 await new Promise(r=>setImmediate(r));
 assert.equal(reads,1);assert.equal(commits,0);assert.equal(h.run('exportState'),'paused');
 await h.$('export-toggle').onclick();await finished;
 assert.equal(writes.length,2);assert.equal(commits,1);assert.equal(h.run('exportState'),'done');
 assert.equal(h.$('export-progress').value,2);
});
test('cancelling a paused export aborts the file without committing',async()=>{
 let release,aborts=0,commits=0;
 const h=harness({window:{showSaveFilePicker:async()=>({name:'calls.ndjson',createWritable:async()=>({async write(){await new Promise(r=>release=r)},async close(){commits++},async abort(){aborts++}})})},fetch:async()=>({ok:true,headers:{get:()=> '2'},body:{getReader:()=>({async read(){return {done:false,value:new Uint8Array([10])}},async cancel(){}})}})});
 const finished=h.$('export').onclick();while(!release)await new Promise(r=>setImmediate(r));
 await h.$('export-toggle').onclick();release();await new Promise(r=>setImmediate(r));
 await h.$('export-cancel').onclick();await finished;
 assert.equal(aborts,1);assert.equal(commits,0);assert.equal(h.run('exportState'),'cancelled');
});
test('save picker cancellation is not reported as success',async()=>{
 const h=harness({window:{showSaveFilePicker:async()=>{throw new DOMException('Cancelled','AbortError')}}});
 await h.$('export').onclick();assert.equal(h.run('exportState'),'cancelled');assert.equal(h.$('export').disabled,false);
});
test('truncated stream aborts the output instead of reporting success',async()=>{
 let aborted=false;
 const h=harness({window:{showSaveFilePicker:async()=>({name:'calls.ndjson',createWritable:async()=>({async close(){throw Error('must not commit')},async abort(){aborted=true}})})},fetch:async()=>({ok:true,headers:{get:()=> '2'},body:{getReader:()=>({async read(){return {done:true}},async cancel(){}})}})});
 await h.$('export').onclick();assert.equal(aborted,true);assert.equal(h.run('exportState'),'error');
});
test('cancel during an in-flight write also releases a pending pause',async()=>{
 let release,aborted=false;
 const h=harness({window:{showSaveFilePicker:async()=>({name:'calls.ndjson',createWritable:async()=>({async write(){await new Promise(r=>release=r)},async close(){throw Error('must not commit')},async abort(){aborted=true}})})},fetch:async()=>({ok:true,headers:{get:()=> '2'},body:{getReader:()=>({async read(){return {done:false,value:new Uint8Array([10])}},async cancel(){}})}})});
 const finished=h.$('export').onclick();while(!release)await new Promise(r=>setImmediate(r));
 await h.$('export-toggle').onclick();await h.$('export-cancel').onclick();release();await finished;
 assert.equal(aborted,true);assert.equal(h.run('exportState'),'cancelled');
});
