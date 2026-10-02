// Clock and sprites for the intro film. The markup lives in stage.html; every animation there is a CSS animation
// that this module pauses and scrubs to the film clock, so play, pause and seek stay frame-accurate.

const TOTAL = 99
const FADE = 0.45
const INK = '#111'
const WHITE = '#fff'
const LED = '#f5a623'
const GRAY = '#8c8c8c'

// One character per pixel: # ink, . paper, o amber, g gray, space transparent.
function px(rows: string[], extra = ''): string {
  const height = rows.length
  const width = Math.max(...rows.map((row) => row.length))
  let out = `<svg viewBox="0 0 ${width} ${height}" shape-rendering="crispEdges" class="sprite">`
  rows.forEach((row, y) => {
    let x = 0
    while (x < row.length) {
      const c = row[x]
      let end = x
      while (end < row.length && row[end] === c) end++
      const fill = c === '#' ? INK : c === '.' ? WHITE : c === 'o' ? LED : c === 'g' ? GRAY : null
      if (fill) out += `<rect x="${x}" y="${y}" width="${end - x}" height="1" fill="${fill}"/>`
      x = end
    }
  })
  return `${out}${extra}</svg>`
}

const MAC = ['################', '#..............#', '#.############.#', '#.#..........#.#', '#.#..#....#..#.#', '#.#..#....#..#.#', '#.#..........#.#', '#.#..#....#..#.#', '#.#...####...#.#', '#.#..........#.#', '#.############.#', '#..............#', '#.######...o...#', '#..............#', '################', '  ############  ', '  ############  ']
const MAC_ASLEEP = MAC.slice()
MAC_ASLEEP[4] = '#.#..........#.#'
MAC_ASLEEP[5] = '#.#.##....##.#.#'
MAC_ASLEEP[7] = '#.#..........#.#'
const BUBBLE = ['  ##############  ', ' #..............# ', '#................#', '#................#', '#................#', '#................#', '#................#', ' #..............# ', '  #....#########  ', '   #...#          ', '   #..#           ', '   #.#            ', '   ##             ']
const BUBBLE_DOTS = '<g class="dots"><rect x="4" y="3" width="2" height="2" fill="#111"/><rect x="8" y="3" width="2" height="2" fill="#111"/><rect x="12" y="3" width="2" height="2" fill="#111"/></g>'
const SPARK = ['  #  ', ' ### ', '#####', ' ### ', '  #  ']
const ARROW = ['#           ', '##          ', '#.#         ', '#..#        ', '#...#       ', '#....#      ', '#.....#     ', '#......#    ', '#.......#   ', '#........#  ', '#.....#####', '#..#..#     ', '#.# #..#    ', '##  #..#    ', '#    #..#   ', '     #..#   ', '      ##    ']
const DOC = ['#######   ', '#.....##  ', '#.....#.# ', '#.....####', '#........#', '#.####...#', '#........#', '#.######.#', '#........#', '#.####...#', '#........#', '##########']
const FOLDER = ['######        ', '#....#########', '#............#', '#............#', '#............#', '#............#', '#............#', '#............#', '#............#', '##############']

function drawSprites(root: HTMLElement) {
  root.querySelectorAll<HTMLElement>('[data-sp]').forEach((node) => {
    if (node.dataset.sp === 'agent') {
      node.innerHTML = `<div style="position:relative">${px(BUBBLE, BUBBLE_DOTS)}<div class="twinkle" style="position:absolute;right:-8%;top:-22%;width:26%">${px(SPARK)}</div></div>`
    } else if (node.dataset.sp === 'mac') {
      const wake = parseFloat(node.dataset.wake || '-1')
      node.innerHTML = wake < 0
        ? `<div style="position:relative">${px(MAC)}</div>`
        : `<div style="position:relative">${px(MAC_ASLEEP)}<div class="alt" style="animation:fade .01s steps(1) ${wake}s both">${px(MAC)}</div></div>`
    }
  })
  root.querySelectorAll<HTMLElement>('[data-ic]').forEach((node) => {
    node.querySelector(':scope > svg')?.remove()
    node.insertAdjacentHTML('afterbegin', px(node.dataset.ic === 'doc' ? DOC : FOLDER))
  })
  const cursor = root.querySelector('#cur')
  if (cursor) cursor.innerHTML = px(ARROW).replace(/^<svg[^>]*>/, '').replace(/<\/svg>$/, '')
}

function clock(seconds: number): string {
  const whole = Math.floor(seconds)
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`
}

const easeOut = (p: number) => 1 - Math.pow(1 - p, 3)

/** Starts the film inside `root` and returns a function that stops it. Safe to call twice (React StrictMode). */
export function mountIntro(root: HTMLElement): () => void {
  drawSprites(root)
  const scenes = Array.from(root.querySelectorAll<HTMLElement>('.scene'))
  const playButton = root.querySelector<HTMLButtonElement>('#pp')!
  const seek = root.querySelector<HTMLInputElement>('#seek')!
  const readout = root.querySelector<HTMLOutputElement>('#clock')!
  const chapter = root.querySelector<HTMLElement>('#chap')!
  const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches
  const stop = new AbortController()

  let t = reduceMotion ? 2 : 0
  let playing = !reduceMotion
  let last: number | null = null
  let frameId = 0
  playButton.textContent = playing ? 'Pause' : 'Play'

  function render() {
    for (const scene of scenes) {
      const start = Number(scene.dataset.s)
      const end = Number(scene.dataset.e)
      const on = t >= start && t < end
      scene.style.visibility = on ? 'visible' : 'hidden'
      if (!on) {
        scene.style.opacity = '0'
        continue
      }
      chapter.textContent = scene.dataset.ch || ''
      const fadeIn = Math.min((t - start) / FADE, 1)
      const fadeOut = Math.min((end - t) / FADE, 1)
      scene.style.opacity = String(Math.max(0, Math.min(fadeIn, fadeOut)))
      const local = t - start
      for (const animation of scene.getAnimations({ subtree: true })) {
        animation.pause()
        animation.currentTime = local * 1000
      }
      scene.querySelectorAll<HTMLElement>('.cnt').forEach((counter) => {
        const delay = Number(counter.dataset.d)
        const duration = Number(counter.dataset.dur)
        const progress = Math.max(0, Math.min(1, (local - delay) / duration))
        counter.textContent = String(Math.round(Number(counter.dataset.to) * easeOut(progress)))
      })
    }
    seek.value = String(t)
    readout.textContent = `${clock(t)} / ${clock(TOTAL)}`
  }

  function frame(now: number) {
    if (playing) {
      if (last !== null) t += Math.min((now - last) / 1000, 0.1)
      if (t >= TOTAL) {
        t = TOTAL - 0.01
        playing = false
        playButton.textContent = 'Replay'
      }
    }
    last = now
    render()
    frameId = requestAnimationFrame(frame)
  }

  playButton.addEventListener('click', () => {
    if (!playing && t >= TOTAL - 0.05) t = 0
    playing = !playing
    playButton.textContent = playing ? 'Pause' : 'Play'
  }, { signal: stop.signal })
  seek.addEventListener('input', () => {
    t = Number(seek.value)
    if (!playing) playButton.textContent = 'Play'
    render()
  }, { signal: stop.signal })

  frameId = requestAnimationFrame(frame)
  return () => {
    cancelAnimationFrame(frameId)
    stop.abort()
  }
}
