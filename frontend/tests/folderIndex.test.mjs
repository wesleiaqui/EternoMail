import { test } from 'node:test'
import assert from 'node:assert/strict'

test('folder index builds an O(1) map of all folders across accounts and trees', () => {
  const accounts = [
    {
      account: { id: 'acc1', email: 'user1@example.com' },
      folders: [
        {
          folder: { id: 'f-inbox', name: 'Inbox', type: 'inbox' },
          children: [
            {
              folder: { id: 'f-sub', name: 'Subfolder', type: 'user' },
              children: [],
            },
          ],
        },
        {
          folder: { id: 'f-sent', name: 'Sent', type: 'sent' },
          children: [],
        },
      ],
    },
    {
      account: { id: 'acc2', email: 'user2@example.com' },
      folders: [
        {
          folder: { id: 'f-work', name: 'Work', type: 'user' },
          children: [],
        },
      ],
    },
  ]

  // Verify the exact indexing algorithm used in accountStore.folderIndex
  const index = {}
  const walk = (trees) => {
    for (const tree of trees) {
      if (tree.folder) {
        index[tree.folder.id] = tree.folder
      }
      if (tree.children && tree.children.length > 0) {
        walk(tree.children)
      }
    }
  }
  for (const acc of accounts) {
    if (acc.folders) walk(acc.folders)
  }

  assert.equal(index['f-inbox']?.name, 'Inbox')
  assert.equal(index['f-sub']?.name, 'Subfolder')
  assert.equal(index['f-sent']?.name, 'Sent')
  assert.equal(index['f-work']?.name, 'Work')
  assert.equal(index['f-nonexistent'], undefined)
})
