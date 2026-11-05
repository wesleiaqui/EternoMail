// P-029: shared list target resolution and message actions.
//
// resolveListTarget() is the single statement of the "checked messages, else
// the keyboard-focused row, else nothing" hierarchy documented in
// docs/KEYBOARD_SHORTCUTS.md. The key handlers, the command panel and (later)
// the selection bar must all use it.

import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function load(relative) {
  const source = await readFile(new URL(relative, import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const { resolveListTarget } = await load('../src/lib/actions/target.ts')
const { LIST_ACTIONS, getListAction, runListAction } = await load('../src/lib/actions/listActions.ts')

function source({ checked = [], focused = [], checkedHasUnstarred = false, selectedStarred = false, checkedHasUnread = false, selectedUnread = false } = {}) {
  return {
    hasChecked: () => checked.length > 0,
    checkedIds: () => checked,
    focusedIds: () => focused,
    checkedHasUnstarred: () => checkedHasUnstarred,
    selectedStarred: () => selectedStarred,
    checkedHasUnread: () => checkedHasUnread,
    selectedUnread: () => selectedUnread,
  }
}

function recordingExecutor() {
  const calls = []
  const record = (name) => (...args) => { calls.push([name, ...args]) }
  return {
    calls,
    executor: {
      archive: record('archive'),
      spam: record('spam'),
      markRead: record('markRead'),
      markUnread: record('markUnread'),
      toggleStar: record('toggleStar'),
      trash: record('trash'),
    },
  }
}

test('checked messages win over the focused row', () => {
  const target = resolveListTarget(source({ checked: ['m1', 'm2'], focused: ['m9'] }))
  assert.equal(target.kind, 'checked')
  assert.deepEqual(target.messageIds, ['m1', 'm2'])
})

test('the focused row is used when nothing is checked', () => {
  const target = resolveListTarget(source({ focused: ['m9', 'm10'] }))
  assert.equal(target.kind, 'focused')
  assert.deepEqual(target.messageIds, ['m9', 'm10'])
})

test('no checked messages and no focused row resolves to no target', () => {
  const target = resolveListTarget(source())
  assert.equal(target.kind, 'none')
  assert.deepEqual(target.messageIds, [])
})

test('star value: checked sets starred while any is unstarred, focused toggles the row', () => {
  assert.equal(resolveListTarget(source({ checked: ['a'], checkedHasUnstarred: true })).star, true)
  assert.equal(resolveListTarget(source({ checked: ['a'], checkedHasUnstarred: false })).star, false)
  assert.equal(resolveListTarget(source({ focused: ['a'], selectedStarred: false })).star, true)
  assert.equal(resolveListTarget(source({ focused: ['a'], selectedStarred: true })).star, false)
})

test('every list action is unavailable without a target', () => {
  const none = resolveListTarget(source())
  for (const def of LIST_ACTIONS) {
    assert.equal(def.isAvailable({ target: none }), false, def.id)
  }
})

test('every list action is available with a target', () => {
  const some = resolveListTarget(source({ focused: ['m1'] }))
  for (const def of LIST_ACTIONS) {
    assert.equal(def.isAvailable({ target: some }), true, def.id)
  }
})

test('list actions cover the mail commands the panel offers', () => {
  assert.deepEqual(
    LIST_ACTIONS.map((d) => d.id).sort(),
    ['mail.archive', 'mail.archiveAndMarkRead', 'mail.markRead', 'mail.markUnread', 'mail.spam', 'mail.star', 'mail.toggleRead', 'mail.trash'],
  )
})

test('actions receive MESSAGE ids from the resolved target, never a thread id', async () => {
  const { calls, executor } = recordingExecutor()
  const target = resolveListTarget(source({ focused: ['msg-uuid-1'] }))
  for (const id of ['mail.archive', 'mail.spam', 'mail.markRead', 'mail.markUnread']) {
    await runListAction(id, target, executor)
  }
  assert.deepEqual(calls, [
    ['archive', ['msg-uuid-1']],
    ['spam', ['msg-uuid-1']],
    ['markRead', ['msg-uuid-1']],
    ['markUnread', ['msg-uuid-1']],
  ])
})

test('star and trash pass the star value and permanence', async () => {
  const { calls, executor } = recordingExecutor()
  const target = resolveListTarget(source({ checked: ['a', 'b'], checkedHasUnstarred: true }))
  await runListAction('mail.star', target, executor)
  await runListAction('mail.trash', target, executor)
  await runListAction('mail.trash', target, executor, { permanent: true })
  assert.deepEqual(calls, [
    ['toggleStar', ['a', 'b'], true],
    ['trash', ['a', 'b'], false],
    ['trash', ['a', 'b'], true],
  ])
})

test('running an action with no target does nothing', async () => {
  const { calls, executor } = recordingExecutor()
  const none = resolveListTarget(source())
  for (const def of LIST_ACTIONS) await runListAction(def.id, none, executor)
  assert.deepEqual(calls, [])
})

test('unknown action ids are ignored', async () => {
  const { calls, executor } = recordingExecutor()
  await runListAction('mail.nope', resolveListTarget(source({ focused: ['a'] })), executor)
  assert.equal(getListAction('mail.nope'), undefined)
  assert.deepEqual(calls, [])
})

// --- App.svelte wiring (source-text guards, as in targetedUndoConsumers) ----

const appSource = await readFile(new URL('../src/App.svelte', import.meta.url), 'utf8')

test('regression: command panel mail actions never pass a thread id as a message id', () => {
  // Thread ids are the thread root's RFC Message-ID; the backend actions take
  // local message ids. Passing selectedThreadId archived/spammed nothing.
  assert.doesNotMatch(appSource, /handleBulk\w+\(\s*\[\s*selectedThreadId\s*\]/)
})

test('command panel list handlers go through the shared action layer', () => {
  for (const id of ['mail.archive', 'mail.spam', 'mail.trash', 'mail.star', 'mail.markRead', 'mail.markUnread']) {
    assert.match(appSource, new RegExp(`registerCommandHandler\\('${id.replace('.', '\\.')}',\\s*\\(\\)\\s*=>\\s*runListAction\\('${id.replace('.', '\\.')}'`), id)
  }
})

test('key handlers no longer repeat the checked/focused hierarchy by hand', () => {
  // One resolver; the pattern below was copy-pasted a dozen times.
  const repeats = appSource.match(/messageListRef\?\.hasCheckedMessages\(\)/g) ?? []
  assert.ok(repeats.length <= 2, `hasCheckedMessages() still appears ${repeats.length} times in App.svelte`)
})
