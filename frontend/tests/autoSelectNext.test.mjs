import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadSelection() {
  const source = await readFile(new URL('../src/lib/components/list/autoSelectNext.ts', import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const { captureAutoSelectNext, findAutoSelectNext, flattenVisibleInboxGroups } = await loadSelection()
const rows = (...values) => values.map(([threadId, unreadCount]) => ({ threadId, unreadCount }))

test('unread A skips read rows and opens the next visible unread row', () => {
  const before = rows(['A', 1], ['B', 0], ['C', 1])
  const snapshot = captureAutoSelectNext(before, 'A', new Set())
  assert.equal(findAutoSelectNext(rows(['B', 0], ['C', 1]), snapshot)?.threadId, 'C')
})

test('unread origin remains preferred after pendingReadOnLeave changes it to read', () => {
  const snapshot = captureAutoSelectNext(rows(['A', 1], ['B', 0], ['C', 1]), 'A', new Set())
  assert.equal(snapshot.preferUnread, true)
  assert.equal(findAutoSelectNext(rows(['A', 0], ['B', 0], ['C', 1]), snapshot)?.threadId, 'C')
})

test('unread origin never falls back to a read row or wraps to the top', () => {
  const snapshot = captureAutoSelectNext(rows(['A', 1], ['B', 0], ['C', 0]), 'A', new Set())
  assert.equal(findAutoSelectNext(rows(['B', 0], ['C', 0]), snapshot), null)
})

test('a read origin retains ordinary next-row selection', () => {
  const snapshot = captureAutoSelectNext(rows(['A', 0], ['B', 0], ['C', 1]), 'A', new Set())
  assert.equal(findAutoSelectNext(rows(['B', 0], ['C', 1]), snapshot)?.threadId, 'B')
})

test('collapsed groups and card limits are excluded from the rendered category order', () => {
  const visible = flattenVisibleInboxGroups(
    [
      { id: 'notifications', conversations: rows(['A', 1], ['B', 0], ['hidden-by-card-limit', 1]) },
      { id: 'collapsed', conversations: rows(['hidden-by-collapse', 1]) },
      { id: 'people', conversations: rows(['C', 1]) },
    ],
    new Set(['collapsed']),
    false,
    group => group.id === 'notifications' ? group.conversations.slice(0, 2) : group.conversations,
  )
  const snapshot = captureAutoSelectNext(visible, 'A', new Set())
  assert.deepEqual(visible.map(row => row.threadId), ['A', 'B', 'C'])
  assert.equal(findAutoSelectNext(rows(['B', 0], ['C', 1]), snapshot)?.threadId, 'C')
})

test('search results use the same visual order and unread preference', () => {
  const snapshot = captureAutoSelectNext(rows(['A', 1], ['B', 0], ['C', 1]), 'A', new Set())
  assert.equal(findAutoSelectNext(rows(['B', 0], ['C', 1]), snapshot)?.threadId, 'C')
})
