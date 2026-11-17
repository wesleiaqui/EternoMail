import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadTypeScript(relativePath) {
  const source = await readFile(new URL(relativePath, import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const search = await loadTypeScript('../src/lib/components/list/serverSearchResults.ts')
const selection = await loadTypeScript('../src/lib/components/list/autoSelectNext.ts')
const undoEvents = await loadTypeScript('../src/lib/components/list/undoMutationEvents.ts')
const messageListSource = await readFile(
  new URL('../src/lib/components/list/MessageList.svelte', import.meta.url),
  'utf8',
)

const row = name => ({
  threadId: name,
  accountId: 'account',
  folderId: 'inbox',
  _uid: name.charCodeAt(0),
  messageIds: [`message-${name}`],
  unreadCount: 0,
})

function extractFunction(source, name) {
  const start = source.indexOf(`function ${name}(`)
  assert.notEqual(start, -1, `${name} was not found`)
  const bodyStart = source.indexOf('{', start)
  let depth = 0
  for (let index = bodyStart; index < source.length; index += 1) {
    if (source[index] === '{') depth += 1
    if (source[index] === '}') {
      depth -= 1
      if (depth === 0) return source.slice(start, index + 1)
    }
  }
  throw new Error(`${name} has no closing brace`)
}

test('Local Search Delete then Undo restores A B C without a search, reload, or viewer change', () => {
  const initial = ['A', 'B', 'C'].map(row)
  const snapshot = { result: initial[1], index: 1 }
  let visible = search.removeServerSearchResults(initial, ['message-B']).results
  let selectedThreadId = 'C'
  let searches = 0
  let folderLoads = 0

  visible = search.restoreSearchResults(visible, [snapshot]).results

  assert.deepEqual(visible.map(item => item.threadId), ['A', 'B', 'C'])
  assert.equal(selectedThreadId, 'C')
  assert.equal(searches, 0)
  assert.equal(folderLoads, 0)
})

test('Server Search Delete then Undo clears the tombstone and restores B locally', () => {
  const initial = ['A', 'B', 'C'].map(row)
  const b = initial[1]
  const state = new search.ServerSearchRequestState()
  const inFlight = state.begin('query')
  state.invalidate([search.serverSearchResultIdentity(b)])
  let visible = search.removeServerSearchResults(initial, ['message-B']).results
  assert.deepEqual(state.filter(initial).results.map(item => item.threadId), ['A', 'C'])
  assert.equal(state.accepts(inFlight, 'query'), false)

  state.restore([search.serverSearchResultIdentity(b)])
  visible = search.restoreSearchResults(visible, [{ result: b, index: 1 }]).results

  assert.deepEqual(visible.map(item => item.threadId), ['A', 'B', 'C'])
  assert.deepEqual(state.filter(initial).results.map(item => item.threadId), ['A', 'B', 'C'])
})

test('Done in Server Search removes B, selects C once, and keeps the server source error-free', () => {
  const initial = ['A', 'B', 'C'].map(row)
  const captured = selection.captureAutoSelectNext(initial, 'B', new Set())
  const visible = search.removeServerSearchResults(initial, ['message-B']).results
  const next = selection.findAutoSelectNext(visible, captured)
  let searchSource = 'server'
  let serverSearchError = null
  let normalFolderLoads = 0

  assert.deepEqual(visible.map(item => item.threadId), ['A', 'C'])
  assert.equal(next?.threadId, 'C')
  assert.equal(searchSource, 'server')
  assert.equal(serverSearchError, null)
  assert.equal(normalFolderLoads, 0)
})

test('global Ctrl+Z publishes the returned operation ID for snapshot restoration without changing the viewer', () => {
  const previousWindow = globalThis.window
  const previousCustomEvent = globalThis.CustomEvent
  class TestCustomEvent extends Event {
    constructor(type, init) {
      super(type)
      this.detail = init.detail
    }
  }
  const testWindow = new EventTarget()
  globalThis.window = testWindow
  globalThis.CustomEvent = TestCustomEvent

  try {
    const operationID = 'token-B'
    const snapshots = new Map([[operationID, { result: row('B'), index: 1 }]])
    let visible = ['A', 'C'].map(row)
    let selectedThreadId = 'C'
    let searches = 0
    testWindow.addEventListener(undoEvents.UNDO_OPERATION_COMPLETED_EVENT, (event) => {
      const snapshot = snapshots.get(event.detail)
      if (snapshot) visible = search.restoreSearchResults(visible, [snapshot]).results
    })

    // This is the additive result returned by UndoLatestWithResult to the
    // exact Ctrl+Z handler in App.svelte.
    const globalUndoResult = { operationId: operationID, description: 'Move to Trash' }
    undoEvents.publishUndoOperationCompleted(globalUndoResult.operationId)

    assert.deepEqual(visible.map(item => item.threadId), ['A', 'B', 'C'])
    assert.equal(selectedThreadId, 'C')
    assert.equal(searches, 0)
  } finally {
    globalThis.window = previousWindow
    globalThis.CustomEvent = previousCustomEvent
  }
})

test('search uses a full-width field and scoped workspace, with refresh and one explicit close', () => {
  const workspace = messageListSource.slice(messageListSource.indexOf('{#if showSearch}\n    <section'), messageListSource.indexOf('<!-- Header -->'))
  assert.match(workspace, /searchUI\.searchMessages/)
  assert.match(workspace, /class="w-full min-w-0/)
  assert.match(workspace, /searchUI\.widen/)
  assert.match(workspace, /onclick=\{refreshActiveSearch\}/)
  assert.equal((workspace.match(/onclick=\{clearSearch\}/g) || []).length, 1)
  assert.match(workspace, /limited=\{!allMailSearch\}/)
})

test('the single Search close control runs one reset and restores normal-list state', () => {
  const clearSearch = extractFunction(messageListSource, 'clearSearch')
  const toggleSearch = extractFunction(messageListSource, 'toggleSearch')
  assert.equal((toggleSearch.match(/clearSearch\(\)/g) ?? []).length, 1)

  const { outputText } = ts.transpileModule(`
    let allMailSearch = true, virtualIssues = '', virtualQueryCursor = ''
    const CancelMailSearch = () => Promise.resolve()
    const virtualSearchSession = 'test'
    let searchQuery = 'needle'
    let searchResults = ['local-result']
    let searchTotalCount = 1
    let searchOffset = 50
    let showSearch = true
    let searchSource = 'server'
    let localSearchGeneration = 4
    let isSearching = true
    let serverSearchResults = ['server-result']
    let serverSearchCount = 1
    let serverSearchTotalCount = 1
    let isServerSearching = true
    let serverResultFetchGeneration = 8
    let serverSearchSelectionLocked = true
    let searchDebounceTimer = null
    const onSearchQueryChange = () => {}
    let cancelCalls = 0
    const serverSearchRequestState = { cancel() { cancelCalls += 1 } }
    let searchInputRef = null
    ${clearSearch}
    ${toggleSearch}
    function state() {
      return {
        searchQuery, searchResults, searchTotalCount, searchOffset, showSearch,
        searchSource, localSearchGeneration, isSearching, serverSearchResults,
        serverSearchCount, serverSearchTotalCount, isServerSearching,
        serverResultFetchGeneration, serverSearchSelectionLocked, cancelCalls,
      }
    }
  `, {
    compilerOptions: { module: ts.ModuleKind.None, target: ts.ScriptTarget.ES2022 },
  })
  const createHarness = new Function('preferServerSearch', `${outputText}; return { toggleSearch, state }`)
  const harness = createHarness(false)

  harness.toggleSearch()
  assert.deepEqual(harness.state(), {
    searchQuery: '',
    searchResults: [],
    searchTotalCount: 0,
    searchOffset: 0,
    showSearch: false,
    searchSource: 'local',
    localSearchGeneration: 5,
    isSearching: false,
    serverSearchResults: [],
    serverSearchCount: 0,
    serverSearchTotalCount: 0,
    isServerSearching: false,
    serverResultFetchGeneration: 9,
    serverSearchSelectionLocked: false,
    cancelCalls: 1,
  })
  const gmail = createHarness(true)
  gmail.toggleSearch()
  assert.equal(gmail.state().searchSource, 'server', 'Gmail keeps server search as its default after closing search')
})
