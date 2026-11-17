<script lang="ts">
  let { onSearch = () => {}, onAddVirtualFolder = () => {}, onRevealContext = () => {}, onHideContext = () => {}, foldersOpen = false, windowControls = false, onCloseWindow }: { onSearch?: () => void; onAddVirtualFolder?: () => void; onRevealContext?: () => void; onHideContext?: () => void; foldersOpen?: boolean; windowControls?: boolean; onCloseWindow?: () => void } = $props()
  import RailButton from './RailButton.svelte'
  import WindowControls from '$lib/components/common/WindowControls.svelte'
  import { toggleMaximise } from '$lib/windowState.svelte'
  import { getRailTabs } from '$lib/stores/extensionRegistry.svelte'
  import { getActiveExtension, setActiveExtension } from '$lib/stores/uiState.svelte'
  import { navigationCards as nav } from '$lib/stores/navigationCards.svelte'
  import { shortcutIndex } from '$lib/sidebar/cards'
  import { _ } from '$lib/i18n'
  import { executeCommand } from '$lib/stores/commandPalette.svelte'
  import { withShortcut } from '$lib/keyboard/format'
  let draggedQuickId = $state<string | null>(null)
  const mac = /Mac|iPhone|iPad/.test(navigator.platform)
  const systemIcons: Record<string, string> = {
    inbox: 'mdi:inbox-outline', starred: 'mdi:pin-outline', drafts: 'mdi:file-outline',
    sent: 'mdi:send-outline', trash: 'mdi:delete-outline', archive: 'mdi:archive-outline',
    replyLater: 'mdi:clock-outline', spam: 'mdi:alert-octagon-outline', snoozed: 'mdi:clock-outline', all: 'mdi:email-multiple-outline',
  }
  function quickIcon(entry: { kind: string; folderType?: string }): string | null {
    if (entry.kind === 'inboxes') return 'mdi:inbox-multiple-outline'
    if (entry.kind === 'system') return systemIcons[entry.folderType || ''] || 'mdi:folder-outline'
    return null
  }
  function favoriteInitial(name: string): string {
    const words = name.trim().split(/\s+/).filter(Boolean)
    const letters = words.length > 1 ? words.slice(0, 2).map(word => Array.from(word)[0]) : Array.from(words[0] || '')
    return letters.join('').toLocaleUpperCase().slice(0, 2)
  }
  function favoriteColor(id: string): string {
    let hash = 0
    for (const character of id) hash = (hash * 31 + character.charCodeAt(0)) | 0
    return `hsl(${Math.abs(hash) % 360} 54% 40%)`
  }
  function keydown(event: KeyboardEvent) {
    if (event.defaultPrevented || document.querySelector('[role="dialog"], [role="alertdialog"]')) return
    const target = event.target as HTMLElement | null
    if (target instanceof Element && target.closest('input, textarea, select, [contenteditable="true"]')) return
    const index = shortcutIndex(event, mac), entry = nav.quick[index]
    if (entry) { event.preventDefault(); event.stopPropagation(); nav.navigate(entry) }
  }
  function startQuickDrag(id: string, event: DragEvent) {
    draggedQuickId = id
    event.dataTransfer?.setData('text/plain', id)
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
  }
  function dropQuick(id: string, event: DragEvent) {
    event.preventDefault()
    const moving = event.dataTransfer?.getData('text/plain') || draggedQuickId
    if (!moving || moving === id) return
    const bounds = (event.currentTarget as HTMLElement).getBoundingClientRect()
    nav.moveQuick(moving, id, event.clientY >= bounds.top + bounds.height / 2)
    draggedQuickId = null
  }
</script>
<svelte:window onkeydown={keydown} />
<nav class="flex flex-col items-stretch {windowControls ? 'w-20' : 'w-12 pt-2'} flex-shrink-0 bg-muted/30 border-r border-border" aria-label={$_('navigation.quickAccess')} onmouseenter={onRevealContext} onmouseleave={onHideContext}>
  {#if windowControls}
    <!-- One bar: the traffic lights live in the top-left corner of the rail, with a drag region around them. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="flex h-10 shrink-0 items-center px-3 select-none" style="--wails-draggable: drag" ondblclick={() => void toggleMaximise()}>
      <WindowControls onClose={onCloseWindow} />
    </div>
  {/if}
  <div data-search-anchor><RailButton icon="mdi:magnify" label="Search all mail" active={false} onclick={onSearch} /></div>
  <RailButton icon="mdi:email" label="Mail" active={getActiveExtension() === 'mail'} onclick={() => setActiveExtension('mail')} />
  <RailButton icon="mdi:folder-multiple-outline" label={withShortcut($_('shortcuts.action.folders'), 'app.folders')} active={foldersOpen} onclick={() => void executeCommand('app.folders')} />
  {#each nav.quick as entry, index (entry.id)}
    <RailButton
      icon={quickIcon(entry)}
      initial={entry.kind === 'folder' || entry.kind === 'virtual' ? favoriteInitial(entry.name) : undefined}
      color={entry.kind === 'folder' || entry.kind === 'virtual' ? favoriteColor(entry.id) : undefined}
      label={`${entry.name} (${mac ? '⌘' : 'Ctrl+'}${(index + 1) % 10}) · ${$_('navigation.dragQuick')}`}
      active={false}
      quickId={entry.id}
      draggable
      ondragstart={event => startQuickDrag(entry.id, event)}
      ondragover={event => event.preventDefault()}
      ondrop={event => dropQuick(entry.id, event)}
      ondragend={() => draggedQuickId = null}
      onclick={() => nav.navigate(entry)}
    />
  {/each}
  {#if nav.state.showApps}
    {#each getRailTabs() as tab (tab.extensionId)}
      <RailButton icon={tab.icon || 'mdi:puzzle'} label={tab.label} active={getActiveExtension() === tab.extensionId} onclick={() => setActiveExtension(tab.extensionId)} />
    {/each}
  {/if}
  <div class="mt-auto border-t border-border pt-2"><RailButton icon="mdi:plus" label="Add virtual folder" active={false} onclick={onAddVirtualFolder} /></div>
</nav>
