// While the Kaikki kansiot panel is open, the four quick-filter buttons at the
// top of the column (all / unread / read later / reply later) prune the FOLDER
// list instead of the message list. Unread uses the live per-folder unread
// counts; Read Later and Reply Later use per-folder membership counts from the
// new GetQueueFolderCounts backend endpoint, because queue rows carry only
// account+conversation refs, not folder ids.

import { test } from 'node:test'
import assert from 'node:assert/strict'
import ts from 'typescript'
import { readFile } from 'node:fs/promises'

async function load(relative) {
  const source = await readFile(new URL(relative, import.meta.url), 'utf8')
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  })
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
}

const read = path => readFile(new URL(path, import.meta.url), 'utf8')
const { folderMatches, pruneFolderTree } = await load('../src/lib/sidebar/folderListFilter.ts')

const queueCounts = {
  readLater: { a1: { f1: 2, f3: 1 } },
  replyLater: { a1: { f2: 4 } },
}

test('no filter keeps every folder', () => {
  assert.equal(folderMatches({ id: 'f1' }, 'a1', '', queueCounts), true)
  assert.equal(folderMatches({ id: 'nope' }, 'a1', '', queueCounts), true)
})

test('unread matches folders with a positive unreadCount', () => {
  assert.equal(folderMatches({ id: 'f1', unreadCount: 3 }, 'a1', 'unread', queueCounts), true)
  assert.equal(folderMatches({ id: 'f1', unreadCount: 0 }, 'a1', 'unread', queueCounts), false)
  assert.equal(folderMatches({ id: 'f1' }, 'a1', 'unread', queueCounts), false)
})

test('readLater/replyLater match on the per-queue per-folder counts', () => {
  assert.equal(folderMatches({ id: 'f1' }, 'a1', 'readLater', queueCounts), true)
  assert.equal(folderMatches({ id: 'f3' }, 'a1', 'readLater', queueCounts), true)
  assert.equal(folderMatches({ id: 'f2' }, 'a1', 'readLater', queueCounts), true)
  assert.equal(folderMatches({ id: 'f2' }, 'a1', 'replyLater', queueCounts), true)
  assert.equal(folderMatches({ id: 'f1' }, 'a1', 'replyLater', queueCounts), true)
  // Counts are scoped by account.
  assert.equal(folderMatches({ id: 'f1' }, 'a2', 'readLater', queueCounts), false)
})

const tree = [
  {
    folder: { id: 'root', unreadCount: 0 },
    children: [
      { folder: { id: 'f1', unreadCount: 0 }, children: [] },
      {
        folder: { id: 'mid', unreadCount: 0 },
        children: [{ folder: { id: 'f3', unreadCount: 0 }, children: [] }],
      },
    ],
  },
  { folder: { id: 'f2', unreadCount: 0 }, children: [] },
]

test('pruning keeps matches and the ancestors they hang from', () => {
  const { trees, contextIds } = pruneFolderTree(tree, 'a1', 'readLater', queueCounts)
  // f1 kept directly; mid kept only as an ancestor of f3; f2 dropped.
  assert.deepEqual(
    trees.map(n => n.folder.id),
    ['root', 'f2']
  )
  assert.deepEqual(
    trees[0].children.map(n => n.folder.id),
    ['f1', 'mid']
  )
  assert.equal(trees[0].children[1].children[0].folder.id, 'f3')
  assert.deepEqual([...contextIds].sort(), ['mid', 'root'])
})

test('pruning with an empty filter returns the input unchanged', () => {
  const { trees, contextIds } = pruneFolderTree(tree, 'a1', '', queueCounts)
  assert.equal(trees, tree)
  assert.equal(contextIds.size, 0)
})

test('pruning does not mutate the input tree', () => {
  const before = JSON.stringify(tree)
  pruneFolderTree(tree, 'a1', 'readLater', queueCounts)
  assert.equal(JSON.stringify(tree), before)
})

test('the sidebar header buttons drive the folder-list filter while the list-pane row still drives the message list', async () => {
  const app = await read('../src/App.svelte')
  // The triage button row markup is identical in both places; the sidebar
  // header row comes first and is re-wired through triageButtonClick, the
  // list-pane row keeps selectTriageFilter.
  const headerRow = app.indexOf("triageButtonClick('')")
  const listRow = app.indexOf("selectTriageFilter('')")
  assert.ok(headerRow > 0, 'sidebar header row uses triageButtonClick')
  assert.ok(listRow > headerRow, 'list-pane row still uses selectTriageFilter')
  assert.match(app, /folderListFilter/)
  assert.match(app, /listFilter={contextPaneMode === 'folders' \? folderListFilter : ''}/)
})

test('the folder panel prunes with the active filter and loads queue counts on demand', async () => {
  const sidebar = await read('../src/lib/components/sidebar/Sidebar.svelte')
  assert.match(sidebar, /pruneFolderTree/)
  assert.match(sidebar, /folderQueueCounts\.acquire/)
  const store = await read('../src/lib/stores/folderQueueCounts.svelte.ts')
  assert.match(store, /GetQueueFolderCounts/)
  assert.match(store, /readLater:changed/)
  assert.match(store, /replyLater:changed/)
  // Queued mail that moves folders or is deleted must refresh the counts.
  assert.match(store, /messages:moved/)
  assert.match(store, /messages:deleted/)
  // Closed filters never rescan: reloads need an active acquire() holder.
  assert.match(store, /if \(activeConsumers > 0\)/)
})

test('ancestors of nested matches stay expanded while the filter is active', async () => {
  const item = await read('../src/lib/components/sidebar/FolderTreeItem.svelte')
  const collapsed = item.slice(item.indexOf('let isCollapsed'), item.indexOf('let isCollapsed') + 260)
  assert.match(collapsed, /contextOnly \? false/)
})

test('the queue-counts endpoint is wired end to end', async () => {
  const appjs = await read('../wailsjs/go/app/App.js')
  const appdts = await read('../wailsjs/go/app/App.d.ts')
  const models = await read('../wailsjs/go/models.ts')
  assert.match(appjs, /GetQueueFolderCounts/)
  assert.match(appdts, /GetQueueFolderCounts/)
  assert.match(models, /QueueFolderCounts/)
  const go = await read('../../app/queue_folder_counts.go')
  assert.match(go, /func \(a \*App\) GetQueueFolderCounts/)
})
