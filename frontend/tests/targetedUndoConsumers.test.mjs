import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

const menuSource = await readFile(
  new URL('../src/lib/components/common/MessageContextMenu.svelte', import.meta.url),
  'utf8',
)
const appSource = await readFile(new URL('../src/App.svelte', import.meta.url), 'utf8')
const messageListSource = await readFile(
  new URL('../src/lib/components/list/MessageList.svelte', import.meta.url),
  'utf8',
)
const rowSource = await readFile(
  new URL('../src/lib/components/list/ConversationRow.svelte', import.meta.url),
  'utf8',
)
const viewerSource = await readFile(
  new URL('../src/lib/components/viewer/ConversationViewer.svelte', import.meta.url),
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

function createUndoHarness(source, setName) {
  const functionSource = extractFunction(source, 'handleTargetedUndo')
  const { outputText } = ts.transpileModule(
    `const ${setName} = new Set<string>()\n${functionSource}`,
    { compilerOptions: { module: ts.ModuleKind.None, target: ts.ScriptTarget.ES2022 } },
  )
  const replacements = []
  const undoCalls = []
  const completed = []
  let resolveUndo
  let deferredUndo = null
  const dependencies = {
    toasts: {
      replace: (id, patch) => {
        replacements.push({ id, patch })
        return true
      },
    },
    UndoOperation: async operationID => {
      undoCalls.push(operationID)
      if (deferredUndo) await deferredUndo
      return `description-${operationID}`
    },
    publishUndoOperationCompleted: operationID => completed.push(operationID),
    translate: (key, options) => options?.values?.description
      ? `${key}:${options.values.description}`
      : key,
  }
  const handler = new Function(
    'toasts',
    'UndoOperation',
    'publishUndoOperationCompleted',
    '$_',
    `${outputText}\nreturn handleTargetedUndo`,
  )(
    dependencies.toasts,
    dependencies.UndoOperation,
    dependencies.publishUndoOperationCompleted,
    dependencies.translate,
  )

  return {
    handler,
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

test('MessageContextMenu uses targeted mutation APIs and no global/lookup Undo path', () => {
  for (const api of [
    'ArchiveWithUndo',
    'TrashWithUndo',
    'MoveToFolderWithUndo',
    'MoveToInboxWithUndo',
    'MarkAsSpamWithUndo',
    'MarkAsNotSpamWithUndo',
  ]) {
    assert.match(menuSource, new RegExp(`await ${api}\\(`), `${api} is not wired`)
  }
  for (const legacy of ['Archive', 'Trash', 'MoveToFolder', 'MoveToInbox', 'MarkAsSpam', 'MarkAsNotSpam']) {
    assert.doesNotMatch(menuSource, new RegExp(`\\b${legacy}\\(`), `${legacy} remains in a specific UI flow`)
  }
  assert.doesNotMatch(menuSource, /GetUndoOperationForMessages/)
  assert.doesNotMatch(menuSource, /\bUndo\s*\(/)
  assert.match(menuSource, /await UndoOperation\(operationID\)/)
  assert.match(menuSource, /showTargetedUndoToast\([^;]*result\.operationId/s)
})

test('every MessageContextMenu message mutation uses the shared in-flight guard', () => {
  for (const handler of [
    'handleMoveToInbox',
    'handleArchive',
    'handleDelete',
    'handleConfirmPermanentDelete',
    'handleSpam',
    'handleToggleStar',
    'handleToggleRead',
    'handleMoveTo',
    'handleCopyTo',
  ]) {
    assert.match(extractFunction(menuSource, handler), /runMessageMutation\(messageIds,/)
  }
})

test('App specific Archive/Spam toasts use returned tokens while Ctrl+Z remains global LIFO', () => {
  assert.match(appSource, /const result = await ArchiveWithUndo\(messageIds\)/)
  assert.match(appSource, /const result = await MarkAsSpamWithUndo\(messageIds\)/)
  assert.match(appSource, /const result = await MarkAsNotSpamWithUndo\(messageIds\)/)
  assert.doesNotMatch(appSource, /GetUndoOperationForMessages/)
  assert.doesNotMatch(appSource, /\bUndo\s*\(/)

  const globalUndo = extractFunction(appSource, 'handleGlobalUndo')
  assert.match(globalUndo, /UndoLatestWithResult\(\)/)
  assert.doesNotMatch(globalUndo, /UndoOperation\(/)
  assert.match(appSource, /case 'z':[\s\S]*?handleGlobalUndo\(\)/)
})

test('App-owned message mutations share one guard without nesting the MessageList callback', () => {
  for (const handler of [
    'handleBulkArchive',
    'handleBulkSpam',
    'handleBulkMarkRead',
    'handleUndoLastFlag',
    'handleBulkMarkUnread',
    'handleBulkToggleStar',
  ]) {
    assert.match(extractFunction(appSource, handler), /runMessageMutation\(messageIds,/)
  }

  const listArchive = extractFunction(messageListSource, 'handleBulkArchive')
  assert.match(listArchive, /await onBulkArchive\?\.\(messageIds\)/)
  assert.doesNotMatch(listArchive, /runMessageMutation\(/)
})

test('remaining explicit row, list, and viewer mutations use the shared guard', () => {
  assert.match(extractFunction(rowSource, 'handleQuickToggleRead'), /runMessageMutation\(ownMessageIds,/)
  assert.match(extractFunction(messageListSource, 'handleConfirmPermanentDelete'), /runMessageMutation\(messageIds,/)
  assert.match(extractFunction(viewerSource, 'handleStar'), /runMessageMutation\(messageIds,/)
  assert.match(extractFunction(viewerSource, 'handleMarkRead'), /runMessageMutation\(messageIds,/)

  const readOnLeave = extractFunction(viewerSource, 'finalizeReadOnLeave')
  assert.doesNotMatch(readOnLeave, /runMessageMutation\(/)
  assert.match(readOnLeave, /take\(\) is single-consumer/)
})

for (const [owner, source, setName] of [
  ['MessageContextMenu', menuSource, 'undoInFlight'],
  ['App', appSource, 'targetedUndoInFlight'],
]) {
  test(`${owner} double-click invokes UndoOperation once and replaces the same toast`, async () => {
    const harness = createUndoHarness(source, setName)
    harness.defer()

    const first = harness.handler('toast-A', 'token-A')
    const second = harness.handler('toast-A', 'token-A')
    await second

    assert.deepEqual(harness.undoCalls, ['token-A'])
    assert.equal(harness.replacements[0].id, 'toast-A')
    assert.deepEqual(harness.replacements[0].patch.actions, [])
    assert.equal(harness.replacements[0].patch.message, 'Undoing...')

    harness.resolve()
    await first
    assert.deepEqual(harness.completed, ['token-A'])
    assert.equal(harness.replacements.at(-1).id, 'toast-A')
    assert.deepEqual(harness.replacements.at(-1).patch.actions, [])
  })

  test(`${owner} targeted Undo keeps Archive/Spam and Trash/Move tokens isolated`, async () => {
    for (const tokens of [['archive-A', 'spam-B'], ['trash-A', 'move-B']]) {
      const harness = createUndoHarness(source, setName)
      await harness.handler(`toast-${tokens[0]}`, tokens[0])
      await harness.handler(`toast-${tokens[1]}`, tokens[1])
      assert.deepEqual(harness.undoCalls, tokens)
      assert.deepEqual(harness.completed, tokens)
      assert.equal(harness.replacements[0].id, `toast-${tokens[0]}`)
      assert.equal(harness.replacements[2].id, `toast-${tokens[1]}`)
    }
  })
}
