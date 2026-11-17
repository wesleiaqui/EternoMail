// Full App, real browser; only the Wails backend is a deterministic fixture.
// Opt in: POSTI_PLAYWRIGHT_PATH=/absolute/path/to/playwright/index.mjs node --test tests/messageIndex.browser.test.mjs
// No live mailbox or native app is used. The regular suite still covers layout policy.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'

const browserModule = process.env.POSTI_PLAYWRIGHT_PATH

test('real App: hidden startup selection rejected and hiding active folder leaves its view', { skip: !browserModule, timeout: 60000 }, async () => {
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
    for (const width of [1280]) {
      const page = await browser.newPage({ viewport: { width, height: 800 } })
      const errors = []
      page.on('pageerror', err => errors.push(err.message))
      await page.addInitScript(() => {
        localStorage.setItem('postilaatikko:context-pane-auto-hide', 'true')
        window.runtime = new Proxy({}, { get: (_, key) => key === 'EventsOnMultiple' ? () => () => {} : () => Promise.resolve() })
        const data = {
          IsReady: true, GetThemeMode: 'dark', GetLanguage: 'en', GetAccounts: [{ id: 'demo', name: 'Demo', email: 'demo@example.test', type: 'imap', enabled: true }],
          GetUIState: { selectedAccountId: 'demo', selectedFolderId: 'old', selectedFolderName: 'Inbox', selectedFolderType: 'inbox' },
          GetFolders:[{id:'old',accountId:'demo',name:'Old',path:'Old',type:'folder',hidden:true}],
          GetUnifiedFolderCount: 1,
          GetUnifiedFolderConversations: [{ threadId: 'thread1', accountId: 'demo', folderId: 'inbox', subject: 'Message index regression', snippet: 'Click to open', latestDate: new Date().toISOString(), messageCount: 1, unreadCount: 0, participants: [{ name: 'Fixture sender', email: 'fixture@example.test' }] }],
          GetConversation: { id: 'thread1', subject: 'Message index regression', messages: [{ id: 'msg1', accountId: 'demo', folderId: 'inbox', threadId: 'thread1', subject: 'Message index regression', fromName: 'Fixture sender', fromEmail: 'fixture@example.test', toList: [], ccList: [], date: new Date().toISOString(), bodyText: 'The list should remain beside this reader.', isRead: true }] },
        }
        window.go = { app: { App: new Proxy({}, { get: (_, key) => () => Promise.resolve(key in data ? data[key] : key.startsWith('Get') || key.startsWith('List') ? [] : null) }) } }
      })
      await page.goto('http://127.0.0.1:5187', { waitUntil: 'domcontentloaded' })

      await page.waitForFunction(()=>document.querySelector('[data-native-context]'))
      await page.waitForFunction(()=>document.querySelector('[data-native-context]').dataset.nativeContext.includes('mail'))
      assert.equal(await page.locator('[data-native-mail-primary]').evaluate(el=>el.dataset.nativeContext.includes('old')),false,'startup cannot restore hidden folder')
      await page.evaluate(async()=>{
        const {accountStore}=await import('/src/lib/stores/accounts.svelte.ts')
        accountStore.folderCatalog.demo=[{id:'old',accountId:'demo',name:'Old',hidden:false}]
        window.dispatchEvent(new CustomEvent('postilaatikko:navigate',{detail:{id:'folder:demo:old',kind:'folder',accountId:'demo',folderId:'old',name:'Old',path:'Old'}}))
      })
      await page.waitForFunction(()=>document.querySelector('[data-native-context]').dataset.nativeContext.includes('old'))
      await page.evaluate(async()=>{
        const {accountStore}=await import('/src/lib/stores/accounts.svelte.ts')
        accountStore.folderCatalog.demo=[{id:'old',accountId:'demo',name:'Old',hidden:true}]
      })
      await page.waitForFunction(()=>!document.querySelector('[data-native-context]').dataset.nativeContext.includes('old'))
      assert.deepEqual(errors,[])
      await page.close()
    }
  } finally { await browser?.close(); server.kill() }
})
