<script lang="ts">
 import ExtensionRail from '../../src/lib/components/rail/ExtensionRail.svelte'
 import MessageList from '../../src/lib/components/list/MessageList.svelte'
 import GlobalSearch from '../../src/lib/components/common/GlobalSearch.svelte'
 import VirtualFolderDialog from '../../src/lib/components/sidebar/VirtualFolderDialog.svelte'
 import { accountStore } from '../../src/lib/stores/accounts.svelte'
 import { virtualFoldersStore } from '../../src/lib/stores/virtualFolders.svelte'
 let open = $state(false), query = $state(''), saveOpen = $state(false)
 let list: MessageList
 let selected = $state('')
 $effect(() => {
   const request = virtualFoldersStore.saveSearchRequest
   if (!request) return
   virtualFoldersStore.clearSaveSearchRequest()
   query = request.query
   saveOpen = true
 })
</script>
<div class="flex h-screen bg-background text-foreground">
 <ExtensionRail onSearch={() => open = true} onAddVirtualFolder={() => saveOpen = true} />
 <div style="width:460px;min-width:0" class="shrink-0 border-r border-border"><MessageList bind:this={list} accountId="a" folderId="inbox-a" folderName="Saapuneet" folderType="inbox" onWidenSearch={q => list.startAllMailSearch(q)} onConversationSelect={(thread, folder, account) => selected = `${account}:${folder}:${thread}`} /></div>
 <div class="flex-1 p-8"><button class="sr-only" onclick={() => list.setQuickFilter('unread')}>Unread fixture filter</button><button class="sr-only" onclick={() => list.setQuickFilter('')}>Clear fixture filter</button><h1 class="mb-4 text-2xl">Reader</h1><p>{selected || 'Choose a result from the message list.'}</p><p class="mt-8 text-muted-foreground">Synthetic mailbox fixture. Search and results components are the real app code.</p></div>
</div>
<GlobalSearch bind:open bind:query onSubmit={q => list.startAllMailSearch(q)} />
<VirtualFolderDialog bind:open={saveOpen} virtualFolder={null} accounts={accountStore.accounts} initialSearchQuery={query} onRefreshFolders={async () => {}} onSave={async value => { await virtualFoldersStore.save(value as any) }} />
