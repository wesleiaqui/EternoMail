import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

const source = await readFile(new URL('../src/lib/components/list/activeInboxGroup.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } })
const { pinnedGroupForSelection, retainPinnedConversationPosition } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)

test('the selected inbox conversation stays in its original group after it becomes read', () => {
  const unreadGroup = { id: 'priority', label: 'Important', category: 'priority' }
  const pin = { threadId: 'open-thread', group: unreadGroup, index: 1 }

  assert.equal(pinnedGroupForSelection('open-thread', 'open-thread', pin), unreadGroup)
  assert.equal(pinnedGroupForSelection('open-thread', 'another-thread', pin), null, 'the old group is released after selection changes')
})

test('a pinned conversation keeps its row position when other messages reorder', () => {
  const rows = [
    { threadId: 'new-top' },
    { threadId: 'open-thread' },
    { threadId: 'old-row' },
  ]

  assert.deepEqual(retainPinnedConversationPosition(rows, 'open-thread', 0), [
    { threadId: 'open-thread' },
    { threadId: 'new-top' },
    { threadId: 'old-row' },
  ])
})
