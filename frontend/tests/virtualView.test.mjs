import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'
const source = await readFile(new URL('../src/lib/sidebar/virtualView.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } })
const { retainReadSelection } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
test('a selected message stays under the cursor after becoming read', () => {
  const a = { threadId: 'a', unreadCount: 1 }, b = { threadId: 'b', unreadCount: 0 }, c = { threadId: 'c', unreadCount: 1 }
  assert.deepEqual(retainReadSelection([a, b, c], [a, c], 'b', true), [a, b, c])
  assert.deepEqual(retainReadSelection([a, c], [a, c], 'b', true), [a, c], 'explicitly removed rows stay removed')
  assert.deepEqual(retainReadSelection([a, b, c], [a, c], 'b', false), [a, c])
})

test('the active row keeps its index when a same-folder refresh resorts it', () => {
  const first = { threadId: 'first', unreadCount: 1 }
  const activeBefore = { threadId: 'active', unreadCount: 0 }
  const last = { threadId: 'last', unreadCount: 1 }
  const activeAfter = { threadId: 'active', unreadCount: 0 }

  assert.deepEqual(
    retainReadSelection([first, activeBefore, last], [activeAfter, first, last], 'active', false),
    [first, activeAfter, last],
  )
})
