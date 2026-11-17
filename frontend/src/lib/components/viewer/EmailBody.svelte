<script lang="ts">
  import { messageFind, highlightMessageText } from './messageFind.svelte'
  import LaterQueueButton from '../common/LaterQueueButton.svelte'
  import Icon from '@iconify/svelte'
  import { ClipboardSetText } from '../../../../wailsjs/runtime/runtime'
  import { GetInlineAttachmentsForAccount, AddImageAllowlist, OpenURL, SetAlwaysLoadImages, ResolveLinkViaCloudflareDNS } from '../../../../wailsjs/go/app/App'
  import { linkifyText, isAllowedEmailURL } from '../../utils/emailLinks'
  import { hasBlockedRemoteImages, restoreBlockedImages } from '../../utils/blockedImages'
  import { getCached, setCache, getCacheGeneration } from '../../stores/inlineAttachmentCache'
  import { isImageAllowedSync, refreshImageAllowlist } from '$lib/stores/imageAllowlist.svelte'
  import { setFocusedPane, focusPreviousPane, focusNextPane } from '$lib/stores/keyboard.svelte'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Dialog from '$lib/components/ui/dialog'
  import { _ } from '$lib/i18n'
  import { toasts } from '$lib/stores/toast'
  import { getAlwaysLoadImages, getThemeMode, setAlwaysLoadImages, getStripTrackingParams } from '$lib/stores/settings.svelte'

  import { stripTrackingParams } from '$lib/utils/linkPrivacy'
  import { linkForAction, openResolvedLink, copyResolvedLink } from '$lib/utils/linkPrivacyActions'

  interface Props {
    conversationId?: string
    messageId: string
    accountId?: string
    bodyHtml?: string
    bodyText?: string
    fromEmail?: string
    onCompose?: (to: string) => void
    onImagesLoaded?: () => void
    encryptedInlineAttachments?: Record<string, string>
    compact?: boolean
    darken?: boolean
  }

  let { conversationId = '', messageId, accountId = '', bodyHtml = '', bodyText = '', fromEmail = '', onCompose, onImagesLoaded, encryptedInlineAttachments, darken = false, compact = false }: Props = $props()

  // State for remote image handling
  let imagesBlocked = $state(true)
  let iframeElement = $state<HTMLIFrameElement | null>(null)
  let plainTextElement = $state<HTMLDivElement | null>(null)
  let iframeReady = $state(false)

  // Inline attachment state
  let inlineAttachments = $state<Record<string, string>>({})
  let lastSentMessageId = $state<string | null>(null)

  // Link tooltip state
  let tooltipVisible = $state(false)
  let tooltipUrl = $state('')
  let tooltipX = $state(0)
  let tooltipY = $state(0)

  // Context menu state (unified for text selection and links)
  let ctxMenuVisible = $state(false)
  let ctxMenuText = $state('')
  let ctxMenuUrl = $state('')
  let ctxMenuX = $state(0)
  let ctxMenuY = $state(0)

  let ctxUrlStripped = $derived(getStripTrackingParams() && stripTrackingParams(ctxMenuUrl).stripped)

  // Derived state
  let hasRemoteImages = $derived(checkForRemoteImages(bodyHtml) || hasBlockedRemoteImages(bodyHtml))

  $effect(() => {
    const query = messageFind.open ? messageFind.query : ''
    if (iframeReady && iframeElement?.contentWindow) iframeElement.contentWindow.postMessage({type: 'reader-find', query}, '*')
    if (plainTextElement) highlightMessageText(plainTextElement, query)
  })

  // Outer iframe element bg: matches the dark-mail surface color (derived from
  // active theme) when darken=true, otherwise white. Reactive to theme changes
  // via getThemeMode() so the iframe outer color updates when user switches.
  let iframeOuterBg = $derived.by(() => {
    void getThemeMode()
    if (compact) return 'transparent'
    return darken ? getChromeBgHsl() : 'white'
  })

  // Loading placeholder SVG - static placeholder without SMIL animateTransform
  // Continuous SMIL animations (<animateTransform>) inside iframes cause WebKit / Wails
  // to constantly re-render and consume excessive (>100%) CPU cycles.
  const loadingPlaceholder = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='120' height='80' viewBox='0 0 120 80'%3E%3Crect fill='%23f3f4f6' width='120' height='80' rx='4'/%3E%3Cg transform='translate(60,35)'%3E%3Ccircle cx='0' cy='0' r='10' fill='none' stroke='%239ca3af' stroke-width='2' stroke-dasharray='16 8'/%3E%3C/g%3E%3Ctext x='60' y='62' text-anchor='middle' fill='%239ca3af' font-size='9' font-family='sans-serif'%3ELoading...%3C/text%3E%3C/svg%3E`

  // Regex pattern for CSS url() with remote http(s) URLs.
  // Handles all quote styles: raw ' or ", decimal &#39;/&#34;, hex &#x27;/&#x22;, named &apos;/&quot;
  const CSS_QUOTE = `(?:['"]|&#(?:39|x27|34|x22);|&(?:apos|quot);)?`
  const CSS_REMOTE_URL_PATTERN = `url\\(\\s*${CSS_QUOTE}\\s*https?://[^)]*?${CSS_QUOTE}\\s*\\)`

  // Bolt optimization: Pre-compile regular expressions once instead of recompiling
  // `new RegExp(CSS_REMOTE_URL_PATTERN)` on every email render or derived state calculation.
  // Pre-compiled regexes eliminate repeated regex compilation overhead in the reader hot path.
  const REMOTE_IMG_RE = /<img[^>]+src=["'](https?:\/\/[^"']+)["']/i
  const CSS_REMOTE_URL_RE = new RegExp(CSS_REMOTE_URL_PATTERN, 'i')
  const CSS_REMOTE_URL_GLOBAL_RE = new RegExp(CSS_REMOTE_URL_PATTERN, 'gi')
  const BG_ATTR_RE = /\bbackground\s*=\s*["'](https?:\/\/[^"']+)["']/i
  const CID_IMG_RE = /src=["']cid:([^"']+)["']/gi
  const BLOCK_IMG_RE = /(<img[^>]+)src=["'](https?:\/\/[^"']+)["']([^>]*>)/gi
  const BG_ATTR_GLOBAL_RE = /\bbackground\s*=\s*["'](https?:\/\/[^"']+)["']/gi

  function checkForRemoteImages(html: string): boolean {
    if (!html) return false
    // Check <img> tags with remote src
    if (REMOTE_IMG_RE.test(html)) return true
    // Check CSS url() references with remote URLs (background-image, background, etc.)
    if (CSS_REMOTE_URL_RE.test(html)) return true
    // Check HTML background attribute with remote URLs
    if (BG_ATTR_RE.test(html)) return true
    return false
  }

  function processCidReferences(html: string): string {
    if (!html) return html
    // Reset lastIndex on global regex before replace
    CID_IMG_RE.lastIndex = 0
    return html.replace(
      CID_IMG_RE,
      (match, contentId) => `src="${loadingPlaceholder}" data-cid="${contentId}"`
    )
  }

  function processHtml(html: string, blockImages: boolean): string {
    if (!html) return ''
    // Backend uses bluemonday in sync fetch and ParseDecryptedBody.
    let processed = processCidReferences(html)
    if (blockImages) {
      // Block <img> tags with remote sources
      BLOCK_IMG_RE.lastIndex = 0
      processed = processed.replace(
        BLOCK_IMG_RE,
        (match, before, src, after) => {
          const placeholder = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='60' viewBox='0 0 100 60'%3E%3Crect fill='%23e5e7eb' width='100' height='60'/%3E%3Ctext x='50' y='35' text-anchor='middle' fill='%239ca3af' font-size='10' font-family='sans-serif'%3EImage blocked%3C/text%3E%3C/svg%3E`
          return `${before}src="${placeholder}" data-blocked-src="${encodeURIComponent(src)}"${after}`
        }
      )
      // Block remote URLs in CSS url() references (covers background-image, background, etc.)
      // Handles all quote encodings: raw, decimal entities, hex entities, named entities
      CSS_REMOTE_URL_GLOBAL_RE.lastIndex = 0
      processed = processed.replace(CSS_REMOTE_URL_GLOBAL_RE, 'url()')
      // Block HTML background attribute with remote URLs
      BG_ATTR_GLOBAL_RE.lastIndex = 0
      processed = processed.replace(
        BG_ATTR_GLOBAL_RE,
        'background=""'
      )
    } else {
      // HTML from the reply extractor arrives with remote images already blocked
      // (placeholder plus data-original-src); loading images puts the address back.
      processed = restoreBlockedImages(processed)
    }
    return processed
  }

  // Reads the active theme's chrome lightness so the dark-mail filter can tune
  // its invert amount — pure black is too dark against most themes' chrome.
  // For themes whose --background hue carries enough chromaticity that the
  // partial-invert + hue-rotate composes into visible color shifts, declare
  // --dark-mail-bg-l (number 0-100) to override with an explicit value.
  function getChromeBgLightness(): number {
    const styles = getComputedStyle(document.documentElement)
    const override = styles.getPropertyValue('--dark-mail-bg-l').trim()
    if (override) {
      const n = parseFloat(override)
      if (!Number.isNaN(n)) return Math.max(0, Math.min(1, n / 100))
    }
    const bg = styles.getPropertyValue('--background').trim()
    const lMatch = bg.match(/(\d+(?:\.\d+)?)%\s*$/)
    if (lMatch) {
      const l = parseFloat(lMatch[1])
      if (!Number.isNaN(l)) return Math.max(0, Math.min(1, l / 100))
    }
    return 0
  }

  // Returns the chrome bg as `hsl(...)` for the iframe element's outer
  // background, matching the rendered dark-mail surface so the rounded
  // corner clip has no color seam.
  function getChromeBgHsl(): string {
    const styles = getComputedStyle(document.documentElement)
    const bg = styles.getPropertyValue('--background').trim()
    return bg ? `hsl(${bg})` : '#000'
  }

  // Per-theme dark-mail saturation override. <1 desaturates the inverted
  // email surface to cancel out the warm/red chromatic shift that partial
  // invert + hue-rotate(180deg) leaves on the residual chromaticity of
  // most input content. Default 1 (no change).
  function getChromeBgSaturate(): number {
    const override = getComputedStyle(document.documentElement)
      .getPropertyValue('--dark-mail-saturate').trim()
    if (override) {
      const n = parseFloat(override)
      if (!Number.isNaN(n) && n > 0) return n
    }
    return 1
  }

  // Per-theme dark-mail hue rotation override (degrees, signed). Shifts the
  // residual chromaticity of the inverted email surface toward a specific
  // hue family — e.g., -20 to cool down a cool-toned theme's surface that
  // would otherwise read as warm gray. Default 0 (no shift, GPU-skipped).
  function getChromeBgHueRotate(): number {
    const override = getComputedStyle(document.documentElement)
      .getPropertyValue('--dark-mail-hue').trim()
    if (override) {
      const n = parseFloat(override)
      if (!Number.isNaN(n)) return n
    }
    return 0
  }

  function buildIframeContent(html: string, applyDarken: boolean): string {
    // Conversation (bubble) mode renders the same faithful HTML as
    // Traditional: bluemonday has already sanitized it and the iframe is
    // sandboxed. Stripping authored styles here flattened HTML email layouts
    // (heading sizes, button fills, card backgrounds).
    const processedHtml = processHtml(html, imagesBlocked)
    // Link icons use the same favicon provider already used by sender
    // avatars. They are intentionally allowed even when marketing images in
    // the email stay blocked; the provider only receives the link's domain.
    const imgSrc = imagesBlocked ? "'self' data: https://www.google.com" : '* data:'

    // Double-invert: page-level invert + image-level re-invert keeps photos
    // looking normal while flipping text, backgrounds, and CSS-defined colors.
    // Blocked-image placeholders intentionally skip the re-invert so they end
    // up dark too — they're chrome, not content.
    // color-scheme: dark switches the UA-default iframe viewport bg to dark so
    // the rounded-corner edge has no white sliver bleeding through.
    // Invert amount derived from theme's chrome lightness — pure invert(1)
    // produces stark black against themes whose chrome isn't pure black.
    const invertAmount = applyDarken ? 1 - getChromeBgLightness() : 1
    const saturate = applyDarken ? getChromeBgSaturate() : 1
    const hueRotate = applyDarken ? getChromeBgHueRotate() : 0
    // Image filter compensates so photos see net saturate(1) + hue-rotate(0) —
    // html's saturate(S) hue-rotate(H) composed with image's saturate(1/S)
    // hue-rotate(-H) approximately cancels for non-grayscale image content.
    const imageSaturate = 1 / saturate
    const darkenStyles = applyDarken && !compact ? `
    html { filter: invert(${invertAmount}) hue-rotate(180deg) saturate(${saturate}) hue-rotate(${hueRotate}deg); background: #fff; color-scheme: dark; }
    img:not([data-blocked-src]), video, iframe, [data-no-invert] { filter: invert(${invertAmount}) hue-rotate(180deg) saturate(${imageSaturate}) hue-rotate(${-hueRotate}deg); }
` : `
    html { color-scheme: light; }
`

    const iframeScript = `
      window.addEventListener('message', function(event) {
        if (event.source !== window.parent || event.data?.type !== 'reader-find' || typeof event.data.query !== 'string') return;
        (${highlightMessageText.toString()})(document.body, event.data.query);
      });

      var lastSentHeight = 0;
      var heightRafId = 0;

      function sendHeight() {
        if (heightRafId) cancelAnimationFrame(heightRafId);
        heightRafId = requestAnimationFrame(function() {
          heightRafId = 0;
          var height = document.body ? document.body.scrollHeight : 0;
          if (height > 0 && Math.abs(height - lastSentHeight) >= 1) {
            lastSentHeight = height;
            window.parent.postMessage({ type: 'iframe-height', height: height }, '*');
          }
        });
      }
      
      function attachImageHandlers() {
        document.querySelectorAll('img').forEach(function(img) {
          if (!img.dataset.heightHandlerAttached) {
            img.dataset.heightHandlerAttached = 'true';
            img.onload = sendHeight;
            img.onerror = sendHeight;
          }
        });
      }

      function decorateTextLinks() {
        var decorated = 0;
        document.querySelectorAll('a[href]').forEach(function(link) {
          if (decorated >= 12 || link.dataset.eternoFavicon || link.querySelector('img, svg')) return;
          var url;
          try { url = new URL(link.href); } catch (_err) { return; }
          if (url.protocol !== 'http:' && url.protocol !== 'https:') return;
          if (!link.textContent || !link.textContent.trim()) return;

          var icon = document.createElement('img');
          icon.className = 'eterno-link-favicon';
          icon.alt = '';
          icon.setAttribute('aria-hidden', 'true');
          icon.setAttribute('data-no-invert', '');
          icon.src = 'https://www.google.com/s2/favicons?domain=' + encodeURIComponent(url.hostname) + '&sz=32';
          icon.onerror = function() { icon.remove(); sendHeight(); };
          link.insertBefore(icon, link.firstChild);
          link.dataset.eternoFavicon = 'true';
          decorated++;
        });
      }
      
      window.addEventListener('message', function(e) {
        if (e.data?.type === 'select-all') {
          var range = document.createRange();
          range.selectNodeContents(document.body);
          var selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
            selection.addRange(range);
          }
          return;
        }
        if (e.data?.type === 'inline-images' && e.data.images) {
          var images = e.data.images;
          var replaced = 0;
          Object.keys(images).forEach(function(cid) {
            // querySelectorAll (not querySelector): a body can legitimately
            // reference the same cid: from multiple <img> tags — e.g. the
            // composer emits cid:c1 twice when the user pastes the same
            // image twice and the dedup collapses both into one inline
            // attachment. Single-querySelector would leave every <img>
            // except the first stuck on the loading placeholder.
            var imgs = document.querySelectorAll('img[data-cid="' + cid + '"]');
            imgs.forEach(function(img) {
              img.src = images[cid];
              img.removeAttribute('data-cid');
              replaced++;
            });
          });
          if (replaced > 0) {
            attachImageHandlers();
            setTimeout(sendHeight, 50);
            setTimeout(sendHeight, 150);
            setTimeout(sendHeight, 300);
          }
        }
        if (e.data?.type === 'request-print-html') {
          var clone = document.body.cloneNode(true);
          clone.querySelectorAll('script').forEach(function(s){ s.remove(); });
          window.parent.postMessage({ type: 'print-html', html: clone.innerHTML }, '*');
        }
      });

      window.addEventListener('load', function() {
        decorateTextLinks();
        attachImageHandlers();
        sendHeight();
        window.parent.postMessage({ type: 'iframe-ready' }, '*');
      });
      
      window.addEventListener('resize', sendHeight);
      new ResizeObserver(sendHeight).observe(document.body);
      setTimeout(sendHeight, 50);
      setTimeout(sendHeight, 200);
      
      document.addEventListener('auxclick', function(e) { e.preventDefault(); });
      document.addEventListener('click', function(e) {
        var link = e.target.closest('a');
        if (link && link.href) {
          e.preventDefault();
          window.parent.postMessage({ type: 'open-link', url: link.href }, '*');
        }
      });

      // Handle link hover for tooltip
      document.addEventListener('mouseover', function(e) {
        var link = e.target.closest('a');
        if (link && link.href) {
          var rect = link.getBoundingClientRect();
          window.parent.postMessage({
            type: 'link-hover',
            url: link.href,
            x: rect.left,
            y: rect.bottom
          }, '*');
        }
      });

      document.addEventListener('mouseout', function(e) {
        var link = e.target.closest('a');
        if (link && link.href) {
          window.parent.postMessage({ type: 'link-hover-end' }, '*');
        }
      });

      // Handle right-click context menu — always prevent native menu, show custom one
      document.addEventListener('contextmenu', function(e) {
        e.preventDefault();
        var selection = window.getSelection();
        var selectedText = (selection && selection.toString().trim().length > 0) ? selection.toString() : '';
        var link = e.target.closest('a');
        var linkUrl = (link && link.href) ? link.href : '';

        window.parent.postMessage({
          type: 'contextmenu',
          text: selectedText,
          url: linkUrl,
          x: e.clientX,
          y: e.clientY
        }, '*');
      });

      // The reader selection belongs to this sandboxed document. Route a
      // native Edit > Copy menu command here instead of copying from the empty
      // parent document.
      window.addEventListener('message', function(e) {
        if (e.data && e.data.type === 'native-edit-command' && e.data.command === 'copy') {
          var selection = window.getSelection();
          var text = selection ? selection.toString() : '';
          if (text) window.parent.postMessage({ type: 'copy-selected-text', text: text }, '*');
          return;
        }
        if (e.data && e.data.type === 'native-edit-command' && e.data.command === 'selectAll') {
          var range = document.createRange();
          range.selectNodeContents(document.body);
          var selection = window.getSelection();
          if (selection) {
            selection.removeAllRanges();
            selection.addRange(range);
          }
        }
      });

      // The message body lives in a sandboxed iframe, so unmodified Gmail-style
      // shortcuts would otherwise never reach the app's global key handler.
      // Forward those shortcuts from non-interactive body content; links and
      // controls keep their normal keyboard behavior.
      var forwardedShortcutKeys = new Set([
        'ArrowUp', 'ArrowDown', 'j', 'k', 'g', 'G', ' ', 'x', 'o', 'Enter',
        'e', 'c', 'v', 'r', 'a', 'f', '/', '!', 'z', 'I', 'U', 'u', 's',
        'F', '#', 'd', 'Backspace', 'Delete', 'Escape'
      ]);
      document.addEventListener('keydown', function(e) {
        var interactiveTarget = isInteractiveElement(e.target);
        if (!interactiveTarget && (e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'c') {
          var selectedText = window.getSelection();
          var text = selectedText ? selectedText.toString() : '';
          if (text) {
            e.preventDefault();
            window.parent.postMessage({ type: 'copy-selected-text', text: text }, '*');
            return;
          }
        }
        var isUnmodifiedShortcut = !e.altKey && !e.ctrlKey && !e.metaKey && forwardedShortcutKeys.has(e.key);
        if (!interactiveTarget && (e.altKey || e.ctrlKey || e.metaKey || isUnmodifiedShortcut)) {
          // For pane navigation, blur inside iframe first
          if (e.altKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'h' || e.key === 'l')) {
            if (document.activeElement) {
              document.activeElement.blur();
            }
            document.body.blur();
            window.blur();
          }
          window.parent.postMessage({
            type: 'iframe-keydown',
            key: e.key,
            code: e.code,
            altKey: e.altKey,
            ctrlKey: e.ctrlKey,
            metaKey: e.metaKey,
            shiftKey: e.shiftKey,
            altGraph: e.getModifierState('AltGraph')
          }, '*');
          if (isUnmodifiedShortcut || ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'f')) e.preventDefault();
        }
      });

      // Notify parent when iframe receives focus/click (but not for links/buttons)
      function isInteractiveElement(el) {
        if (!el) return false;
        var link = el.closest('a');
        if (link && link.href) return true;
        var button = el.closest('button');
        if (button) return true;
        if (el.tagName === 'INPUT' || el.tagName === 'SELECT' || el.tagName === 'TEXTAREA') return true;
        return false;
      }
      document.addEventListener('click', function(e) {
        if (!isInteractiveElement(e.target)) {
          window.parent.postMessage({ type: 'iframe-focus' }, '*');
        }
      });
      document.addEventListener('focus', function(e) {
        if (!isInteractiveElement(e.target)) {
          window.parent.postMessage({ type: 'iframe-focus' }, '*');
        }
      }, true);
    `

    return `<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'self' data:; img-src ${imgSrc}; style-src 'unsafe-inline'; script-src 'unsafe-inline';">
  <sty` + `le>${darkenStyles}
    /* Minimal base styles - avoid overriding email's inline styles */
    * { box-sizing: border-box; }
    html, body {
      margin: 0; padding: 0;
      font-family: system-ui, sans-serif;
      font-size: 14px; line-height: 1.5;
      color: #1a1a0a; background-color: white;
      overflow-x: auto; word-wrap: break-word;
      scrollbar-width: none; /* Firefox */
      -ms-overflow-style: none; /* IE/Edge */
    }
    html::-webkit-scrollbar, body::-webkit-scrollbar { display: none; /* Chrome/Safari/WebKit */ }
    body { padding: 16px; }
    img { max-width: 100%; height: auto; }
    /* HTML emails often use fixed-width tables that overflow a narrow reader
       pane. Shrink them to fit instead of forcing a horizontal scrollbar. */
    table { max-width: 100%; }
    img.eterno-link-favicon { display: inline-block; width: 1em; height: 1em; min-width: 0; min-height: 0; margin: 0 0.26em 0 0; vertical-align: -0.14em; object-fit: contain; }
    /* Ensure empty paragraphs (blank lines) render with visible height */
    p:empty { min-height: 1em; }
    p:has(> br:only-child) { min-height: 1em; }
    img[data-cid] { min-width: 100px; min-height: 60px; }
    a { color: #2563eb; }
    /* Only apply defaults to elements without inline styles */
    blockquote:not([style]) { margin: 0.5em 0; padding-left: 1em; border-left: 3px solid #e5e7eb; color: #6b7280; }
    pre:not([style]) { background: #f3f4f6; padding: 0.5em; border-radius: 4px; overflow-x: auto; }
    /* Conversation bubbles use app colors without a separate white page. */
    ${compact ? `html { color-scheme: normal; }
      /* Render on a white canvas like Gmail and Traditional mode: many HTML
         emails paint white inner cards but leave heading/body text unstyled,
         expecting the default near-black on white. A transparent canvas with
         a light inherited foreground makes that text invisible, so give the
         document a white base and dark default text instead. Nothing here is
         !important on color/background: the email's own styles always win. */
      html, body { margin: 0 !important; padding: 0 !important; }
      html { background: #ffffff; }
      body { background: #ffffff; font-family: system-ui, sans-serif; color: #1f2328; }
      body pre, body code { font-family: ui-monospace, monospace; }` : ''}
    /* Remove table/td defaults that conflict with email layouts */
  </sty` + `le>
</head>
<body>
${processedHtml}
<scr` + `ipt>${iframeScript}</scr` + `ipt>
</body>
</html>`
  }

  // Strip known tracking parameters at use time (link privacy setting).
  // The stored message and displayed URL are never mutated.
  function cleanLinkUrl(url: string): string {
    return getStripTrackingParams() ? stripTrackingParams(url).url : url
  }

  // Handle a clicked link URL — routes mailto: to composer, others to system browser.
  // Used by both the iframe message handler and the plain text div click handler.
  function handleLinkClick(url: string) {
    if (url.startsWith('mailto:')) {
      const emailAddress = url.replace('mailto:', '').split('?')[0]
      if (onCompose) {
        onCompose(emailAddress)
        return
      }
    }
    safeOpenURL(cleanLinkUrl(url))
  }

  // Helper function to safely open URLs
  // Uses our custom OpenURL backend function which properly handles shell escaping
  async function safeOpenURL(url: string) {
    if (!isAllowedEmailURL(url)) return

    // Validate URL format first
    try {
      new URL(url) // Validate it's a proper URL

      // Use our backend OpenURL function which properly handles shell escaping
      try {
        await OpenURL(url)
        toasts.info($_('toast.linkOpened'))
      } catch (err) {
        console.error('[EmailBody] OpenURL failed:', err)
      }
    } catch {
      console.error('[EmailBody] Invalid URL')
    }
  }

  function handleIframeMessage(event: MessageEvent) {
    // Only handle messages from this component's iframe
    if (event.source !== iframeElement?.contentWindow) return

    if (event.data?.type === 'iframe-height' && iframeElement) {
      const targetHeight = `${event.data.height + (compact ? 4 : 20)}px`
      if (iframeElement.style.height !== targetHeight) {
        iframeElement.style.height = targetHeight
      }
    } else if (event.data?.type === 'iframe-ready') {
      iframeReady = true
    } else if (event.data?.type === 'open-link') {
      handleLinkClick(event.data.url as string)
    } else if (event.data?.type === 'copy-selected-text' && typeof event.data.text === 'string') {
      ClipboardSetText(event.data.text).catch((err) => {
        console.error('[EmailBody] Failed to copy selected text:', err)
      })
    } else if (event.data?.type === 'iframe-keydown') {
      // Handle Alt+arrow/hjkl directly for pane navigation
      if (event.data.altKey) {
        const key = event.data.key
        if (key === 'ArrowLeft' || key === 'h') {
          focusPreviousPane()
          // Dispatch event to let App.svelte handle focus
          window.dispatchEvent(new CustomEvent('escape-iframe-focus'))
          return
        } else if (key === 'ArrowRight' || key === 'l') {
          focusNextPane()
          window.dispatchEvent(new CustomEvent('escape-iframe-focus'))
          return
        }
      }
      // A synthetic KeyboardEvent cannot represent AltGraph reliably; normalize
      // only recognized symbol shortcuts from a non-interactive message body.
      const symbol = ['#', '/', '!', '?'].includes(event.data.key) && !event.data.metaKey &&
        (!event.data.ctrlKey || event.data.altGraph) && event.data.altKey
      // For other shortcuts (Ctrl+, Escape), dispatch to window
      const syntheticEvent = new KeyboardEvent('keydown', {
        key: event.data.key,
        code: event.data.code,
        altKey: symbol ? false : event.data.altKey,
        ctrlKey: symbol ? false : event.data.ctrlKey,
        metaKey: event.data.metaKey,
        shiftKey: event.data.shiftKey,
        bubbles: true,
        cancelable: true
      })
      window.dispatchEvent(syntheticEvent)
    } else if (event.data?.type === 'iframe-focus') {
      // Set focus to viewer pane when iframe is clicked/focused
      setFocusedPane('viewer')
    } else if (event.data?.type === 'link-hover') {
      // Show tooltip with link URL - adjust coordinates relative to iframe position
      if (iframeElement) {
        const iframeRect = iframeElement.getBoundingClientRect()
        tooltipUrl = event.data.url
        tooltipX = iframeRect.left + event.data.x
        tooltipY = iframeRect.top + event.data.y
        tooltipVisible = true
      }
    } else if (event.data?.type === 'link-hover-end') {
      // Hide tooltip
      tooltipVisible = false
    } else if (event.data?.type === 'contextmenu') {
      // Show unified context menu for text selection and/or links
      if (iframeElement) {
        const iframeRect = iframeElement.getBoundingClientRect()
        ctxMenuText = event.data.text || ''
        ctxMenuUrl = event.data.url || ''
        ctxMenuX = iframeRect.left + event.data.x
        ctxMenuY = iframeRect.top + event.data.y
        ctxMenuVisible = true
      }
    }
  }

  function sendInlineImagesToIframe(images: Record<string, string>) {
    if (iframeElement?.contentWindow && Object.keys(images).length > 0) {
      // Use spread operator to create plain object from Svelte 5 $state proxy
      // This is needed because postMessage uses structured clone which can't handle proxies
      iframeElement.contentWindow.postMessage({
        type: 'inline-images',
        images: { ...images }
      }, '*')
    }
  }

  // Returns the message body as printable HTML. The body iframe is sandboxed
  // WITHOUT allow-same-origin, so its DOM isn't readable from here — instead we
  // ask the iframe's own script (over the existing postMessage channel) to hand
  // back its rendered body, with images exactly as displayed (inline resolved,
  // remote in their loaded/blocked state, so printing fires no trackers). Falls
  // back to the plain-text render when there's no iframe.
  export function getPrintableHtml(): Promise<string> {
    const win = iframeElement?.contentWindow
    if (!win) {
      return Promise.resolve(bodyText ? `<div style="white-space:pre-wrap">${linkifyText(bodyText)}</div>` : '')
    }
    return new Promise<string>((resolve) => {
      let settled = false
      const onMsg = (ev: MessageEvent) => {
        if (ev.source !== win || ev.data?.type !== 'print-html') return
        settled = true
        window.removeEventListener('message', onMsg)
        resolve(ev.data.html || '')
      }
      window.addEventListener('message', onMsg)
      win.postMessage({ type: 'request-print-html' }, '*')
      setTimeout(() => {
        if (settled) return
        window.removeEventListener('message', onMsg)
        resolve('')
      }, 1500)
    })
  }

  function loadImages() {
    imagesBlocked = false
    onImagesLoaded?.()
  }

  // Handle "Always load for all emails" action.
  async function handleAlwaysLoadAll() {
    try {
      await SetAlwaysLoadImages(true)
      setAlwaysLoadImages(true)
      loadImages()
    } catch (err) {
      console.error('[EmailBody] Failed to enable always-load remote images:', err)
    }
  }

  // Extract domain from email address
  function extractDomain(email: string): string {
    const parts = email.split('@')
    return parts.length === 2 ? parts[1] : ''
  }

  // Handle "Always load for this sender" action
  async function handleAlwaysLoadSender() {
    if (!fromEmail) return
    try {
      await AddImageAllowlist('sender', fromEmail)
      refreshImageAllowlist()
      loadImages()
    } catch (err) {
      console.error('[EmailBody] Failed to add sender to allowlist:', err)
    }
  }

  // Handle "Always load for this domain" action
  async function handleAlwaysLoadDomain() {
    const domain = extractDomain(fromEmail)
    if (!domain) return
    try {
      await AddImageAllowlist('domain', domain)
      refreshImageAllowlist()
      loadImages()
    } catch (err) {
      console.error('[EmailBody] Failed to add domain to allowlist:', err)
    }
  }

  // Check allowlist and reset state on message change (single effect to avoid race conditions)
  // Uses synchronous frontend cache instead of async Wails call to avoid bridge saturation.
  $effect(() => {
    void messageId // dependency only
    const email = fromEmail
    const hasImages = hasRemoteImages

    // Reset state (was in separate effect — merged to avoid race)
    iframeReady = false
    lastSentMessageId = null
    inlineAttachments = {}
    imagesBlocked = true

    if (!hasImages) return

    if (getAlwaysLoadImages()) {
      imagesBlocked = false
      return
    }

    if (email && isImageAllowedSync(email)) {
      imagesBlocked = false
    }
  })

  // Fetch inline attachments when we have cid references
  $effect(() => {
    const id = messageId
    const html = bodyHtml
    const hasCid = html ? /src=["']cid:([^"']+)["']/i.test(html) : false
    const encInline = encryptedInlineAttachments

    if (!id || !hasCid) {
      return
    }

    // For encrypted messages, use the in-memory inline attachments from decryption
    if (encInline && Object.keys(encInline).length > 0) {
      inlineAttachments = encInline
      return
    }

    // Check memory cache first
    const cached = getCached(id)
    if (cached && Object.keys(cached).length > 0) {
      inlineAttachments = cached
      return
    }

    let active = true
    const generation = getCacheGeneration()
    GetInlineAttachmentsForAccount(accountId, id)
      .then((result: Record<string, string>) => {
        if (!active || generation !== getCacheGeneration()) return
        const data = result || {}
        inlineAttachments = data
        if (Object.keys(data).length > 0) {
          setCache(id, data)
        }
      })
      .catch((err: Error) => {
        console.error('[EmailBody] Fetch error:', err)
      })
    return () => { active = false }
  })

  // Build iframe content
  $effect(() => {
    const html = bodyHtml
    void imagesBlocked // dependency only
    void getThemeMode() // rebuild when theme changes so dark-mail invert re-derives
    const applyDarken = darken

    if (iframeElement && html) {
      const content = buildIframeContent(html, applyDarken)
      iframeElement.srcdoc = content
      iframeReady = false
      lastSentMessageId = null
    }
  })

  // Send inline images when ready
  $effect(() => {
    const ready = iframeReady
    const images = inlineAttachments
    const id = messageId
    const alreadySent = lastSentMessageId === id

    if (ready && Object.keys(images).length > 0 && !alreadySent) {
      sendInlineImagesToIframe(images)
      lastSentMessageId = id
    }
  })

  // Message listener
  $effect(() => {
    window.addEventListener('message', handleIframeMessage)
    return () => window.removeEventListener('message', handleIframeMessage)
  })

  // State for controlling the Always Load dropdown
  let alwaysLoadDropdownOpen = $state(false)

  // Listen for Ctrl-L load images event
  $effect(() => {
    function handleLoadImagesEvent() {
      if (hasRemoteImages && imagesBlocked) {
        loadImages()
      }
    }
    window.addEventListener('load-remote-images', handleLoadImagesEvent)
    return () => window.removeEventListener('load-remote-images', handleLoadImagesEvent)
  })

  // Listen for Ctrl-Shift-L always load dropdown event
  $effect(() => {
    function handleAlwaysLoadDropdownEvent() {
      if (hasRemoteImages && imagesBlocked) {
        alwaysLoadDropdownOpen = true
      }
    }
    window.addEventListener('open-always-load-dropdown', handleAlwaysLoadDropdownEvent)
    return () => window.removeEventListener('open-always-load-dropdown', handleAlwaysLoadDropdownEvent)
  })

  // Copy selected text to clipboard
  async function copyTextToClipboard() {
    if (!ctxMenuText) return
    try {
      await navigator.clipboard.writeText(ctxMenuText)
      ctxMenuVisible = false
    } catch (err) {
      console.error('[EmailBody] Failed to copy text:', err)
    }
  }

  // Copy link to clipboard
  async function copyLinkToClipboard() {
    if (!ctxMenuUrl) return
    try {
      await navigator.clipboard.writeText(cleanLinkUrl(ctxMenuUrl))
      ctxMenuVisible = false
    } catch (err) {
      console.error('[EmailBody] Failed to copy link:', err)
    }
  }

  // Open the original (unstripped) link — per-link bypass for the rare site
  // that needs its tracking parameters.
  function openOriginalLink() {
    if (!ctxMenuUrl) return
    safeOpenURL(ctxMenuUrl)
    ctxMenuVisible = false
  }

  // Copy the original (unstripped) link to clipboard
  async function copyOriginalLinkToClipboard() {
    if (!ctxMenuUrl) return
    try {
      await navigator.clipboard.writeText(ctxMenuUrl)
      ctxMenuVisible = false
    } catch (err) {
      console.error('[EmailBody] Failed to copy original link:', err)
    }
  }

  // "Open via Cloudflare DNS": resolve the link's host through Cloudflare DoH
  // (bypassing local DNS-based ad blocking) and follow the redirect chain
  // without cookies so the real destination can be shown before opening.
  // Explicit per-link action only; the lookup contacts the tracking service.
  interface ChainHop {
    url: string
    statusCode: number
  }
  let chainDialogVisible = $state(false)
  let chainLoading = $state(false)
  let chainHops = $state<ChainHop[]>([])
  let chainFinal = $state('')
  let chainError = $state('')
  let chainOriginal = $state('')

  async function openViaCloudflareDNS(original = false) {
    if (!ctxMenuUrl) return
    chainOriginal = ctxMenuUrl
    ctxMenuVisible = false
    chainDialogVisible = true
    chainLoading = true
    chainHops = []
    chainFinal = ''
    chainError = ''
    try {
      const result = await ResolveLinkViaCloudflareDNS(linkForAction(chainOriginal, original, cleanLinkUrl))
      chainHops = result.hops ?? []
      chainFinal = result.finalURL ?? ''
      chainError = result.error ?? ''
    } catch (err) {
      chainError = String(err)
    } finally {
      chainLoading = false
    }
  }

  function openChainURL(url: string, original = false) {
    openResolvedLink(url, original, cleanLinkUrl, safeOpenURL)
    chainDialogVisible = false
  }

  async function copyChainURL(url: string) {
    try {
      await copyResolvedLink(url, cleanLinkUrl, value => navigator.clipboard.writeText(value))
      chainDialogVisible = false
    } catch (err) {
      console.error('[EmailBody] Failed to copy link:', err)
    }
  }

  // Select all text in iframe
  function selectAllInIframe() {
    if (bodyHtml) {
      iframeElement?.contentWindow?.postMessage({ type: 'select-all' }, '*')
    } else if (plainTextElement) {
      const range = document.createRange()
      range.selectNodeContents(plainTextElement)
      const selection = window.getSelection()
      selection?.removeAllRanges()
      selection?.addRange(range)
    }
    ctxMenuVisible = false
  }
  // Open the context-menu link (tracking parameters stripped per setting)
  function openLink() {
    if (!ctxMenuUrl) return
    safeOpenURL(cleanLinkUrl(ctxMenuUrl))
    ctxMenuVisible = false
  }
</script>

<div class="email-body relative">
  {#if bodyHtml}
    {#if hasRemoteImages && imagesBlocked}
      <div class="flex items-center gap-2 px-3 py-2 mb-3 rounded-md bg-yellow-500/10 border border-yellow-500/30 text-sm">
        <Icon icon="mdi:image-off" class="w-4 h-4 text-yellow-600 flex-shrink-0" />
        <span class="text-yellow-700 dark:text-yellow-400">{$_('viewer.remoteImagesBlocked')}</span>

        <div class="ml-auto flex items-center gap-1">
          <!-- Load Images button -->
          <button
            class="px-2 py-1 text-xs font-medium rounded bg-yellow-600 text-white hover:bg-yellow-700 transition-colors"
            onclick={loadImages}
          >
            {$_('viewer.loadImages')}
          </button>

          <!-- Always Load dropdown -->
          <DropdownMenu.Root bind:open={alwaysLoadDropdownOpen}>
            <DropdownMenu.Trigger
              class="px-2 py-1 text-xs font-medium rounded bg-yellow-600 text-white hover:bg-yellow-700 transition-colors flex items-center gap-1"
            >
              {$_('viewer.alwaysLoad')}
              <Icon icon="mdi:chevron-down" class="w-3 h-3" />
            </DropdownMenu.Trigger>
            <DropdownMenu.Content align="end">
              <DropdownMenu.Item onSelect={handleAlwaysLoadAll}>
                <Icon icon="mdi:earth" class="w-4 h-4 mr-2" />
                {$_('viewer.forAll')}
              </DropdownMenu.Item>
              {#if fromEmail}
                <DropdownMenu.Item onSelect={handleAlwaysLoadDomain}>
                  <Icon icon="mdi:domain" class="w-4 h-4 mr-2" />
                  {$_('viewer.forDomain', { values: { domain: extractDomain(fromEmail) || 'this domain' } })}
                </DropdownMenu.Item>
                <DropdownMenu.Item onSelect={handleAlwaysLoadSender}>
                  <Icon icon="mdi:account" class="w-4 h-4 mr-2" />
                  {$_('viewer.forSender', { values: { email: fromEmail } })}
                </DropdownMenu.Item>
              {/if}
            </DropdownMenu.Content>
          </DropdownMenu.Root>
        </div>
      </div>
    {/if}

    <iframe
      bind:this={iframeElement}
      title={$_('aria.emailContent')}
      sandbox="allow-scripts"
      class="w-full border-0 rounded-md {compact ? '' : 'min-h-[100px]'}"
      style="height: 200px; background-color: {iframeOuterBg}; {compact ? 'color-scheme: normal;' : ''}"
    ></iframe>
  {:else if bodyText}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="whitespace-pre-wrap font-sans text-sm text-foreground rounded-md {compact ? '' : 'bg-muted/30 p-4'}"
      onkeydown={(e) => { if (e.key === 'Enter') { const link = (e.target as HTMLElement).closest('a'); if (link?.href) { e.preventDefault(); handleLinkClick(link.href) } } }}
      bind:this={plainTextElement}
      oncontextmenu={(e) => {
        // Same unified menu as the iframe path: without this the native menu
        // replaces ours and the link actions never appear for plain text.
        const link = (e.target as HTMLElement).closest('a')
        const selection = window.getSelection()?.toString() || ''
        if (!link?.href && !selection) return
        e.preventDefault()
        ctxMenuText = selection
        ctxMenuUrl = link?.href || ''
        ctxMenuX = e.clientX
        ctxMenuY = e.clientY
        ctxMenuVisible = true
      }}
      onauxclick={(e) => {
        const link = (e.target as HTMLElement).closest('a')
        if (!link?.href) return
        e.preventDefault()
        if (e.button === 1) handleLinkClick(link.href)
      }}
      onclick={(e) => {
        const link = (e.target as HTMLElement).closest('a')
        if (!link?.href) return
        e.preventDefault()
        handleLinkClick(link.href)
      }}
    >
      <!-- eslint-disable-next-line svelte/no-at-html-tags -- linkifyText escapes user content; only injects safe <a> tags around URLs -->
      {@html linkifyText(bodyText)}
    </div>
  {:else}
    <p class="text-muted-foreground italic">{$_('viewer.noContent')}</p>
  {/if}

  <!-- Link hover tooltip -->
  {#if tooltipVisible && tooltipUrl}
    <div
      class="fixed z-50 px-3 py-1.5 text-xs bg-gray-800 dark:bg-gray-200 text-white dark:text-gray-900 rounded shadow-lg max-w-md truncate pointer-events-none border border-gray-700 dark:border-gray-300"
      style="left: {tooltipX}px; top: {tooltipY + 5}px;"
    >
      {tooltipUrl}
    </div>
  {/if}

  <!-- Context menu (text copy and/or link copy) -->
  {#if ctxMenuVisible}
    <div
      class="fixed z-50 bg-white dark:bg-gray-800 rounded-md shadow-lg border border-gray-200 dark:border-gray-700 py-1 min-w-[160px]"
      style="left: {ctxMenuX}px; top: {ctxMenuY}px;"
      role="menu"
    >
      {#if conversationId && accountId}
        <LaterQueueButton {accountId} {conversationId} variant="menu" onComplete={() => ctxMenuVisible = false} />
      {/if}
      {#if ctxMenuText}
        <button
          class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
          onclick={copyTextToClipboard}
        >
          <Icon icon="mdi:content-copy" class="w-4 h-4" />
          {$_('viewer.copy')}
        </button>
      {/if}
      {#if ctxMenuUrl}
        <button
          class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
          onclick={openLink}
        >
          <Icon icon="mdi:open-in-new" class="w-4 h-4" />
          {$_('viewer.openLink')}
        </button>
        <button
          class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
          onclick={copyLinkToClipboard}
        >
          <Icon icon="mdi:link-variant" class="w-4 h-4" />
          {$_('viewer.copyLink')}
        </button>
        {#if ctxUrlStripped}
          <button
            class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
            onclick={openOriginalLink}
          >
            <Icon icon="mdi:link-off" class="w-4 h-4" />
            {$_('viewer.openOriginalLink')}
          </button>
          <button
            class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
            onclick={copyOriginalLinkToClipboard}
          >
            <Icon icon="mdi:link-variant-off" class="w-4 h-4" />
            {$_('viewer.copyOriginalLink')}
          </button>
        {/if}
        {#if /^https?:\/\//i.test(ctxMenuUrl)}
          <button
            class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
            onclick={() => openViaCloudflareDNS()}
          >
            <Icon icon="mdi:dns" class="w-4 h-4" />
            {$_('viewer.openViaCloudflareDNS')}
          </button>
          {#if ctxUrlStripped}
            <button
              class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
              onclick={() => openViaCloudflareDNS(true)}
            >
              <Icon icon="mdi:dns" class="w-4 h-4" />
              {$_('viewer.resolveOriginalViaCloudflareDNS')}
            </button>
          {/if}
        {/if}
      {/if}
      <button
        class="w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-gray-700 flex items-center gap-2"
        onclick={selectAllInIframe}
      >
        <Icon icon="mdi:select-all" class="w-4 h-4" />
        {$_('viewer.selectAll')}
      </button>
    </div>
  {/if}
  <!-- Cloudflare DNS redirect-chain dialog (ui/dialog gives focus trapping, Escape to close, and focus restoration) -->
  <Dialog.Root bind:open={chainDialogVisible}>
    <Dialog.Content class="relative max-w-lg max-h-[80vh] overflow-y-auto">
      <Dialog.Header>
        <Dialog.Title>{$_('viewer.linkChainTitle')}</Dialog.Title>
        <Dialog.Description>{$_('viewer.linkChainNote')} {$_('viewer.linkChainBrowserDNSNote')}</Dialog.Description>
      </Dialog.Header>
      {#if chainLoading}
        <p class="text-sm text-muted-foreground">{$_('viewer.resolvingLink')}</p>
      {:else}
        {#if chainHops.length > 0}
          <ul class="mb-3 space-y-1">
            {#each chainHops as hop, i (hop.url)}
              <li class="text-xs break-all flex gap-2">
                <span class="text-muted-foreground shrink-0">{i + 1}. [{hop.statusCode}]</span>
                <span class={hop.url === chainFinal ? 'font-medium' : ''}>{hop.url}</span>
              </li>
            {/each}
          </ul>
        {/if}
        {#if chainError}
          <p class="text-xs text-red-600 dark:text-red-400 mb-3 break-all">{chainError}</p>
        {/if}
        <Dialog.Footer>
          {#if chainFinal && chainFinal !== chainOriginal}
            <button class="px-3 py-1.5 text-sm rounded bg-primary text-primary-foreground hover:opacity-90" onclick={() => openChainURL(chainFinal)}>
              {$_('viewer.openFinalURL')}
            </button>
            <button class="px-3 py-1.5 text-sm rounded border border-gray-300 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700" onclick={() => copyChainURL(chainFinal)}>
              {$_('viewer.copyFinalURL')}
            </button>
          {/if}
          <button class="px-3 py-1.5 text-sm rounded border border-gray-300 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-gray-700" onclick={() => openChainURL(chainOriginal, true)}>
            {$_('viewer.openOriginalLink')}
          </button>
          <button class="px-3 py-1.5 text-sm rounded hover:bg-gray-100 dark:hover:bg-gray-700" onclick={() => chainDialogVisible = false}>
            {$_('common.cancel')}
          </button>
        </Dialog.Footer>
      {/if}
    </Dialog.Content>
  </Dialog.Root>
</div>

<!-- Click outside to close context menu -->
{#if ctxMenuVisible}
  <button
    type="button"
    class="fixed inset-0 z-40 cursor-default"
    aria-label={$_('aria.closeContextMenu')}
    onclick={() => ctxMenuVisible = false}
    onkeydown={(e) => { if (e.key === 'Escape') ctxMenuVisible = false }}
  ></button>
{/if}
