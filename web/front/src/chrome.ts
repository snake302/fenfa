// Shared visual pieces for the public pages: inline icons and the hero tint
// sampled from the product icon.

export const icons = {
  back: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>',
  download: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 4v11"/><path d="M7.5 10.5 12 15l4.5-4.5"/><path d="M4 17v1.5A1.5 1.5 0 0 0 5.5 20h13a1.5 1.5 0 0 0 1.5-1.5V17"/></svg>',
  device: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="6" y="2.5" width="12" height="19" rx="3"/><path d="M10.5 18.5h3"/></svg>',
  signOut: '<svg width="19" height="19" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 4h3.5A1.5 1.5 0 0 1 19 5.5v13a1.5 1.5 0 0 1-1.5 1.5H14"/><path d="M10 16.5 5.5 12 10 7.5"/><path d="M5.5 12H15"/></svg>',
  lock: '<svg width="44" height="44" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="5" y="10.5" width="14" height="10" rx="2.5"/><path d="M8.5 10.5V7.5a3.5 3.5 0 0 1 7 0v3"/></svg>'
}

export const defaultTint = 'linear-gradient(rgb(40, 66, 66), rgb(16, 22, 22))'

// Picks the most saturated colour of the icon so dark icons with a bright glyph
// still give the hero a recognisable hue. Cross-origin icons without CORS
// headers cannot be read; those keep the default tint.
export function sampleIconTint(src: string): Promise<string> {
  return new Promise((resolve) => {
    if (!src) return resolve(defaultTint)
    const image = new Image()
    image.crossOrigin = 'anonymous'
    image.onload = () => {
      try {
        const size = 24
        const canvas = document.createElement('canvas')
        canvas.width = size
        canvas.height = size
        const ctx = canvas.getContext('2d')
        if (!ctx) return resolve(defaultTint)
        ctx.drawImage(image, 0, 0, size, size)
        const pixels = ctx.getImageData(0, 0, size, size).data
        let r = 0, g = 0, b = 0, total = 0
        for (let i = 0; i < pixels.length; i += 4) {
          if (pixels[i + 3] < 128) continue
          const max = Math.max(pixels[i], pixels[i + 1], pixels[i + 2])
          const min = Math.min(pixels[i], pixels[i + 1], pixels[i + 2])
          const weight = 0.02 + ((max - min) / 255) ** 2
          r += pixels[i] * weight
          g += pixels[i + 1] * weight
          b += pixels[i + 2] * weight
          total += weight
        }
        if (!total) return resolve(defaultTint)
        const top = [r, g, b].map((channel) => channel / total)
        // Normalise brightness so every product gets an equally subtle wash.
        const scale = 150 / Math.max(...top, 1)
        const start = top.map((channel) => Math.round(Math.min(255, channel * scale)))
        const end = start.map((channel) => Math.round(channel * 0.3))
        resolve(`linear-gradient(rgb(${start.join(', ')}), rgb(${end.join(', ')}))`)
      } catch {
        resolve(defaultTint)
      }
    }
    image.onerror = () => resolve(defaultTint)
    image.src = src
  })
}
