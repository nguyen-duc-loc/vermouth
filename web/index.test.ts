import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { JSDOM } from 'jsdom'
import { describe, expect, it } from 'vitest'

const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')

type BootOptions = {
  dark?: boolean
  stored?: string
  blockedStorage?: boolean
}

function bootAppearance({ dark = false, stored, blockedStorage = false }: BootOptions = {}) {
  return new JSDOM(html, {
    runScripts: 'dangerously',
    url: 'http://vermouth.test/',
    beforeParse(window) {
      Object.defineProperty(window, 'matchMedia', {
        configurable: true,
        value: () => ({ matches: dark }),
      })

      if (blockedStorage) {
        Object.defineProperty(window, 'localStorage', {
          configurable: true,
          get() {
            throw new Error('storage is blocked')
          },
        })
      } else if (stored !== undefined) {
        window.localStorage.setItem('vermouth.appearance.v1', stored)
      }
    },
  })
}

describe('appearance boot script', () => {
  it('AC-2 applies a valid stored choice before the app mounts', () => {
    const dom = bootAppearance({
      stored: JSON.stringify({ theme: 'dark', accent: 'violet' }),
    })

    expect(dom.window.document.documentElement).toHaveAttribute('data-theme-choice', 'dark')
    expect(dom.window.document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(dom.window.document.documentElement).toHaveAttribute('data-accent', 'violet')
    expect(dom.window.document.documentElement.style.colorScheme).toBe('dark')
  })

  it('AC-2 defaults to the system theme and blue accent', () => {
    const dom = bootAppearance({ dark: true })

    expect(dom.window.document.documentElement).toHaveAttribute('data-theme-choice', 'system')
    expect(dom.window.document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(dom.window.document.documentElement).toHaveAttribute('data-accent', 'blue')
  })

  it('AC-3 keeps each valid field when another stored field is unknown', () => {
    const dom = bootAppearance({
      stored: JSON.stringify({ theme: 'unknown', accent: 'rose' }),
    })

    expect(dom.window.document.documentElement).toHaveAttribute('data-theme-choice', 'system')
    expect(dom.window.document.documentElement).toHaveAttribute('data-accent', 'rose')
  })

  it.each([
    ['malformed JSON', '{'],
    ['an old storage shape', JSON.stringify({ version: 0, color: 'red' })],
  ])('AC-3 falls back safely for %s', (_caseName, stored) => {
    const dom = bootAppearance({ stored })

    expect(dom.window.document.documentElement).toHaveAttribute('data-theme-choice', 'system')
    expect(dom.window.document.documentElement).toHaveAttribute('data-accent', 'blue')
  })

  it('AC-3 still creates the app root when storage access is blocked', () => {
    const dom = bootAppearance({ blockedStorage: true })

    expect(dom.window.document.getElementById('root')).toBeInTheDocument()
    expect(dom.window.document.documentElement).toHaveAttribute('data-theme-choice', 'system')
    expect(dom.window.document.documentElement).toHaveAttribute('data-accent', 'blue')
  })
})
