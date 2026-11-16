<script>
  import AttachmentList from '../../src/lib/components/viewer/AttachmentList.svelte'
  let { accountId, messageId, encryptedAttachments } = $props()
</script>
<!-- The real reader card's key handler, kept here to test delegated event order. -->
<div role="button" tabindex="-1" onkeydown={(event)=>{
  if(event.key==='Enter'||event.key===' '){event.preventDefault();window.cardToggles++}
}}>
  <AttachmentList {accountId} {messageId} {encryptedAttachments}/>
</div>
