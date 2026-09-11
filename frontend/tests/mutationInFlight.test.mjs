import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadGuard() {
  const source = await readFile(new URL('../src/lib/components/list/mutationInFlight.ts', import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

test('rapid Done calls for the same local ID call RemoveFromInboxWithUndo once', async () => {
  const { runMessageMutation } = await loadGuard()
  const gate = deferred()
  let backendCalls = 0
  const removeFromInboxWithUndo = async () => {
    backendCalls += 1
    await gate.promise
    return { operationId: 'done-operation' }
  }

  const first = runMessageMutation(['A'], removeFromInboxWithUndo)
  const second = await runMessageMutation(['A'], removeFromInboxWithUndo)
  assert.deepEqual(second, { started: false })
  assert.equal(backendCalls, 1)

  gate.resolve()
  assert.equal((await first).started, true)
})

test('rapid Delete calls for the same local ID call TrashWithUndo once', async () => {
  const { runMessageMutation } = await loadGuard()
  const gate = deferred()
  let backendCalls = 0
  const trashWithUndo = async () => {
    backendCalls += 1
    await gate.promise
    return { operationId: 'trash-operation' }
  }

  const first = runMessageMutation(['A'], trashWithUndo)
  const second = await runMessageMutation(['A'], trashWithUndo)
  assert.deepEqual(second, { started: false })
  assert.equal(backendCalls, 1)

  gate.resolve()
  await first
})

test('Done and Delete cannot overlap for one ID, and settling permits a later action', async () => {
  const { runMessageMutation } = await loadGuard()
  const gate = deferred()
  let doneCalls = 0
  let trashCalls = 0

  const first = runMessageMutation(['A'], async () => {
    doneCalls += 1
    await gate.promise
  })
  const competing = await runMessageMutation(['A'], async () => { trashCalls += 1 })
  assert.equal(competing.started, false)
  assert.equal(doneCalls, 1)
  assert.equal(trashCalls, 0)

  gate.resolve()
  await first
  const later = await runMessageMutation(['A'], async () => { trashCalls += 1 })
  assert.equal(later.started, true)
  assert.equal(trashCalls, 1)
})

test('an overlapping batch is rejected as a whole', async () => {
  const { runMessageMutation } = await loadGuard()
  const gate = deferred()
  let overlappingBatchCalls = 0

  const first = runMessageMutation(['A'], async () => { await gate.promise })
  const overlapping = await runMessageMutation(['A', 'B'], async () => { overlappingBatchCalls += 1 })
  assert.equal(overlapping.started, false)
  assert.equal(overlappingBatchCalls, 0)

  gate.resolve()
  await first
})
