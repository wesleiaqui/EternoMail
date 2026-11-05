<script lang="ts">
  import Icon from '@iconify/svelte'
  import { DropdownMenu } from 'bits-ui'
  import { navigationCards as nav } from '$lib/stores/navigationCards.svelte'
  import { _ } from '$lib/i18n'
  import type { NavigationCard, NavigationEntry } from '$lib/sidebar/cards'
  let { selectedFolderId = null, onSelect, onEdit, onDelete }: { selectedFolderId?: string | null; onSelect: (entry: NavigationEntry) => void; onEdit?: (entry: NavigationEntry) => void; onDelete?: (entry: NavigationEntry) => void } = $props()
  function cardName(card: NavigationCard): string {
    const names: Record<string, [string, string]> = { virtual: ['Virtual folders', 'navigation.virtualCard'], primary: ['Mail', 'navigation.mailCard'], secondary: ['More', 'navigation.moreCard'], pinned: ['Pinned folders', 'navigation.pinnedCard'], favorites: ['Starred folders', 'navigation.favoritesCard'] }
    return names[card.id]?.[0] === card.name ? $_(names[card.id][1]) : card.name
  }
  let editing = $state(false)
  let newName = $state('')
  function drop(event: DragEvent, card: string, index: number) {
    event.preventDefault(); event.stopPropagation()
    const entry = event.dataTransfer?.getData('application/x-postilaatikko-entry')
    if (entry) { nav.moveEntry(entry, card, index); return }
    const source = event.dataTransfer?.getData('application/x-postilaatikko-card')
    const from = nav.state.cards.findIndex(c => c.id === source), to = nav.state.cards.findIndex(c => c.id === card)
    if (source && from >= 0 && to >= 0) nav.moveCard(source, to - from)
  }
  function addCard() {
    if (!newName.trim()) return
    nav.update({ ...nav.state, cards: [...nav.state.cards, { id: crypto.randomUUID(), name: newName.trim(), hidden: false, collapsed: false, entries: [] }] })
    newName = ''
  }
  function dragCard(event: DragEvent, card: string) {
    event.dataTransfer?.setData('application/x-postilaatikko-card', card)
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
  }
  function dragEntry(event: DragEvent, id: string) {
    event.stopPropagation()
    event.dataTransfer?.setData('application/x-postilaatikko-entry', id)
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
  }
  function isQuick(id: string): boolean { return nav.state.quick.includes(id) }
  function quickFull(): boolean { return nav.state.quick.length >= 10 }
  function badgeCount(entry: NavigationEntry): number | undefined {
    return entry.count ?? (entry.unreadCount ? entry.unreadCount : entry.totalCount)
  }
</script>
<div class="flex items-center justify-end px-2 py-1">
  <button type="button" class="text-xs text-muted-foreground hover:text-foreground" aria-pressed={editing} onclick={() => editing = !editing}>{$_('navigation.organize')}</button>
</div>
{#each nav.state.cards as card, cardIndex (card.id)}
  {#if editing || (!card.hidden && card.entries.length > 0)}
    <section class="mx-2 mb-3 rounded-xl border border-border bg-muted/20 overflow-hidden {card.hidden ? 'opacity-50' : ''}"
      aria-label={cardName(card)} data-navigation-card={card.id}>
      <div class="group/card flex items-center gap-1 px-2 py-2" draggable={editing}
        role="group" aria-label={cardName(card)}
        ondragstart={(event) => dragCard(event, card.id)}
        ondragover={(event) => event.preventDefault()} ondrop={(event) => drop(event, card.id, card.entries.length)}>
        {#if !editing}
          <span class="inline-flex h-5 w-4 shrink-0 cursor-grab items-center justify-center text-muted-foreground/60 opacity-0 transition-opacity group-hover/card:opacity-100 active:cursor-grabbing"
            draggable="true" role="button" tabindex="-1" aria-label={$_('navigation.dragCard')}
            ondragstart={(event) => dragCard(event, card.id)}>
            <Icon icon="mdi:drag-vertical" class="h-4 w-4" />
          </span>
        {/if}
        <button type="button" class="flex flex-1 items-center gap-1 text-sm font-medium text-left min-w-0" aria-label={cardName(card)} title={cardName(card)} aria-expanded={!card.collapsed} onclick={() => nav.patchCard(card.id, { collapsed: !card.collapsed })}>
          <Icon icon={card.collapsed ? 'mdi:chevron-right' : 'mdi:chevron-down'} class="h-4 w-4 shrink-0" /><span class="min-w-0 whitespace-normal break-words leading-tight line-clamp-2">{cardName(card)}</span>
        </button>
        {#if editing}
          <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted disabled:opacity-30" disabled={cardIndex === 0} aria-label={$_('navigation.up')} onclick={() => nav.moveCard(card.id, -1)}><Icon icon="mdi:arrow-up" /></button>
          <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted disabled:opacity-30" disabled={cardIndex === nav.state.cards.length - 1} aria-label={$_('navigation.down')} onclick={() => nav.moveCard(card.id, 1)}><Icon icon="mdi:arrow-down" /></button>
          <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted" aria-label={$_(card.hidden ? 'navigation.show' : 'navigation.hide')} onclick={() => nav.patchCard(card.id, { hidden: !card.hidden })}><Icon icon={card.hidden ? 'mdi:eye-outline' : 'mdi:eye-off-outline'} /></button>
        {/if}
      </div>
      {#if !card.collapsed || editing}
        <div class="min-h-3 pb-1" role="group" aria-label={cardName(card)} ondragover={(event) => event.preventDefault()} ondrop={(event) => drop(event, card.id, card.entries.length)}>
          {#each card.entries as id, index (id)}
            {@const entry = nav.entries.find(e => e.id === id)}
            {#if entry}
              <div class="group relative flex items-center gap-1 px-2 py-0.5" role="group" aria-label={entry.name} draggable={editing}
                ondragstart={(event) => dragEntry(event, id)}
                ondragover={(event) => event.preventDefault()} ondrop={(event) => drop(event, card.id, index)}>
                <button type="button" class="sidebar-primary-link min-w-0 flex-1 {selectedFolderId === entry.folderId || (entry.kind === 'system' && selectedFolderId === entry.folderType) || (entry.kind === 'inboxes' && selectedFolderId === 'inbox') ? 'sidebar-primary-link-selected' : ''} {entry.kind === 'virtual' ? (isQuick(id) ? 'pr-16' : 'group-hover:pr-16') : (isQuick(id) ? 'pr-10' : 'group-hover:pr-10')}" onclick={() => onSelect(entry)} title={entry.path || entry.name} aria-label={entry.name}>
                  <Icon icon={entry.folderType === 'replyLater' || entry.folderType === 'snoozed' ? 'mdi:clock-outline' : entry.folderType === 'readLater' ? 'mdi:book-clock-outline' : entry.kind === 'virtual' ? 'mdi:folder-multiple-outline' : entry.kind === 'inboxes' ? 'mdi:inbox-multiple-outline' : 'mdi:folder-outline'} class="h-5 w-5 shrink-0" />
                  <span class="navigation-entry-label min-w-0 flex-1 whitespace-normal break-words leading-tight line-clamp-2">{entry.name}</span>
                  {#if badgeCount(entry) !== undefined && badgeCount(entry)! > 0}<span class="ml-auto shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-xs tabular-nums text-muted-foreground" title={entry.unreadCount ? `${entry.unreadCount} unread / ${entry.totalCount || 0} messages` : `${badgeCount(entry)} messages`}>{badgeCount(entry)}</span>{/if}
                </button>
                {#if !editing}
                  <button type="button" class="absolute {entry.kind === 'virtual' ? 'right-8' : 'right-1'} top-1/2 z-10 inline-flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded bg-background/80 hover:bg-muted focus-visible:opacity-100 transition-opacity {isQuick(id) ? 'text-amber-500' : 'text-muted-foreground/60 opacity-0 group-hover:opacity-100'}"
                    aria-label={$_('navigation.favorite')} aria-pressed={isQuick(id)} title={$_('navigation.favorite')}
                    disabled={!isQuick(id) && quickFull()}
                    onclick={() => nav.toggleQuick(id)}>
                    <Icon icon={isQuick(id) ? 'mdi:star' : 'mdi:star-outline'} />
                  </button>
                {/if}
                {#if entry.kind === 'virtual' && !editing}
                  <DropdownMenu.Root>
                    <DropdownMenu.Trigger
                      class="absolute right-1 top-1/2 z-10 inline-flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded bg-background/80 text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover:opacity-100"
                      aria-label={$_('common.moreActions')}
                      title={$_('common.moreActions')}
                    >
                      <Icon icon="mdi:dots-horizontal" class="h-4 w-4" />
                    </DropdownMenu.Trigger>
                    <DropdownMenu.Portal>
                      <DropdownMenu.Content align="end" side="right" sideOffset={4} class="z-50 min-w-[160px] rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md">
                        <DropdownMenu.Item onSelect={() => onEdit?.(entry)} class="flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent focus:text-accent-foreground">
                          <Icon icon="mdi:pencil-outline" class="mr-2 h-4 w-4" />{$_('common.edit')}
                        </DropdownMenu.Item>
                        <DropdownMenu.Item onSelect={() => onDelete?.(entry)} class="flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm text-destructive outline-none focus:bg-accent focus:text-accent-foreground">
                          <Icon icon="mdi:delete-outline" class="mr-2 h-4 w-4" />{$_('common.delete')}
                        </DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu.Portal>
                  </DropdownMenu.Root>
                {:else if entry.kind === 'virtual'}
                  <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Edit ${entry.name}`} title={`Edit ${entry.name}`} onclick={() => onEdit?.(entry)}><Icon icon="mdi:pencil-outline" /></button>
                  <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label={`Delete ${entry.name}`} title={`Delete ${entry.name}`} onclick={() => onDelete?.(entry)}><Icon icon="mdi:delete-outline" /></button>
                {/if}
                {#if editing}
                  <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted" aria-label={$_('navigation.quickAccess')} aria-pressed={nav.state.quick.includes(id)} disabled={!nav.state.quick.includes(id) && nav.state.quick.length >= 10} onclick={() => nav.toggleQuick(id)}><Icon icon={nav.state.quick.includes(id) ? 'mdi:star' : 'mdi:star-outline'} /></button>
                  <select class="w-20 min-w-0 rounded border border-border bg-background text-xs" aria-label={$_('navigation.moveGroup')} value={card.id} onchange={(event) => nav.moveEntry(id, event.currentTarget.value, 999)}>
                    {#each nav.state.cards as target (target.id)}<option value={target.id}>{cardName(target)}</option>{/each}
                  </select>
                  <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted disabled:opacity-30" disabled={index === 0} aria-label={$_('navigation.up')} onclick={() => nav.moveEntry(id, card.id, index - 1)}><Icon icon="mdi:arrow-up" /></button>
                  <button type="button" class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded hover:bg-muted disabled:opacity-30" disabled={index === card.entries.length - 1} aria-label={$_('navigation.down')} onclick={() => nav.moveEntry(id, card.id, index + 1)}><Icon icon="mdi:arrow-down" /></button>
                {/if}
              </div>
            {/if}
          {/each}
        </div>
      {/if}
    </section>
  {/if}
{/each}
{#if editing}
  <form class="mx-2 mb-2 flex gap-1" onsubmit={(event) => { event.preventDefault(); addCard() }}>
    <input class="w-full min-w-0 rounded border border-border bg-background px-2 text-sm" bind:value={newName} placeholder={$_('navigation.groupName')} aria-label={$_('navigation.groupName')} maxlength="100" />
    <button type="submit" class="p-2" aria-label={$_('navigation.addGroup')}><Icon icon="mdi:plus" /></button>
  </form>
  <label class="mx-3 text-xs flex gap-2"><input type="checkbox" checked={nav.state.showApps} onchange={(event) => nav.update({ ...nav.state, showApps: event.currentTarget.checked })} />{$_('navigation.showApps')}</label>
{/if}
