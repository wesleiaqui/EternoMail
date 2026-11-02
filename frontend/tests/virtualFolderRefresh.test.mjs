import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'
const source = await readFile(new URL('../src/lib/components/list/virtualFolderRefresh.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
const { refreshVirtualView, isExpectedSearchCancellation } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
test('manual refresh recomputes index before resetting cursor and loading live results', async () => {
  const calls = []
  await refreshVirtualView({ refreshCache: async () => calls.push('cache'), isCurrent: () => true, reset: () => calls.push('reset'), reload: async () => calls.push('load') })
  assert.deepEqual(calls, ['cache', 'reset', 'load'])
})
test('navigation during index refresh cannot reset or reload the new folder', async () => {
  const calls = []
  await refreshVirtualView({ refreshCache: async () => calls.push('cache'), isCurrent: () => false, reset: () => calls.push('reset'), reload: async () => calls.push('load') })
  assert.deepEqual(calls, ['cache'])
})
test('index failure preserves current rows and propagates a genuine error', async () => {
  let reset = false
  await assert.rejects(refreshVirtualView({ refreshCache: async () => { throw new Error('disk unavailable') }, isCurrent: () => true, reset: () => { reset = true }, reload: async () => {} }), /disk unavailable/)
  assert.equal(reset, false)
})
test('only known cancellation and supersession are normal outcomes', () => {
  for (const err of ['context canceled', 'context cancelled', 'search superseded', new DOMException('aborted', 'AbortError')]) assert.equal(isExpectedSearchCancellation(err), true)
  for (const err of ['Gmail quota exceeded', 'permission denied', 'unknown error', null]) assert.equal(isExpectedSearchCancellation(err), false)
})


// Exercise the actual Svelte entry point too: the helper alone cannot prove
// double-click suppression, branch selection or generation-safe error display.
const messageListSource = await readFile(new URL('../src/lib/components/list/MessageList.svelte', import.meta.url), 'utf8')
const start = messageListSource.indexOf('export async function syncFolder()')
assert.notEqual(start, -1)
const end = messageListSource.indexOf('// Cancel folder sync', start)
const entryPoint = ts.transpileModule(messageListSource.slice(start, end).replace('export ', ''), { compilerOptions: { module: ts.ModuleKind.None, target: ts.ScriptTarget.ES2022 } }).outputText
function integrationHarness(overrides = {}) {
  const calls = []
  const state = {
    manualSyncing: false, isUnifiedView: false, folderId: 'view-a',
    isVirtualFolderView: true, loadGeneration: 7, isSearchMode: false,
    listError: null, offset: 50, virtualCursor: 'old-cursor',
    conversations: [{ id: 'keep-me' }], PAGE_SIZE: 50,
    ...overrides,
  }
  const dependencies = {
    Math, refreshVirtualView, isExpectedSearchCancellation,
    SyncVirtualFolder: async view => calls.push(['sync', view]),
    String,
    loadConversations: async count => calls.push(['load', count]),
    $_: key => key,
  }
  // Production entry point closes over Svelte state. A Proxy-backed scope
  // makes those same assignments observable without compiling a mock component.
  const scope = new Proxy({ ...state, ...dependencies }, {
    has: () => true,
    get: (target, key) => key === Symbol.unscopables ? undefined : target[key],
  })
  const run = new Function('scope', `with (scope) { return (${entryPoint}); }`)(scope)
  return { run, scope, calls }
}
test('Svelte refresh entry point ignores duplicate clicks while cache work is pending', async () => {
  const h = integrationHarness()
  let finish
  h.scope.SyncVirtualFolder = view => {
    h.calls.push(['sync', view])
    return new Promise(resolve => { finish = resolve })
  }
  const first = h.run()
  assert.equal(h.scope.manualSyncing, true)
  await h.run()
  assert.deepEqual(h.calls, [['sync', 'view-a']])
  finish()
  await first
  assert.deepEqual(h.calls, [['sync', 'view-a'], ['load', 50]])
  assert.equal(h.scope.offset, 0)
  assert.equal(h.scope.virtualCursor, '')
  assert.equal(h.scope.manualSyncing, false)
})
test('Svelte refresh entry point does not reset a view reached during cache refresh', async () => {
  const h = integrationHarness()
  h.scope.SyncVirtualFolder = async () => { h.scope.folderId = 'view-b' }
  await h.run()
  assert.equal(h.scope.offset, 50)
  assert.equal(h.scope.virtualCursor, 'old-cursor')
  assert.deepEqual(h.calls, [])
  assert.equal(h.scope.manualSyncing, false)
})
test('Svelte refresh entry point reports real cache failure without clearing rows or cursor', async () => {
  const h = integrationHarness()
  h.scope.SyncVirtualFolder = async () => { throw new Error('disk unavailable') }
  await h.run()
  assert.equal(h.scope.virtualIssues, 'Error: disk unavailable')
  assert.equal(h.scope.virtualCursor, 'old-cursor')
  assert.deepEqual(h.scope.conversations, [{ id: 'keep-me' }])
  assert.deepEqual(h.calls, [])
  assert.equal(h.scope.manualSyncing, false)
})
test('Svelte refresh entry point keeps cancellation and superseded-view errors quiet', async () => {
  for (const mode of ['cancel', 'navigation']) {
    const h = integrationHarness()
    h.scope.SyncVirtualFolder = async () => {
      if (mode === 'navigation') h.scope.loadGeneration += 1
      throw new Error(mode === 'cancel' ? 'context canceled' : 'disk unavailable')
    }
    await h.run()
    assert.equal(h.scope.virtualIssues, '')
    assert.equal(h.scope.offset, 50)
    assert.deepEqual(h.calls, [])
  }
})
