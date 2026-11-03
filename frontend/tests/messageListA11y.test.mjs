import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

test('message list header action buttons have accessible names (aria-label)', () => {
  const filePath = path.resolve(__dirname, '../src/lib/components/list/MessageList.svelte')
  const content = fs.readFileSync(filePath, 'utf-8')

  // cancelFolderSync button has aria-label
  assert.ok(
    content.includes('aria-label={syncProgress ? `${$_(\'sidebar.syncing\')} ${syncProgress.phase}: ${syncProgress.percentage}% - ${$_(\'sidebar.clickToCancel\')}` : `${$_(\'sidebar.syncing\')} ${$_(\'sidebar.clickToCancel\')}`}'),
    'cancelFolderSync button should have descriptive aria-label'
  )

  // sync options dropdown trigger has aria-label and title
  assert.ok(
    content.includes('aria-label={$_(\'messageList.syncFolder\')}'),
    'sync options dropdown trigger should have aria-label'
  )

  // filter options dropdown trigger has aria-label
  assert.ok(
    content.includes('aria-label={filterMode ? `${$_(\'messageList.filter\')}: ${filterLabel}` : $_(\'messageList.filter\')}'),
    'filter options dropdown trigger should have aria-label'
  )

  // toggleSortOrder button has aria-label
  assert.ok(
    content.includes('aria-label={getMessageListSortOrder() === \'newest\' ? $_(\'messageList.showingNewest\') : $_(\'messageList.showingOldest\')}'),
    'toggleSortOrder button should have dynamic aria-label'
  )
})
