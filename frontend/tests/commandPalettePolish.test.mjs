// Command palette: Cmd/Ctrl+K opens it (and archives nothing), repeatable commands keep it open, and
// the lesser-used mail commands are listed. Source-level checks, no DOM.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const read = file => readFileSync(new URL(`../src/lib/${file}`, import.meta.url), 'utf8')
const registry = read('keyboard/registry.ts')
const shortcuts = read('keyboard/shortcuts.ts')
const store = read('stores/commandPalette.svelte.ts')

function commandLine(id) {
  const line = registry.split('\n').find(l => l.includes(`{ id: '${id}',`))
  assert.ok(line, `${id} is in CORE_COMMANDS`)
  return line
}

test('Cmd/Ctrl+K opens the palette and archives nothing; E archives and marks read', () => {
  assert.match(shortcuts, /COMMAND_PALETTE[\s\S]{0,200}e\.key === 'k'/)
  assert.match(shortcuts, /COMMAND_PALETTE[\s\S]{0,200}e\.key === 'p'/)
  assert.match(registry, /id: 'app\.commandPalette'[^\n]*combos: \['Mod\+k', 'Mod\+p', 'Mod\+Shift\+p'\]/)
  assert.doesNotMatch(registry, /id: 'mail\.archive',[^\n]*combos/)
  assert.match(registry, /id: 'mail\.archiveAndMarkRead'[^\n]*combos: \['e', 'Mod\+e'\]/)
  // No Mod+k combo anywhere but the palette.
  const modK = registry.split('\n').filter(l => /combos:[^\]]*'Mod\+k'/.test(l))
  assert.equal(modK.length, 1)
  assert.match(modK[0], /app\.commandPalette/)
  const app = readFileSync(new URL('../src/App.svelte', import.meta.url), 'utf8')
  assert.doesNotMatch(app, /case 'k':\s*\n\s*e\.preventDefault\(\)\s*\n\s*void executeCommand\('mail\.archive'\)/)
})

test('repeatable commands keep the palette open', () => {
  for (const id of ['app.undo', 'theme.toggle', 'mail.star', 'mail.markRead', 'mail.markUnread']) {
    assert.match(commandLine(id), /keepOpen: true/, id)
  }
  assert.doesNotMatch(commandLine('mail.archive'), /keepOpen/)
  assert.match(store, /if \(!getCommand\(id\)\?\.keepOpen\) closeCommandPalette\(\)/)
})

test('lesser-used mail commands are in the palette; permanent delete stays out', () => {
  for (const id of ['mail.toggleRead', 'mail.readLater', 'mail.moveTo', 'mail.copyTo']) {
    assert.doesNotMatch(commandLine(id), /palette: false/, id)
    assert.match(commandLine(id), /category: 'commandPalette\.categories\.mail'/, id)
  }
  assert.match(commandLine('mail.deletePermanently'), /palette: false/)
})

test('recents are reactive, newest first, capped at eight, and only the palette records them', () => {
  const palette = readFileSync(new URL('../src/lib/components/command/CommandPalette.svelte', import.meta.url), 'utf8')
  assert.match(store, /RECENT_MAX = 8/)
  assert.match(store, /let recent = \$state<string\[\]>/)
  assert.match(store, /recent = \[id, \.\.\.recent\.filter\(entry => entry !== id\)\]\.slice\(0, RECENT_MAX\)/)
  // executeCommand (keyboard shortcuts, hidden commands) must not write history.
  const execute = store.slice(store.indexOf('export async function executeCommand'))
  assert.doesNotMatch(execute, /recordRecentCommand/)
  assert.match(palette, /recordRecentCommand\(cmd\.id\)/)
})

test('after a keep-open command the selection follows it by id', () => {
  const palette = readFileSync(new URL('../src/lib/components/command/CommandPalette.svelte', import.meta.url), 'utf8')
  assert.match(palette, /selectedIndex = Math\.max\(0, filteredCommands\.findIndex\(item => item\.id === cmd\.id\)\)/)
})

test('Enter on an unmatched query goes through runCommand, so Search lands in recents', async () => {
  const { readFile } = await import('node:fs/promises')
  const src = await readFile(new URL('../src/lib/components/command/CommandPalette.svelte', import.meta.url), 'utf8')
  assert.match(src, /runCommand\(searchFallbackItem\(\)\)/)
  assert.doesNotMatch(src, /\/\/ Fallback: trigger search[\s\S]{0,80}executeCommand\('app\.search'\)\n/)
})

test('archive and mark read remembers unread ids and restores them from every undo path', async () => {
  const { readFile } = await import('node:fs/promises')
  const events = await readFile(new URL('../src/lib/components/list/undoMutationEvents.ts', import.meta.url), 'utf8')
  assert.match(events, /rememberUnreadForUndo/)
  assert.match(events, /restoreUnread\?\.\(unread\)/)
  for (const f of ['src/App.svelte', 'src/lib/components/viewer/ConversationViewer.svelte']) {
    const src = await readFile(new URL('../' + f, import.meta.url), 'utf8')
    assert.match(src, /rememberUnreadForUndo\(result\.operationId, unreadIds\)/, f)
  }
})

test('unread snapshots expire by age, not count, and the list supplies them without the backend', async () => {
  const { readFile } = await import('node:fs/promises')
  const events = await readFile(new URL('../src/lib/components/list/undoMutationEvents.ts', import.meta.url), 'utf8')
  assert.match(events, /UNREAD_SNAPSHOT_TTL_MS/)
  assert.doesNotMatch(events, /MAX_UNREAD_SNAPSHOTS/)
  const list = await readFile(new URL('../src/lib/components/list/MessageList.svelte', import.meta.url), 'utf8')
  assert.match(list, /export function getUnreadSnapshot\(/)
  const app = await readFile(new URL('../src/App.svelte', import.meta.url), 'utf8')
  assert.match(app, /messageListRef\?\.getUnreadSnapshot\(messageIds\)/)
})

test('read changes keep the search result rows in step with the list', async () => {
  const { readFile } = await import('node:fs/promises')
  const list = await readFile(new URL('../src/lib/components/list/MessageList.svelte', import.meta.url), 'utf8')
  assert.match(list, /applyReadDelta\(searchResults,/)
  assert.match(list, /applyReadDelta\(serverSearchResults,/)
})
