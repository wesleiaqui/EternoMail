import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function loadTS(path) {
  const source = await readFile(new URL(path, import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}
const { linkifyText, isAllowedEmailURL } = await loadTS('../src/lib/utils/emailLinks.ts')
const cache = await loadTS('../src/lib/stores/inlineAttachmentCache.ts')

test('linkification escapes attributes and does not create nested links', () => {
  const result = linkifyText('https://example.com/user@example.com?q=" onmouseover="attack <script>x</script>')
  assert.equal((result.match(/<a /g) || []).length, 1)
  assert.ok(!result.includes('<script>'))
  assert.ok(!result.includes(' onmouseover="'))
  assert.ok(result.includes('&quot;'))
})
test('email URL schemes cannot launch local files or scripts', () => {
  for (const url of ['javascript:alert(1)', 'file:///etc/passwd', 'data:text/html,x', 'custom:exec']) assert.equal(isAllowedEmailURL(url), false)
  for (const url of ['https://example.com', 'http://example.com', 'mailto:user@example.com']) assert.equal(isAllowedEmailURL(url), true)
})
test('clearing attachments invalidates outstanding requests', () => {
  cache.setCache('old-message', { cid: 'secret-image' })
  const before = cache.getCacheGeneration()
  cache.clearCache()
  assert.equal(cache.getCached('old-message'), null)
  assert.notEqual(cache.getCacheGeneration(), before)
})

test('ordinary URLs, query ampersands, Unicode and email text remain usable', () => {
  const result = linkifyText('Olá https://example.test/ação?a=1&b=2 user@example.test < & " \'')
  assert.ok(result.includes('href="https://example.test/ação?a=1&amp;b=2"'))
  assert.ok(result.includes('href="mailto:user@example.test"'))
  assert.ok(result.endsWith('&lt; &amp; &quot; &#39;'))
  assert.equal((result.match(/<a /g) || []).length, 2)
})
