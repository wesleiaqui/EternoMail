import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

const treeSource = await readFile(
  new URL('../src/lib/components/sidebar/FolderTreeItem.svelte', import.meta.url),
  'utf8',
)
const menuSource = await readFile(
  new URL('../src/lib/components/sidebar/FolderContextMenu.svelte', import.meta.url),
  'utf8',
)

function extractFunction(source, name) {
  const asyncMarker = `async function ${name}(`
  const functionMarker = `function ${name}(`
  const start = source.includes(asyncMarker)
    ? source.indexOf(asyncMarker)
    : source.indexOf(functionMarker)
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

function transpile(source) {
  return ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.None, target: ts.ScriptTarget.ES2022 },
  }).outputText
}

function createUndoHarness() {
  const functions = [
    extractFunction(treeSource, 'showTargetedUndoToast'),
    extractFunction(treeSource, 'handleTargetedUndo'),
  ].join('\n')
  const successes = []
  const replacements = []
  const undoCalls = []
  const completed = []
  let resolveUndo
  let deferredUndo = null
  const toasts = {
    success: (message, actions) => {
      successes.push({ message, actions })
      return `toast-${successes.length}`
    },
    replace: (id, patch) => {
      replacements.push({ id, patch })
      return true
    },
  }
  const undoOperation = async operationID => {
    undoCalls.push(operationID)
    if (deferredUndo) await deferredUndo
    return `description-${operationID}`
  }
  const factory = new Function(
    'toasts',
    'UndoOperation',
    'publishUndoOperationCreated',
    'publishUndoOperationCompleted',
    '$_',
    `${transpile(`const undoInFlight = new Set<string>()\n${functions}`)}\n` +
      'return { showTargetedUndoToast, handleTargetedUndo }',
  )
  const handlers = factory(
    toasts,
    undoOperation,
    () => {},
    operationID => completed.push(operationID),
    (key, options) => options?.values?.description
      ? `${key}:${options.values.description}`
      : key,
  )

  return {
    ...handlers,
    successes,
    replacements,
    undoCalls,
    completed,
    defer() {
      deferredUndo = new Promise(resolve => { resolveUndo = resolve })
    },
    resolve() {
      resolveUndo?.()
    },
  }
}

test('FolderTreeItem drag-drop owns one targeted MoveToFolder mutation and toast', () => {
  const drop = extractFunction(treeSource, 'handleDrop')
  assert.match(drop, /runMessageMutation\(payload\.messageIds,/)
  assert.match(drop, /await MoveToFolderWithUndo\(payload\.messageIds, tree\.folder!\.id\)/)
  assert.match(drop, /showTargetedUndoToast\([\s\S]*?result\.operationId,[\s\S]*?payload\.messageIds/)
  assert.equal((drop.match(/showTargetedUndoToast\(/g) ?? []).length, 1)
  assert.doesNotMatch(treeSource, /GetUndoOperationForMessages/)
  assert.doesNotMatch(treeSource, /\bUndo\s*\(/)
  assert.doesNotMatch(treeSource, /\bMoveToFolder\s*\(/)
})

test('FolderTreeItem forwards the mutation token without a later lookup', async () => {
  const calls = { guard: [], mutation: [], toast: [], moved: 0 }
  const factory = new Function(
    'runMessageMutation',
    'MoveToFolderWithUndo',
    'showTargetedUndoToast',
    'onMessagesMoved',
    'toasts',
    '$_',
    'console',
    `${transpile(`
      let isDragOver = false
      const tree = { folder: { id: 'folder-destination', name: 'Destination' } }
      const selectedFolderId = 'folder-source'
      const selectionSource = 'account'
      ${extractFunction(treeSource, 'handleDrop')}
    `)}\nreturn handleDrop`,
  )
  const handleDrop = factory(
    async (messageIds, mutation) => {
      calls.guard.push(messageIds)
      return { started: true, value: await mutation() }
    },
    async (messageIds, folderId) => {
      calls.mutation.push({ messageIds, folderId })
      return { operationId: 'operation-from-mutation', coalesced: false }
    },
    (...args) => calls.toast.push(args),
    () => { calls.moved += 1 },
    { error: () => {} },
    () => 'Moved',
    { error: () => {} },
  )
  await handleDrop({
    preventDefault() {},
    dataTransfer: {
      getData: () => JSON.stringify({
        messageIds: ['message-A', 'message-B'],
        sourceAccountId: 'account-A',
      }),
    },
  })

  assert.deepEqual(calls.guard, [['message-A', 'message-B']])
  assert.deepEqual(calls.mutation, [{
    messageIds: ['message-A', 'message-B'],
    folderId: 'folder-destination',
  }])
  assert.equal(calls.moved, 1)
  assert.equal(calls.toast.length, 1)
  assert.equal(calls.toast[0][1], 'operation-from-mutation')
  assert.deepEqual(calls.toast[0][2], ['message-A', 'message-B'])
})

test('FolderTreeItem Undo double-click calls UndoOperation once and replaces the same toast', async () => {
  const harness = createUndoHarness()
  harness.defer()
  harness.showTargetedUndoToast('Moved', 'operation-A', ['message-A'], 'move-to-folder')
  const undoAction = harness.successes[0].actions[0]

  const first = undoAction.onClick()
  const second = undoAction.onClick()
  await Promise.resolve(second)

  assert.deepEqual(harness.undoCalls, ['operation-A'])
  assert.equal(harness.replacements[0].id, 'toast-1')
  assert.equal(harness.replacements[0].patch.message, 'Undoing...')
  assert.deepEqual(harness.replacements[0].patch.actions, [])

  harness.resolve()
  await first
  await new Promise(resolve => setImmediate(resolve))
  assert.deepEqual(harness.completed, ['operation-A'])
  assert.equal(harness.replacements.at(-1).id, 'toast-1')
  assert.deepEqual(harness.replacements.at(-1).patch.actions, [])
})

test('FolderTreeItem targeted Undo keeps independent drag-drop tokens isolated', async () => {
  const harness = createUndoHarness()
  await harness.handleTargetedUndo('toast-A', 'operation-A')
  await harness.handleTargetedUndo('toast-B', 'operation-B')

  assert.deepEqual(harness.undoCalls, ['operation-A', 'operation-B'])
  assert.deepEqual(harness.completed, ['operation-A', 'operation-B'])
  assert.equal(harness.replacements[0].id, 'toast-A')
  assert.equal(harness.replacements[2].id, 'toast-B')
})

test('FolderContextMenu read-state actions stay non-undoable and callbacks do not add a second toast', () => {
  assert.match(menuSource, /await MarkAllFolderMessagesAsRead\(folderId\)/)
  assert.match(menuSource, /await MarkAllFolderMessagesAsUnread\(folderId\)/)
  assert.doesNotMatch(menuSource, /GetUndoOperationForMessages/)
  assert.doesNotMatch(menuSource, /\bUndo\s*\(/)
  assert.doesNotMatch(menuSource, /UndoOperation/)
  assert.doesNotMatch(menuSource, /common\.undo/)
  assert.match(treeSource, /<FolderContextMenu folderId=\{tree\.folder\.id\}>/)
  assert.equal((extractFunction(treeSource, 'handleDrop').match(/onMessagesMoved\?\.\(\)/g) ?? []).length, 1)
})
