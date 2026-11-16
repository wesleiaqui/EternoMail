import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

test('viewer AttachmentList action buttons have accessible names and focus-visible states', () => {
  const filePath = path.resolve(__dirname, '../src/lib/components/viewer/AttachmentList.svelte')
  const content = fs.readFileSync(filePath, 'utf-8')

  assert.equal((content.match(/aria-label=\{`\$\{\$_\('attachment\.open'\)\}: \$\{item\.attachment\.filename\}`\}/g) || []).length, 2, 'chip and expanded Open name their exact file')
  assert.equal((content.match(/aria-label=\{`\$\{\$_\('attachment\.download'\)\}: \$\{item\.attachment\.filename\}`\}/g) || []).length, 2, 'chip and expanded Download name their exact file')
  assert.ok(content.includes('aria-expanded={expanded}'))
  assert.ok(content.includes('focus-visible:ring-2 focus-visible:ring-ring'))
})

test('composer ComposerAttachmentList remove button has accessible name and focus-visible state', () => {
  const filePath = path.resolve(__dirname, '../src/lib/components/composer/ComposerAttachmentList.svelte')
  const content = fs.readFileSync(filePath, 'utf-8')

  // Remove button has aria-label
  assert.ok(
    content.includes("aria-label={$_('attachment.removeAttachment')}"),
    'remove button should have aria-label'
  )

  // Focus-visible styling
  assert.ok(
    content.includes('focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'),
    'remove button should have focus-visible styling'
  )
})
