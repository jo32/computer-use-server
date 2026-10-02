const assert = require('node:assert/strict');
const { test } = require('node:test');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../internal/server/assets/app.js'), 'utf8');
function harness(extra = {}) {
  const context = {
    t: s => s,
    esc: v => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),
    ...extra,
  };
  vm.createContext(context);
  vm.runInContext(source.slice(source.indexOf('function previewValue('), source.indexOf('function renderDetail(')) + '\nglobalThis.pretty = pretty;', context);
  return context;
}

test('expanding a large browser screenshot does not insert or serialize its image payload', () => {
  const h = harness();
  const part = {type:'image', mimeType:'image/png', data:'A'.repeat(32 * 1024 * 1024)};
  const result = {content:[{type:'text', text:'Screenshot captured'}, part]};
  const html = h.outputView({category:'browser', result}) + h.pretty(result);
  assert.ok(html.length < 2000);
  assert.ok(html.includes('data-browser-image="1"'));
  assert.ok(!html.includes('<img'));
  assert.ok(!html.includes('AAAA'));
  assert.equal(result.content[1].data.length, 32 * 1024 * 1024, 'copy retains the original payload');
});

test('large text and nested responses produce bounded, escaped previews', () => {
  const h = harness();
  const huge = '<script>'.repeat(1024 * 1024);
  const json = h.pretty({items:Array(10000).fill(huge)});
  assert.ok(json.length < 100000);
  assert.ok(!json.includes('<script>'));
  const text = h.outputView({category:'browser',result:{content:[{type:'text',text:huge}]}});
  assert.ok(text.length < 12000);
  let nested = {};
  for (let i = 0; i < 100; i++) nested = {nested};
  assert.ok(h.pretty(nested).length < 3000);
  assert.equal(h.pretty({ok:true,count:2}), '{\n  &quot;ok&quot;: true,\n  &quot;count&quot;: 2\n}');
});

test('image decoding is deferred until requested and object URLs are released', () => {
  const released = [], images = [];
  const h = harness({
    state:{detail:{call:{result:{content:[{type:'image',mimeType:'image/png',data:'aGVsbG8='}]}}}},
    atob: data => Buffer.from(data, 'base64').toString('binary'), Blob,
    URL:{createObjectURL: blob => {assert.equal(blob.size, 5);return 'blob:test'},revokeObjectURL:url=>released.push(url)},
    document:{createElement:()=>({})},
  });
  h.showBrowserImage({dataset:{browserImage:'0'},replaceWith:img=>images.push(img)});
  assert.equal(images[0].src, 'blob:test');
  assert.equal(images[0].decoding, 'async');
  h.clearDetailImages();h.clearDetailImages();
  assert.deepEqual(released, ['blob:test']);
});

test('completed call details are reused while running calls keep refreshing', async () => {
  let requests = 0, renders = 0;
  const h = harness({
    state:{selected:'one',detailVersion:0,detail:{call:{id:'one',status:'success'}}},
    api:async()=>{requests++;return {call:{id:'one',status:'running'}}},
    renderCalls:()=>{},renderDetail:()=>renders++,toast:()=>{},
  });
  vm.runInContext(source.slice(source.indexOf('async function loadDetail('), source.indexOf('// Bound previews')), h);
  await h.loadDetail();await h.loadDetail();
  assert.equal(requests, 0);assert.equal(renders, 2);
  h.state.detail.call.status='running';
  await h.loadDetail();
  assert.equal(requests, 1);
});
