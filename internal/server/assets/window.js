'use strict';

const relayWindow = (() => {
  const shell = new URLSearchParams(location.search).get('shell');
  if (shell === 'darwin') document.body.classList.add('native-mac');
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

  return {
    setActivity({ connected, paused = false, running = 0 }) {
      const activity = !connected ? 'offline' : paused ? 'paused' : running > 0 ? 'working' : 'ready';
      const label = { offline: '连接已断开', paused: '控制已暂停', working: '正在运行', ready: '已就绪' }[activity];
      brand.dataset.activity = activity;
      brand.setAttribute('aria-label', `Relay · ${label}`);
      brand.title = `Relay · ${label}`;
      if (activity === 'offline' || activity === 'paused') stopWake();
    },
  };
})();
