import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadTracker() {
  const source = await readFile(new URL('../src/lib/components/viewer/pendingReadOnLeave.ts', import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const { PendingReadOnLeave } = await loadTracker()

test('opening and waiting keep unread messages pending without marking them', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([{ id: 'a', isRead: false }, { id: 'b', isRead: true }])
  assert.deepEqual(pending.take(), ['a'])
})

test('leaving a thread marks exactly all messages unread at entry', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([
    { id: 'read', isRead: true },
    { id: 'unread-1', isRead: false },
    { id: 'unread-2', isRead: false },
  ])
  assert.deepEqual(pending.take(), ['unread-1', 'unread-2'])
  assert.deepEqual(pending.take(), [])
})

test('direct A to B finalizes A before B gets its own unchanged snapshot', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([{ id: 'a', isRead: false }])
  assert.deepEqual(pending.take(), ['a'])
  pending.capture([{ id: 'b', isRead: false }])
  assert.deepEqual(pending.take(), ['b'])
})

test('a later manual or external read-state update cancels only those IDs', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([{ id: 'a', isRead: false }, { id: 'b', isRead: false }])
  pending.cancel(['a'])
  assert.deepEqual(pending.take(), ['b'])
})

test('refreshes cannot add a message that became unread after opening', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([{ id: 'already-read', isRead: true }, { id: 'opened-unread', isRead: false }])
  assert.deepEqual(pending.take(), ['opened-unread'])
})

test('an unrendered or discarded snapshot can be cleared without a read operation', () => {
  const pending = new PendingReadOnLeave()
  pending.capture([{ id: 'startup', isRead: false }])
  pending.clear()
  assert.deepEqual(pending.take(), [])
})
