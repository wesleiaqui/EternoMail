import type { folder, virtualfolder, mailfilter } from '../../../wailsjs/go/models'

export function descendantFolderIds(all: folder.Folder[], selected: string[]): string[] {
  const seen = new Set(selected)
  for (let changed = true; changed;) {
    changed = false
    for (const item of all) if (item.parentId && seen.has(item.parentId) && !seen.has(item.id)) { seen.add(item.id); changed = true }
  }
  return [...seen].filter(id => !selected.includes(id))
}

export function folderDependencies(ids: string[], views: virtualfolder.VirtualFolder[], rules: mailfilter.Rule[]) {
  const selected = new Set(ids)
  return {
    views: views.filter(view => view.folderMode === 'exclude' ? ids.some(id => !view.folderIds.includes(id)) : view.folderIds.some(id => selected.has(id))).map(view => view.name),
    rules: rules.filter(rule => rule.enabled && selected.has(rule.destFolderId)).map(rule => rule.name),
  }
}
