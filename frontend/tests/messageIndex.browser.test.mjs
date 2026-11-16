// Full App, real browser; only the Wails backend is a deterministic fixture.
// Opt in: POSTI_PLAYWRIGHT_PATH=/absolute/path/to/playwright/index.mjs node --test tests/messageIndex.browser.test.mjs
// No live mailbox or native app is used. The regular suite still covers layout policy.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'

const browserModule = process.env.POSTI_PLAYWRIGHT_PATH

test('real App: open mail, leave index, focus, resize and return on desktop and phone', { skip: !browserModule, timeout: 60000 }, async () => {
  const { chromium } = await import(browserModule)
  const server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '5187', '--strictPort'], { cwd: fileURLToPath(new URL('..', import.meta.url)), stdio: ['ignore', 'pipe', 'pipe'] })
  let serverLog = ''
  server.stdout.on('data', data => { serverLog += data })
  server.stderr.on('data', data => { serverLog += data })
  let browser
  try {
    for (let i = 0; i < 100; i++) {
      try { if ((await fetch('http://127.0.0.1:5187')).ok) break } catch { /* Vite starting */ }
      if (server.exitCode !== null || i === 99) throw new Error(`Vite did not start: ${serverLog}`)
      await delay(100)
    }
    browser = await chromium.launch({ executablePath: process.env.CHROME_BIN || '/usr/bin/google-chrome-stable', headless: true, args: ['--no-sandbox'] })
    for (const width of [767, 768, 1000, 1024, 1280]) {
      const page = await browser.newPage({ viewport: { width, height: 800 } })
      const errors = []
      page.on('pageerror', err => errors.push(err.message))
      await page.addInitScript(() => {
        localStorage.setItem('postilaatikko:context-pane-auto-hide', 'true')
        window.runtime = new Proxy({}, { get: (_, key) => key === 'EventsOnMultiple' ? () => () => {} : () => Promise.resolve() })
        const data = {
          IsReady: true, GetThemeMode: 'dark', GetLanguage: 'en', GetAccounts: [{ id: 'demo', name: 'Demo', email: 'demo@example.test', type: 'imap', enabled: true }],
          GetUIState: { selectedAccountId: 'unified', selectedFolderId: 'inbox', selectedFolderName: 'Inbox', selectedFolderType: 'inbox' },
          GetUnifiedFolderCount: 1,
          GetUnifiedFolderConversations: [{ threadId: 'thread1', accountId: 'demo', folderId: 'inbox', subject: 'Message index regression', snippet: 'Click to open', latestDate: new Date().toISOString(), messageCount: 1, unreadCount: 0, participants: [{ name: 'Fixture sender', email: 'fixture@example.test' }] }],
          GetConversation: { id: 'thread1', subject: 'Message index regression', messages: [{ id: 'msg1', accountId: 'demo', folderId: 'inbox', threadId: 'thread1', subject: 'Message index regression', fromName: 'Fixture sender', fromEmail: 'fixture@example.test', toList: [], ccList: [], date: new Date().toISOString(), bodyText: 'The list should remain beside this reader.', isRead: true }] },
        }
        window.go = { app: { App: new Proxy({}, { get: (_, key) => () => Promise.resolve(key in data ? data[key] : key.startsWith('Get') || key.startsWith('List') ? [] : null) }) } }
      })
      await page.goto('http://127.0.0.1:5187', { waitUntil: 'domcontentloaded' })
      const row = page.locator('[data-mail-row-key]')
      await row.waitFor({ state: 'attached' })
      await page.mouse.move(25, 300) // reveal the auto-hidden context pane
      await row.click()
      await page.getByText('The list should remain beside this reader.', { exact: true }).waitFor()
      await page.mouse.move(width - 80, 400)
      await page.waitForTimeout(650) // past the 350ms auto-hide timer and overlay transition
      const index = page.locator('[data-pane="messageList"]')
      const reader = page.locator('main[data-pane="viewer"]')
      const geometry = await page.evaluate(() => {
        const list = document.querySelector('[data-pane="messageList"]')
        const viewer = document.querySelector('main[data-pane="viewer"]')
        const l = list.getBoundingClientRect(), r = viewer.getBoundingClientRect()
        return { listWidth: l.width, readerWidth: r.width, listRight: l.right, readerLeft: r.left, overlay: viewer.classList.contains('responsive-viewer-overlay') }
      })
      if (width >= 768) {
        assert.ok(await index.isVisible(), `index visible at ${width}`)
        assert.equal(geometry.overlay, false, `not overlaid at ${width}`)
        assert.ok(geometry.listWidth > 200 && geometry.readerWidth > 300, JSON.stringify(geometry))
        assert.ok(geometry.listRight <= geometry.readerLeft + 1, JSON.stringify(geometry))
        // Explicit focus is still allowed to cover the index, then Escape restores it.
        await page.getByRole('button', { name: 'Focus thread', exact: true }).click()
        await page.waitForTimeout(300)
        assert.equal(await reader.evaluate(el => el.classList.contains('responsive-viewer-visible')), true)
        await page.keyboard.press('Escape')
        await page.waitForTimeout(300)
        assert.equal(await reader.evaluate(el => el.classList.contains('responsive-viewer-overlay')), false)
        await page.setViewportSize({ width: 767, height: 800 })
        await page.setViewportSize({ width, height: 800 })
        await page.waitForTimeout(300)
        assert.equal(await reader.evaluate(el => el.classList.contains('responsive-viewer-overlay')), false)
        assert.ok(await index.isVisible())
      } else {
        assert.equal(geometry.overlay, true, 'phone keeps single-pane reader')
        await page.keyboard.press('Escape')
        await page.waitForTimeout(300)
        await page.mouse.move(25, 300)
        assert.ok(await row.isVisible(), 'phone returns to index')
      }
      // Exercise the actual Contacts activation + kit DetailPane on the same
      // shared layout store, not a source-code facsimile of its behavior.
      await page.evaluate(async () => {
        const original = window.go.app.App
        const contact = { id: 'fixture-contact', name: 'Fixture contact', emails: ['contact@example.test'], phones: [], sourceId: 'aerion' }
        window.go.app.App = new Proxy(original, { get: (target, key) => key === 'Contacts_ListContactsForBrowse' ? () => Promise.resolve([contact]) : key === 'Contacts_GetContactDetail' ? () => Promise.resolve(contact) : target[key] })
        const { setActiveExtension } = await import('/src/lib/stores/uiState.svelte.ts')
        setActiveExtension('contacts')
      })
      const contactRow = page.getByText('Fixture contact', { exact: true }).first()
      await contactRow.click()
      const contactOverlay = page.locator('section.responsive-viewer-overlay').filter({ hasText: 'contact@example.test' })
      if (width <= 1024) {
        await contactOverlay.waitFor({ state: 'visible' })
        await page.waitForTimeout(300)
        assert.equal(await contactOverlay.evaluate(el => el.classList.contains('responsive-viewer-visible') && el.getBoundingClientRect().left < innerWidth / 2), true, `contact detail revealed at ${width}`)
        await contactOverlay.getByRole('button', { name: 'Back', exact: true }).click()
        await page.waitForTimeout(300)
        assert.equal(await contactOverlay.evaluate(el => el.classList.contains('responsive-viewer-visible')), false, `contact back at ${width}`)
        await contactRow.click()
        await page.setViewportSize({ width: 1280, height: 800 })
        await page.waitForTimeout(200)
        assert.equal(await page.locator('section.responsive-viewer-overlay').count(), 0, 'full width removes contact overlay')
        await page.setViewportSize({ width, height: 800 })
        await page.waitForTimeout(300)
        assert.equal(await contactOverlay.evaluate(el => el.classList.contains('responsive-viewer-visible')), false, 'returning from full clears old overlay state')
        await contactRow.click()
        await page.waitForTimeout(300)
        assert.equal(await contactOverlay.evaluate(el => el.classList.contains('responsive-viewer-visible')), true, 'contact reactivation after resize')
      } else {
        assert.equal(await page.locator('section.responsive-viewer-overlay').count(), 0)
        assert.ok(await page.getByText('contact@example.test', { exact: true }).last().isVisible())
      }
      assert.deepEqual(errors, [], `no render errors at ${width}`)
      if (process.env.POSTI_INDEX_SCREENSHOT && width === 1000) await page.screenshot({ path: process.env.POSTI_INDEX_SCREENSHOT.replace('.png', '-contacts.png') })
      await page.close()
    }
  } finally {
    if (browser) await browser.close()
    server.kill()
  }
})
