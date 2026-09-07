import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

test('late photo responses cannot repopulate an invalidated cache', async () => {
  let source = await readFile(new URL('../src/lib/stores/contactPhotos.svelte.ts', import.meta.url), 'utf8')
  source = source.replace(/^import .*$/gm, '')
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
  const requests = []
  const factory = new Function('$state', 'GetContactPhotos', 'GetAccountProfilePhotos', 'EventsOn', 'isConfiguredAccountEmail', outputText.replace('export const contactPhotos', 'const contactPhotos') + '\nreturn contactPhotos')
  const photos = factory(x => x, () => new Promise(resolve => requests.push(resolve)), async () => [], () => {}, () => false)
  const old = photos.ensure(['user@example.com'])
  photos.invalidate()
  assert.equal(requests.length, 2)
  requests[1]([{ email: 'user@example.com', data: 'new', mediaType: 'image/png' }])
  await new Promise(resolve => setImmediate(resolve))
  requests[0]([{ email: 'user@example.com', data: 'old', mediaType: 'image/png' }])
  await old
  assert.equal(photos.get('user@example.com').data, 'new')
})
