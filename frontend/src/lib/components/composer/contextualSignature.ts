export type SignatureMessageType = 'new' | 'reply' | 'reply-all' | 'forward'

export interface SignatureRuleLike {
  id: string
  enabled: boolean
  recipients: string[]
  messageTypes: SignatureMessageType[]
  signatureHtml?: string
  signatureText?: string
  signaturePlacement?: string
  signatureSeparator?: boolean
}

export type SignatureRecipient = string | { address?: string; email?: string }

function recipientAddress(recipient: SignatureRecipient): string {
  const value = typeof recipient === 'string' ? recipient : recipient.address || recipient.email || ''
  return value.trim().toLowerCase()
}

function ruleSpecificity(rule: SignatureRuleLike, recipients: string[]): number {
  let specificity = 0
  for (const configured of rule.recipients || []) {
    const normalized = configured.trim().toLowerCase()
    if (!normalized) continue
    if (normalized.startsWith('@')) {
      const domain = normalized.slice(1)
      if (recipients.some((email) => email.endsWith(`@${domain}`))) {
        specificity = Math.max(specificity, 1)
      }
      continue
    }
    if (recipients.includes(normalized)) specificity = 2
  }
  return specificity
}

/** Resolve the most specific enabled rule; array order breaks equal-specificity ties. */
export function resolveSignatureRule<T extends SignatureRuleLike>(
  rules: T[] | undefined,
  mode: SignatureMessageType,
  recipients: SignatureRecipient[],
): T | null {
  const normalizedRecipients = recipients.map(recipientAddress).filter(Boolean)
  let selected: T | null = null
  let selectedSpecificity = 0

  for (const rule of rules || []) {
    if (!rule.enabled || !rule.messageTypes?.includes(mode)) continue
    const specificity = ruleSpecificity(rule, normalizedRecipients)
    if (specificity <= selectedSpecificity) continue
    selected = rule
    selectedSpecificity = specificity
  }

  return selected
}
