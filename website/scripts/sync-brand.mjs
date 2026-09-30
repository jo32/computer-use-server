import { access, mkdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'
import { resolve } from 'node:path'
import sharp from 'sharp'

const source = fileURLToPath(new URL('../../internal/brand/assets/readyrig-app-icon.png', import.meta.url))
const output = fileURLToPath(new URL('../public/brand/', import.meta.url))
const assets = [
  { file: 'app-icon.webp', size: 256, format: 'webp' },
  { file: 'favicon.png', size: 64, format: 'png' },
  { file: 'apple-touch-icon.png', size: 180, format: 'png' },
  { file: 'social-icon.png', size: 512, format: 'png' },
]

export async function syncBrand() {
  let artwork
  try {
    artwork = await readFile(source)
  } catch (error) {
    if (error.code !== 'ENOENT') throw error
    // A standalone copy of website/ can still build using committed assets.
    await Promise.all(assets.map(({ file }) => access(`${output}${file}`)))
    console.log('Brand: using bundled app icon.')
    return false
  }

  const digest = createHash('sha256').update(artwork).digest('hex')
  const manifestPath = `${output}manifest.json`
  try {
    const manifest = JSON.parse(await readFile(manifestPath, 'utf8'))
    if (manifest.sourceSha256 === digest) {
      await Promise.all(assets.map(({ file }) => access(`${output}${file}`)))
      return false
    }
  } catch { /* First build, updated artwork, or a missing output. */ }

  await mkdir(output, { recursive: true })
  await Promise.all(assets.map(async ({ file, size, format }) => {
    const buffer = await sharp(artwork).resize(size, size, { fit: 'contain' })
      .toFormat(format, format === 'webp' ? { quality: 90 } : { compressionLevel: 9 })
      .toBuffer()
    await writeFile(`${output}${file}`, buffer)
  }))
  await writeFile(manifestPath, `${JSON.stringify({ sourceSha256: digest, assets: assets.map(({ file }) => file) }, null, 2)}\n`)
  console.log('Brand: app icon, favicon, and social artwork synchronized.')
  return true
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  await syncBrand()
}
