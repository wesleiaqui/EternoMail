<script lang="ts">
  import MessageContextMenu from '../../src/lib/components/common/MessageContextMenu.svelte'
</script>
<MessageContextMenu messageIds={['message-a']} replyMessageId="message-a" accountId="a" currentFolderId="inbox" folderType="inbox" isStarred={false} isRead={false}>
  <button type="button" class="m-8 rounded border p-4">Selected message</button>
</MessageContextMenu>
