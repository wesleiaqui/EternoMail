// Olli's rule: every command has a menu entry. A button is optional, a key is
// optional and is shown in the menu when there is one. A command's menu entry
// is declared with `menu` in registry.ts (the native/app menu is generated from
// it). MENU_DEBT lists the commands that do not have one yet. The test fails
// when a command is missing a menu entry and is not listed, and when a listed
// command gets one (so the list can only shrink). New commands get no debt.

import { test, after } from 'node:test'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import { createServer } from 'vite'

const frontendDir = fileURLToPath(new URL('..', import.meta.url))
const server = await createServer({
  root: frontendDir,
  configFile: path.join(frontendDir, 'vite.config.ts'),
  logLevel: 'error',
  server: { middlewareMode: true, hmr: false, watch: null },
  appType: 'custom',
  optimizeDeps: { noDiscovery: true, include: [] },
})
after(() => server.close())
const { CORE_SHORTCUTS } = await server.ssrLoadModule('/src/lib/keyboard/registry.ts')

const MENU_DEBT = [
  'app.commandPalette',
  'app.shortcuts',
  'app.preferences',
  'app.compose',
  'app.search',
  'app.folders',
  'app.undo',
  'app.syncAll',
  'app.syncFolder',
  'app.nextRail',
  'app.prevRail',
  'app.closeTab',
  'app.quit',
  'nav.historyBack',
  'nav.historyForward',
  'nav.paneNext',
  'nav.panePrev',
  'nav.folderNext',
  'nav.folderPrev',
  'nav.folderFirst',
  'nav.folderLast',
  'nav.toggleAccount',
  'nav.accountPrev',
  'nav.accountNext',
  'nav.folderMenu',
  'sidebar.next',
  'sidebar.prev',
  'sidebar.toggle',
  'list.next',
  'list.prev',
  'list.first',
  'list.last',
  'list.nextCheck',
  'list.prevCheck',
  'list.toggleCheck',
  'list.open',
  'list.selectAll',
  'list.contextMenu',
  'list.escape',
  'list.filterAll',
  'list.filterUnread',
  'list.filterReadLater',
  'mail.reply',
  'mail.replyAll',
  'mail.forward',
  'mail.archiveAndMarkRead',
  'mail.readLater',
  'mail.spam',
  'mail.trash',
  'mail.deletePermanently',
  'mail.star',
  'mail.markRead',
  'mail.markUnread',
  'mail.toggleRead',
  'mail.moveTo',
  'mail.copyTo',
  'viewer.scrollDown',
  'viewer.scrollUp',
  'viewer.threadFocus',
  'viewer.messageFocus',
  'viewer.selectText',
  'viewer.loadImages',
  'viewer.alwaysLoadImages',
  'composer.send',
  'composer.attach',
  'composer.detach',
  'composer.toolbar',
  'composer.pgp',
  'composer.smime',
  'composer.bold',
  'composer.italic',
  'composer.underline',
  'composer.undo',
  'composer.redo',
  'composer.close',
  'lists.next',
  'lists.prev',
  'lists.first',
  'lists.last',
  'lists.open',
  'lists.delete',
]

test('every command has a menu entry, except the listed debt', () => {
  const missing = CORE_SHORTCUTS.filter(d => !d.menu).map(d => d.id)
  assert.deepEqual(missing.filter(id => !MENU_DEBT.includes(id)), [], 'new command without a menu entry')
  assert.deepEqual(MENU_DEBT.filter(id => !missing.includes(id)), [], 'remove from MENU_DEBT: it has a menu entry now')
})
