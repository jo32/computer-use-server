'use strict';

const readyRigI18n = (() => {
  const storageKey = 'readyrig-language';
  const valid = value => ['auto', 'zh-CN', 'en'].includes(value);
  let requested = new URLSearchParams(location.search).get('lang');
  let preference = 'auto';
  try { const saved = localStorage.getItem(storageKey); if (valid(saved)) preference = saved; } catch {}
  if (valid(requested)) preference = requested;
  const resolve = () => {
    if (preference !== 'auto') return preference;
    for (const language of navigator.languages || [navigator.language]) {
      if (/^zh(?:-|$)/i.test(language)) return 'zh-CN';
      if (/^en(?:-|$)/i.test(language)) return 'en';
    }
    return 'en';
  };
  let locale = resolve();
  const diagnosticPrefixes = Object.entries(readyRigEnglish).filter(([key]) => /%[wvsd]/.test(key)).map(([key, value]) => [key.split(/%[wvsd]/)[0], value.split(/%[wvsd]/)[0]]).sort(([a], [b]) => b.length - a.length);
  function t(message, values) {
    const catalog = locale === 'en' ? readyRigEnglish : {};
    let text = Object.hasOwn(catalog, message) ? catalog[message] : message;
    if (typeof message === 'string' && text === message && locale === 'en') {
      const trimmed = message.trim();
      if (Object.hasOwn(catalog, trimmed)) text = message.slice(0, message.indexOf(trimmed)) + catalog[trimmed] + message.slice(message.indexOf(trimmed) + trimmed.length);
      else {
        const prefix = diagnosticPrefixes.find(([source]) => message.startsWith(source));
        if (prefix) text = prefix[1] + t(message.slice(prefix[0].length));
      }
    }
    return String(text ?? '').replace(/\{(\d+)\}/g, (token, key) => values && Object.hasOwn(values, key) ? String(values[key]) : token);
  }
  function translateData(value) {
    if (typeof value === 'string') return t(value);
    if (Array.isArray(value)) return value.map(translateData);
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, translateData(item)]));
    return value;
  }

  // Record only original interface copy. Runtime data, logs, paths, and user input
  // are rendered explicitly by the app and are never scanned for translation.
  const bindings = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) {
    const node = walker.currentNode;
    if (/[\u3400-\u9fff]/.test(node.data) && !node.parentElement.closest('script,style')) bindings.push({ node, source: node.data, last: node.data });
  }
  for (const node of document.querySelectorAll('[title],[aria-label],[placeholder]')) {
    for (const attribute of ['title', 'aria-label', 'placeholder']) {
      const source = node.getAttribute(attribute);
      if (source && /[\u3400-\u9fff]/.test(source)) bindings.push({ node, attribute, source, last: source });
    }
  }
  function apply() {
    locale = resolve();
    document.documentElement.lang = locale;
    for (const binding of bindings) {
      const { node, source, attribute, last } = binding;
      if (!node.isConnected || (attribute ? node.getAttribute(attribute) : node.data) !== last) continue;
      const next = t(source);
      if (attribute) node.setAttribute(attribute, next); else node.data = next;
      binding.last = next;
    }
    document.getElementById('language').value = preference;
    window.dispatchEvent(new CustomEvent('readyrig-language-change', { detail: { locale, preference } }));
  }
  const native = !!new URLSearchParams(location.search).get('shell');
  async function setPreference(value, { persist = true, syncNative = true, keepQuery = false } = {}) {
    if (!valid(value)) return;
    // A manual choice replaces the initial URL override, including on reload.
    if (!keepQuery && requested !== null) {
      requested = null;
      const url = new URL(location.href);
      url.searchParams.delete('lang');
      history.replaceState(null, '', url);
    }
    const previous = preference;
    preference = value;
    if (persist) { try { localStorage.setItem(storageKey, value); } catch {} }
    if (previous !== preference || locale !== resolve()) apply();
    if (native && syncNative) {
      try {
        const response = await fetch('/api/window/language', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ preference, locale }) });
        if (!response.ok) throw new Error(t('无法保存语言偏好'));
      } catch (error) { window.dispatchEvent(new CustomEvent('readyrig-language-error', { detail: error.message })); }
    }
  }
  document.getElementById('language').onchange = event => { void setPreference(event.target.value); };
  window.addEventListener('storage', event => {
    if (event.key === storageKey || event.key === null) void setPreference(valid(event.newValue) ? event.newValue : 'auto', { persist: false });
  });
  window.addEventListener('languagechange', () => { if (preference === 'auto') void setPreference('auto'); });
  apply();
  async function sync() {
    if (!native) return;
    try {
      const response = await fetch('/api/window/language');
      if (!response.ok) return;
      const saved = await response.json();
      const value = valid(requested) ? requested : saved.preference;
      await setPreference(valid(value) ? value : preference, { keepQuery: true });
    } catch {}
  }
  // Both native windows read the same saved preference when they regain focus.
  window.addEventListener('focus', () => { void sync(); });
  void sync();
  return { t, translateData, setPreference, get locale() { return locale; }, get preference() { return preference; } };
})();
