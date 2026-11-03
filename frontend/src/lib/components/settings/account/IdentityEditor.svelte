<script lang="ts">
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { _ } from '$lib/i18n'
  import SignatureEditor from './SignatureEditor.svelte'
  // @ts-ignore - wailsjs path
  import { account } from '../../../../../wailsjs/go/models'

  interface Props {
    /** Whether the dialog is open */
    open?: boolean
    /** Identity to edit (null for new identity) */
    identity?: account.Identity | null
    /** Account ID (required for creating new identity) */
    accountId: string
    /** Callback when dialog should close */
    onClose?: () => void
    /** Callback when identity is saved */
    onSave?: (config: account.IdentityConfig) => Promise<void>
  }

  let {
    open = $bindable(false),
    identity = null,
    accountId: _accountId,
    onClose,
    onSave,
  }: Props = $props()

  // Form state
  let email = $state('')
  let name = $state('')
  let signatureHtml = $state('')
  let signatureText = $state('')
  let signatureEnabled = $state(true)
  let signatureForNew = $state(true)
  let signatureForReply = $state(true)
  let signatureForForward = $state(true)
  let signaturePlacement = $state<'above' | 'below'>('above')
  let signatureSeparator = $state(false)
  type SignatureRule = {
    id: string
    name: string
    enabled: boolean
    recipients: string[]
    messageTypes: ('new' | 'reply' | 'reply-all' | 'forward')[]
    signatureHtml: string
    signatureText: string
    signaturePlacement: 'above' | 'below'
    signatureSeparator: boolean
  }
  const supportedSignatureMessageTypes = new Set<SignatureRule['messageTypes'][number]>([
    'new', 'reply', 'reply-all', 'forward',
  ])

  function normalizeSignatureRules(rules: account.SignatureRule[] | null | undefined): SignatureRule[] {
    return (rules || []).map(rule => ({
      ...structuredClone(rule),
      signatureHtml: rule.signatureHtml || '',
      signatureText: rule.signatureText || '',
      signaturePlacement: rule.signaturePlacement === 'below' ? 'below' : 'above',
      signatureSeparator: !!rule.signatureSeparator,
      messageTypes: rule.messageTypes.filter((type): type is SignatureRule['messageTypes'][number] =>
        supportedSignatureMessageTypes.has(type as SignatureRule['messageTypes'][number])),
    }))
  }

  let signatureRules = $state<SignatureRule[]>([])

  let saving = $state(false)
  let errors = $state<Record<string, string>>({})

  // Initialize form when identity changes
  $effect(() => {
    if (open) {
      if (identity) {
        // Editing existing identity
        email = identity.email || ''
        name = identity.name || ''
        signatureHtml = identity.signatureHtml || ''
        signatureText = identity.signatureText || ''
        signatureEnabled = identity.signatureEnabled ?? true
        signatureForNew = identity.signatureForNew ?? true
        signatureForReply = identity.signatureForReply ?? true
        signatureForForward = identity.signatureForForward ?? true
        signaturePlacement = (identity.signaturePlacement as 'above' | 'below') || 'above'
        signatureSeparator = identity.signatureSeparator ?? false
        signatureRules = normalizeSignatureRules(identity.signatureRules)
      } else {
        // New identity - reset to defaults
        email = ''
        name = ''
        signatureHtml = ''
        signatureText = ''
        signatureEnabled = true
        signatureForNew = true
        signatureForReply = true
        signatureForForward = true
        signaturePlacement = 'above'
        signatureSeparator = false
        signatureRules = []
      }
      errors = {}
    }
  })

  function validate(): boolean {
    errors = {}

    if (!email.trim()) {
      errors.email = $_('identity.emailRequired')
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      errors.email = $_('identity.invalidEmailFormat')
    }

    if (!name.trim()) {
      errors.name = $_('identity.displayNameRequired')
    }

    return Object.keys(errors).length === 0
  }

  async function handleSave() {
    if (!validate()) return

    saving = true
    try {
      const config = new account.IdentityConfig({
        email: email.trim(),
        name: name.trim(),
        signatureHtml,
        signatureText,
        signatureRules,
        signatureEnabled,
        signatureForNew,
        signatureForReply,
        signatureForForward,
        signaturePlacement,
        signatureSeparator,
      })

      await onSave?.(config)
      open = false
      onClose?.()
    } catch (err) {
      console.error('Failed to save identity:', err)
      errors.general = $_('identity.saveFailed')
    } finally {
      saving = false
    }
  }

  const messageTypes: SignatureRule['messageTypes'] = ['new', 'reply', 'reply-all', 'forward']

  function addSignatureRule() {
    signatureRules = [...signatureRules, {
      id: crypto.randomUUID(),
      name: $_('identity.signatureRuleName'),
      enabled: true,
      recipients: [],
      messageTypes: ['new', 'reply', 'reply-all', 'forward'],
      signatureHtml: '',
      signatureText: '',
      signaturePlacement: 'above',
      signatureSeparator: false,
    }]
  }

  function updateRuleRecipients(rule: SignatureRule, value: string) {
    rule.recipients = value.split(/[;,\n]/).map(item => item.trim().toLowerCase()).filter(Boolean)
    signatureRules = [...signatureRules]
  }

  function toggleRuleMessageType(rule: SignatureRule, mode: SignatureRule['messageTypes'][number], checked: boolean) {
    rule.messageTypes = checked
      ? [...new Set([...rule.messageTypes, mode])]
      : rule.messageTypes.filter(item => item !== mode)
    signatureRules = [...signatureRules]
  }

  function moveSignatureRule(index: number, offset: -1 | 1) {
    const target = index + offset
    if (target < 0 || target >= signatureRules.length) return
    const reordered = [...signatureRules]
    ;[reordered[index], reordered[target]] = [reordered[target], reordered[index]]
    signatureRules = reordered
  }

  function handleCancel() {
    open = false
    onClose?.()
  }

  function handleOpenChange(isOpen: boolean) {
    open = isOpen
    if (!isOpen) {
      onClose?.()
    }
  }

  // Convert HTML to plain text for the "Generate from HTML" button
  function generatePlainTextFromHtml() {
    if (!signatureHtml) return

    const temp = document.createElement('div')
    temp.innerHTML = signatureHtml

    // Replace <br> and block elements with newlines
    const blockElements = temp.querySelectorAll('p, div, br, li')
    blockElements.forEach(el => {
      if (el.tagName === 'BR') {
        el.replaceWith('\n')
      } else if (el.tagName === 'LI') {
        el.prepend(document.createTextNode('- '))
        el.append(document.createTextNode('\n'))
      } else {
        el.append(document.createTextNode('\n'))
      }
    })

    let text = temp.textContent || ''
    text = text.replace(/\n{3,}/g, '\n\n')
    signatureText = text.trim()
  }
</script>

<Dialog.Root bind:open onOpenChange={handleOpenChange}>
  <Dialog.Content class="max-w-2xl max-h-[90vh] overflow-y-auto">
    <Dialog.Header>
      <Dialog.Title>
        {identity ? $_('identity.editEmailTitle') : $_('identity.addEmailTitle')}
      </Dialog.Title>
      <Dialog.Description>
        {identity
          ? $_('identity.editEmailDescription')
          : $_('identity.addEmailDescription')}
      </Dialog.Description>
    </Dialog.Header>

    <form onsubmit={(e) => { e.preventDefault(); handleSave() }} class="space-y-6">
      <!-- Email & Name -->
      <div class="space-y-4">
        <div class="space-y-2">
          <Label for="email">{$_('identity.emailAddressLabel')}</Label>
          <Input
            id="email"
            type="email"
            placeholder="you@example.com"
            bind:value={email}
            class={errors.email ? 'border-destructive' : ''}
          />
          {#if errors.email}
            <p class="text-sm text-destructive">{errors.email}</p>
          {/if}
        </div>

        <div class="space-y-2">
          <Label for="name">{$_('identity.displayNameLabel')}</Label>
          <Input
            id="name"
            type="text"
            placeholder="John Smith"
            bind:value={name}
            class={errors.name ? 'border-destructive' : ''}
          />
          <p class="text-xs text-muted-foreground">
            {$_('identity.displayNameHelp')}
          </p>
          {#if errors.name}
            <p class="text-sm text-destructive">{errors.name}</p>
          {/if}
        </div>
      </div>

      <!-- Divider -->
      <div class="border-t border-border"></div>

      <!-- Signature Section -->
      <div class="space-y-4">
        <div class="flex items-center gap-2">
          <input
            type="checkbox"
            id="signatureEnabled"
            bind:checked={signatureEnabled}
            class="w-4 h-4 rounded border-input accent-primary"
          />
          <Label for="signatureEnabled" class="cursor-pointer font-medium">
            {$_('identity.useSignature')}
          </Label>
        </div>

        {#if signatureEnabled}
          <!-- HTML Signature Editor -->
          <div class="space-y-2">
            <Label>{$_('identity.htmlSignature')}</Label>
            <SignatureEditor
              value={signatureHtml}
              placeholder="Enter your signature..."
              onchange={(html) => signatureHtml = html}
            />
            <p class="text-xs text-muted-foreground">
              {$_('identity.signatureHelp')}
            </p>
          </div>

          <!-- Plain Text Signature -->
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <Label for="signatureText">{$_('identity.plainTextSignature')}</Label>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onclick={generatePlainTextFromHtml}
                disabled={!signatureHtml}
                class="text-xs h-7"
              >
                {$_('identity.generateFromHtml')}
              </Button>
            </div>
            <textarea
              id="signatureText"
              bind:value={signatureText}
              placeholder="Plain text version for text-only emails..."
              class="w-full min-h-[80px] p-3 text-sm bg-background border border-input rounded-md resize-y focus:outline-none focus:ring-2 focus:ring-ring font-mono"
            ></textarea>
            <p class="text-xs text-muted-foreground">
              {$_('identity.plainTextHelp')}
            </p>
          </div>

          <!-- Divider -->
          <div class="border-t border-border"></div>

          <!-- Signature Behavior -->
          <div class="space-y-4">
            <Label class="font-medium">{$_('identity.appendSignatureTo')}</Label>
            <div class="flex flex-wrap gap-4">
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  bind:checked={signatureForNew}
                  class="w-4 h-4 rounded border-input accent-primary"
                />
                <span class="text-sm">{$_('identity.newMessages')}</span>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  bind:checked={signatureForReply}
                  class="w-4 h-4 rounded border-input accent-primary"
                />
                <span class="text-sm">{$_('identity.replies')}</span>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  bind:checked={signatureForForward}
                  class="w-4 h-4 rounded border-input accent-primary"
                />
                <span class="text-sm">{$_('identity.forwards')}</span>
              </label>
            </div>
          </div>

          <!-- Signature Placement -->
          <div class="space-y-3">
            <Label class="font-medium">{$_('identity.signaturePlacementLabel')}</Label>
            <div class="flex gap-4">
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="placement"
                  value="above"
                  bind:group={signaturePlacement}
                  class="w-4 h-4 accent-primary"
                />
                <span class="text-sm">{$_('identity.aboveQuotedText')}</span>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name="placement"
                  value="below"
                  bind:group={signaturePlacement}
                  class="w-4 h-4 accent-primary"
                />
                <span class="text-sm">{$_('identity.belowQuotedText')}</span>
              </label>
            </div>
          </div>

          <!-- Separator Option -->
          <div class="flex items-center gap-2">
            <input
              type="checkbox"
              id="signatureSeparator"
              bind:checked={signatureSeparator}
              class="w-4 h-4 rounded border-input accent-primary"
            />
            <Label for="signatureSeparator" class="cursor-pointer text-sm">
              {$_('identity.addSeparator')} (<code class="text-xs bg-muted px-1 rounded">-- </code>)
            </Label>
          </div>
        {/if}

        <div class="border-t border-border pt-4 space-y-4">
          <div class="flex items-center justify-between gap-3">
            <div>
              <Label class="font-medium">{$_('identity.contextualSignatures')}</Label>
              <p class="text-xs text-muted-foreground">{$_('identity.contextualSignaturesHelp')}</p>
            </div>
            <Button type="button" variant="outline" size="sm" onclick={addSignatureRule} disabled={signatureRules.length >= 32}>
              <Icon icon="mdi:plus" class="w-4 h-4 mr-1" /> {$_('identity.addSignatureRule')}
            </Button>
          </div>

          {#each signatureRules as rule, index (rule.id)}
            <section class="rounded-md border border-border p-3 space-y-3">
              <div class="flex items-center gap-2">
                <input type="checkbox" bind:checked={rule.enabled} aria-label={$_('identity.signatureRuleEnabled')} class="w-4 h-4 accent-primary" />
                <Input bind:value={rule.name} aria-label={$_('identity.signatureRuleName')} class="flex-1" />
                <Button type="button" variant="ghost" size="sm" disabled={index === 0} onclick={() => moveSignatureRule(index, -1)} aria-label={$_('identity.moveRuleUp')}>
                  <Icon icon="mdi:arrow-up" class="w-4 h-4" />
                </Button>
                <Button type="button" variant="ghost" size="sm" disabled={index === signatureRules.length - 1} onclick={() => moveSignatureRule(index, 1)} aria-label={$_('identity.moveRuleDown')}>
                  <Icon icon="mdi:arrow-down" class="w-4 h-4" />
                </Button>
                <Button type="button" variant="ghost" size="sm" onclick={() => signatureRules = signatureRules.filter((_, i) => i !== index)} aria-label={$_('identity.removeSignatureRule')}>
                  <Icon icon="mdi:delete-outline" class="w-4 h-4" />
                </Button>
              </div>
              <div class="space-y-1">
                <Label>{$_('identity.signatureRuleRecipients')}</Label>
                <Input value={rule.recipients.join(', ')} oninput={(event) => updateRuleRecipients(rule, event.currentTarget.value)} placeholder="person@example.org, @example.net" />
                <p class="text-xs text-muted-foreground">{$_('identity.signatureRuleRecipientsHelp')}</p>
              </div>
              <div class="flex flex-wrap gap-x-4 gap-y-2">
                {#each messageTypes as mode (mode)}
                  <label class="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={rule.messageTypes.includes(mode)} onchange={(event) => toggleRuleMessageType(rule, mode, event.currentTarget.checked)} class="w-4 h-4 accent-primary" />
                    {$_(`identity.signatureRule${mode === 'new' ? 'New' : mode === 'reply' ? 'Reply' : mode === 'reply-all' ? 'ReplyAll' : 'Forward'}`)}
                  </label>
                {/each}
              </div>
              <div class="space-y-2">
                <Label>{$_('identity.signatureRuleContent')}</Label>
                <SignatureEditor value={rule.signatureHtml} placeholder="Enter this rule's signature..." onchange={(html) => { rule.signatureHtml = html; signatureRules = [...signatureRules] }} />
                <textarea bind:value={rule.signatureText} aria-label={$_('identity.signatureRuleContent')} placeholder={$_('identity.signatureRulePlainText')} class="w-full min-h-[60px] p-2 text-sm bg-background border border-input rounded-md resize-y font-mono"></textarea>
              </div>
              <div class="space-y-2">
                <Label>{$_('identity.signaturePlacementLabel')}</Label>
                <div class="flex flex-wrap gap-4">
                  <label class="flex items-center gap-2 text-sm"><input type="radio" name={`signature-placement-${rule.id}`} value="above" bind:group={rule.signaturePlacement} class="accent-primary" />{$_('identity.aboveQuotedText')}</label>
                  <label class="flex items-center gap-2 text-sm"><input type="radio" name={`signature-placement-${rule.id}`} value="below" bind:group={rule.signaturePlacement} class="accent-primary" />{$_('identity.belowQuotedText')}</label>
                </div>
                <label class="flex items-center gap-2 text-sm">
                  <input type="checkbox" bind:checked={rule.signatureSeparator} class="accent-primary" />
                  {$_('identity.addSeparator')}
                </label>
              </div>
            </section>
          {/each}
        </div>
      </div>

      <!-- Error message -->
      {#if errors.general}
        <div class="flex items-start gap-2 p-3 rounded-lg bg-destructive/10 border border-destructive/20">
          <Icon icon="mdi:alert-circle" class="w-5 h-5 text-destructive flex-shrink-0 mt-0.5" />
          <p class="text-sm text-destructive">{errors.general}</p>
        </div>
      {/if}

      <!-- Actions -->
      <div class="flex items-center justify-end gap-2 pt-4 border-t border-border">
        <Button type="button" variant="ghost" onclick={handleCancel} disabled={saving}>
          {$_('common.cancel')}
        </Button>
        <Button type="submit" disabled={saving}>
          {#if saving}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
          {/if}
          {identity ? $_('identity.saveIdentityChanges') : $_('identity.addEmailAddressButton')}
        </Button>
      </div>
    </form>
  </Dialog.Content>
</Dialog.Root>
