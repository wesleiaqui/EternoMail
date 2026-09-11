import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadServerSearchResults() {
  const source = await readFile(new URL('../src/lib/components/list/serverSearchResults.ts', import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const { removeServerSearchResults, serverSearchResultIdentity, ServerSearchRequestState, updateServerSearchCounts } = await loadServerSearchResults()
const result = (threadId, accountId, folderId, uid, messageIds = []) => ({ threadId, accountId, folderId, _uid: uid, messageIds })

test('Done removes a local server-search result by its message identity', () => {
  const a = result('A', 'account', 'inbox', 1, ['message-A'])
  const b = result('B', 'account', 'inbox', 2, ['message-B'])
  const updated = removeServerSearchResults([a, b], ['message-A'])
  assert.deepEqual(updated.results.map(item => item.threadId), ['B'])
  assert.equal(updated.removed, 1)
})

test('a fetched non-local result keeps its UID identity and is removed after Done', () => {
  const fetched = result('local-thread', 'account', 'inbox', 7, ['fetched-message'])
  const updated = removeServerSearchResults([fetched], ['fetched-message'])
  assert.equal(updated.results.length, 0)
})

test('a stale FetchServerMessage result is invalidated by account, folder and UID', () => {
  const stale = result('server-uid-42', 'account', 'inbox', 42)
  const updated = removeServerSearchResults([stale], [], [serverSearchResultIdentity(stale)])
  assert.equal(updated.results.length, 0)
})

test('Unified Inbox results with identical UIDs in different mailboxes do not collide', () => {
  const inboxA = result('A', 'account-A', 'inbox-A', 9, ['message-A'])
  const inboxB = result('B', 'account-B', 'inbox-B', 9, ['message-B'])
  const updated = removeServerSearchResults([inboxA, inboxB], [], [serverSearchResultIdentity(inboxA)])
  assert.deepEqual(updated.results.map(item => item.threadId), ['B'])
})

test('optimistic removal keeps visible and server totals coherent until reconciliation', () => {
  assert.deepEqual(updateServerSearchCounts(3, 21, 1), { count: 2, totalCount: 20 })
  assert.deepEqual(updateServerSearchCounts(0, 0, 1), { count: 0, totalCount: 0 })
})

test('an older server-search reply cannot restore an invalidated result', () => {
  const stale = result('A', 'account', 'inbox', 1, ['message-A'])
  const state = new ServerSearchRequestState()
  const request = state.begin('github')
  state.invalidate([serverSearchResultIdentity(stale)])
  assert.equal(state.accepts(request, 'github'), false)
  const currentRequest = state.begin('github')
  assert.equal(state.accepts(currentRequest, 'github'), true)
  assert.deepEqual(state.filter([stale, result('B', 'account', 'inbox', 2)]).results.map(item => item.threadId), ['B'])
})

test('a fresh query clears prior tombstones and can use its authoritative response', () => {
  const stale = result('A', 'account', 'inbox', 1)
  const state = new ServerSearchRequestState()
  state.begin('github')
  state.invalidate([serverSearchResultIdentity(stale)])
  state.begin('gitlab')
  assert.deepEqual(state.filter([stale]).results.map(item => item.threadId), ['A'])
})

test('two invalidations are cumulative and duplicate events do not change the result twice', () => {
  const a = result('A', 'account', 'inbox', 1, ['message-A'])
  const b = result('B', 'account', 'inbox', 2, ['message-B'])
  const state = new ServerSearchRequestState()
  state.begin('github')
  state.invalidate([serverSearchResultIdentity(a)])
  state.invalidate([serverSearchResultIdentity(b)])
  const first = removeServerSearchResults([a, b], ['message-A'])
  const second = removeServerSearchResults(first.results, ['message-A'])
  assert.equal(first.removed, 1)
  assert.equal(second.removed, 0)
  assert.deepEqual(state.filter([a, b]).results, [])
})

test('five consecutive destructive actions only shrink the cached result set', () => {
  let results = [1, 2, 3, 4, 5].map(uid => result(`thread-${uid}`, 'account', 'inbox', uid, [`message-${uid}`]))
  const state = new ServerSearchRequestState()
  const initialRequest = state.begin('reis')

  for (let uid = 1; uid <= 5; uid += 1) {
    const row = results.find(item => item._uid === uid)
    state.invalidate([serverSearchResultIdentity(row)])
    results = removeServerSearchResults(results, [], [serverSearchResultIdentity(row)]).results
    assert.equal(results.length, 5 - uid)
  }

  assert.equal(state.accepts(initialRequest, 'reis'), false)
  assert.deepEqual(results, [])
})

test('an explicit same-query server search starts a new request instead of becoming a no-op', () => {
  const state = new ServerSearchRequestState()
  const first = state.begin('reis')
  const explicitRepeat = state.begin('reis')
  assert.notEqual(first, explicitRepeat)
  assert.equal(state.accepts(first, 'reis'), false)
  assert.equal(state.accepts(explicitRepeat, 'reis'), true)
})

test('the common removal path preserves a local result cache without a reload', () => {
  const local = [
    result('A', 'account', 'inbox', 1, ['message-A']),
    result('B', 'account', 'inbox', 2, ['message-B']),
  ]
  const afterDelete = removeServerSearchResults(local, ['message-A'])
  assert.deepEqual(afterDelete.results.map(item => item.threadId), ['B'])
  assert.equal(afterDelete.removed, 1)
})
