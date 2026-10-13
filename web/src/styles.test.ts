import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const styles = readFileSync(new URL('./styles.css', import.meta.url), 'utf8')

describe('responsive and keyboard presentation', () => {
  it('keeps the 320px floor, mobile layouts, and visible focus treatment', () => {
    expect(styles).toContain('min-width: 320px')
    expect(styles).toContain('@media (max-width: 760px)')
    expect(styles).toContain('@media (max-width: 420px)')
    expect(styles).toContain(':focus-visible')
    expect(styles).toContain('prefers-reduced-motion')
    expect(styles).toContain(':root[data-theme="light"]')
    expect(styles).toContain('.preference-controls')
  })
})
