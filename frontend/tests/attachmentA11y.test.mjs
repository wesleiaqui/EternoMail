import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

test('viewer AttachmentList action buttons have accessible names and focus-visible states', () => {
  const filePath = path.resolve(__dirname, '../src/lib/components/viewer/AttachmentList.svelte')
  const content = fs.readFileSync(filePath, 'utf-8')

  // Open and download buttons in both loops have aria-label
  const openAriaCount = (content.match(/aria-label=\{\$_?\('attachment\.open'\)\}/g) || []).length
  assert.equal(openAriaCount, 2, 'both encrypted and standard open buttons should have aria-label')

  const downloadAriaCount = (content.match(/aria-label=\{\$_?\('attachment\.download'\)\}/g) || []).length
  assert.equal(downloadAriaCount, 2, 'both encrypted and standard download buttons should have aria-label')

  // Focus-visible ring and opacity-100 on hover-hidden buttons
  const focusVisibleCount = (content.match(/focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/g) || []).length
  assert.equal(focusVisibleCount, 4, 'all 4 open/download attachment buttons should have focus-visible styling')
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
