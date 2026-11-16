import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8')
const layout = readFileSync(new URL('../src/lib/stores/layout.svelte.ts', import.meta.url), 'utf8')
function derived(name) {
  return app.match(new RegExp(`const ${name} = \\$derived\\(([^\\n]+)\\)`))[1]
}
function evaluate(expression, values) {
  return Function(...Object.keys(values), `return (${expression})`)(...Object.values(values))
}

test('opening a message keeps the index beside the reader at medium and full desktop widths', () => {
  for (const mode of ['medium', 'full']) {
    const values = { getLayoutMode: () => mode, isResponsive: () => mode !== 'full', focusMode: 'off' }
    assert.equal(evaluate(derived('viewerIsOverlay'), values), false, mode)
  }
})

test('phone drill-down and explicit reader focus remain overlays', () => {
  for (const mode of ['narrow', 'medium', 'full']) {
    for (const focusMode of ['off', 'thread', 'message']) {
      assert.equal(evaluate(derived('viewerIsOverlay'), { getLayoutMode: () => mode, isResponsive: () => mode !== 'full', focusMode }), mode === 'narrow' || focusMode !== 'off')
    }
  }
})

test('auto-hide cannot hide the desktop index while a conversation is selected', () => {
  for (const mode of ['medium', 'full']) {
    const values = { contextPaneAutoHide: true, contextPaneRevealed: false, getLayoutMode: () => mode, selectedThreadId: 'thread-1' }
    assert.equal(evaluate(derived('contextPaneVisible'), values), true, mode)
    values.selectedThreadId = null
    assert.equal(evaluate(derived('contextPaneVisible'), values), false, 'auto-hide still works without an opened conversation')
  }
})

test('shared overlay transitions remain available to Contacts at medium widths', () => {
  const showViewer = layout.match(/export function showViewer\(\) \{([\s\S]*?)\n\}/)[1]
  for (const mode of ['narrow', 'medium', 'full']) {
    const next = Function('layoutMode', `let responsiveView = 'default'; function run() {${showViewer}} run(); return responsiveView`)(mode)
    assert.equal(next, mode !== 'full' ? 'viewer' : 'default', mode)
  }
})

test('desktop reader remains considered visible for mark-as-read lifecycle', () => {
  assert.ok(/isViewerVisible=\{[^\n]+getLayoutMode\(\) !== 'narrow'/.test(app))
})
