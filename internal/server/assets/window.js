'use strict';

const readyRigWindow = (() => {
  const shell = new URLSearchParams(location.search).get('shell');
  const panel = new URLSearchParams(location.search).get('panel') === '1';
  document.body.classList.toggle('native-panel',panel);
  if (shell && panel) {
    const openMain=document.getElementById('open-main');
    openMain.classList.remove('hidden');
    openMain.onclick=()=>fetch('/api/window/open',{method:'POST'}).catch(console.error);
  }
  if (shell === 'darwin' && !panel) document.body.classList.add('native-mac');
  if (shell) {
    // Wails installs dragging from --wails-draggable when its runtime loads.
    // Browser mode keeps its normal header and never requests this module.
    import('/wails/runtime.js').catch(error => console.warn('Window controls unavailable:', error));
  }

  const brand = document.getElementById('brand');
  const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
  let wakeTimer;

  function stopWake() {
    clearTimeout(wakeTimer);
    brand.classList.remove('is-waking');
  }

  function wake() {
    if (document.hidden || reducedMotion.matches || brand.classList.contains('is-waking')) return;
    if (['offline', 'paused'].includes(brand.dataset.activity)) return;
    brand.classList.add('is-waking');
    // Also clear when an animation is cancelled by a theme/accessibility change.
    wakeTimer = setTimeout(stopWake, 950);
  }

  brand.addEventListener('mouseenter', wake);
  window.addEventListener('focus', wake);
  reducedMotion.addEventListener('change', () => { if (reducedMotion.matches) stopWake(); });
  document.addEventListener('visibilitychange', () => {
    brand.classList.toggle('motion-paused', document.hidden);
    if (document.hidden) stopWake();
    else wake();
  });
  setTimeout(wake, 250);

  let lastStatus = { connected: false };
  window.addEventListener('readyrig-language-change', () => readyRigWindow.setActivity(lastStatus));
  return {
    setActivity({ connected, paused = false, running = 0 }) {
      lastStatus = { connected, paused, running };
      const activity = !connected ? 'offline' : paused ? 'paused' : running > 0 ? 'working' : 'ready';
      const label = { offline: readyRigI18n.t("连接已断开"), paused: readyRigI18n.t("控制已暂停"), working: readyRigI18n.t("正在运行"), ready: readyRigI18n.t("已就绪") }[activity];
      brand.dataset.activity = activity;
      brand.setAttribute('aria-label', `ReadyRig · ${readyRigI18n.t(label)}`);
      brand.title = `ReadyRig · ${readyRigI18n.t(label)}`;
      if (activity === 'offline' || activity === 'paused') stopWake();
    },
  };
})();
