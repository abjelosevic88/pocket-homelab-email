// The text of one message: HTML in a sandboxed frame (no scripts, links open outside), or plain text with links and folded quotes.
import { useEffect, useMemo, useRef, useState } from 'react'
import { escapeHTML } from '../lib/mail'

const FONT = `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`

/** The frame's document: the (already sanitized) HTML with a strict policy. */
export function frameDoc(html: string, remote: boolean): string {
  const img = `'self' data:${remote ? ' https: http:' : ''}`
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${img}; style-src 'unsafe-inline'; font-src 'self' data:; media-src 'self' data:">
<base target="_blank"><meta name="color-scheme" content="light">
<style>
html,body{margin:0;padding:0;background:#fff;color:#1f2328}
body{padding:2px 2px 6px;font:14.5px/1.55 ${FONT};word-wrap:break-word;overflow-wrap:anywhere;-webkit-text-size-adjust:100%}
img{max-width:100%;height:auto}
table{max-width:100% !important}
pre{white-space:pre-wrap}
a{color:#0b6bcb}
blockquote{margin:8px 0;padding-left:12px;border-left:3px solid #d0d7de;color:#57606a}
</style></head><body>${html}</body></html>`
}

export function HTMLBody({ html, remote, title }: { html: string; remote: boolean; title: string }) {
  const ref = useRef<HTMLIFrameElement>(null)
  const [h, setH] = useState(120)
  const doc = useMemo(() => frameDoc(html, remote), [html, remote])
  useEffect(() => {
    const f = ref.current
    if (!f) return
    let ro: ResizeObserver | null = null
    const measure = () => {
      const d = f.contentDocument
      if (!d?.documentElement) return
      const next = Math.max(40, Math.ceil(d.documentElement.scrollHeight))
      setH(prev => Math.abs(prev - next) > 2 ? next : prev)
    }
    const onLoad = () => {
      measure()
      const d = f.contentDocument
      if (d?.body) { ro = new ResizeObserver(measure); ro.observe(d.body) }
      d?.querySelectorAll('img').forEach(i => i.addEventListener('load', measure))
    }
    f.addEventListener('load', onLoad)
    return () => { f.removeEventListener('load', onLoad); ro?.disconnect() }
  }, [doc])
  return (
    <div className="mail-paper">
      <iframe ref={ref} title={title} srcDoc={doc} style={{ height: h }} className="mail-frame"
        sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox" referrerPolicy="no-referrer" />
    </div>
  )
}

const URL_RE = /(https?:\/\/[^\s<>"')\]]+[^\s<>"')\].,;:!?])|([\w.+-]+@[\w-]+\.[\w.-]+\w)/g

function linkify(line: string): string {
  return escapeHTML(line).replace(URL_RE, (m, url) => url
    ? `<a href="${m}" target="_blank" rel="noopener noreferrer">${m}</a>`
    : `<a href="mailto:${m}">${m}</a>`)
}

/** Plain text: links clickable, the quoted part of a reply folded away. */
export function TextBody({ text }: { text: string }) {
  const [showQuote, setShowQuote] = useState(false)
  const { main, quote } = useMemo(() => {
    const lines = text.replace(/\r\n/g, '\n').split('\n')
    // the quote starts at "On … wrote:" or the first run of "> " lines that lasts to the end
    let cut = lines.length
    for (let i = 0; i < lines.length; i++) {
      const l = lines[i]
      if (/^(On|Am|Le|Dana|U|El)\b.{5,200}(wrote|schrieb|a écrit|napisao|napisala|je napisao|escribió):\s*$/i.test(l.trim()) || /^-{2,}\s*(Original Message|Forwarded message)/i.test(l.trim())) { cut = i; break }
      if (l.startsWith('>') && lines.slice(i).every(x => x.startsWith('>') || !x.trim())) { cut = i; break }
    }
    return { main: lines.slice(0, cut).join('\n').replace(/\s+$/, ''), quote: lines.slice(cut).join('\n') }
  }, [text])
  const html = (s: string) => s.split('\n').map(l => l.startsWith('>') ? `<span class="q">${linkify(l)}</span>` : linkify(l)).join('\n')
  return (
    <div className="mail-text">
      <div className="mail-text-body" dangerouslySetInnerHTML={{ __html: html(main) || '<span class="muted">(no text)</span>' }} />
      {quote.trim() && <>
        <button type="button" className="quote-toggle" onClick={() => setShowQuote(s => !s)} title={showQuote ? 'Hide the quoted text' : 'Show the quoted text'} aria-expanded={showQuote}>•••</button>
        {showQuote && <div className="mail-text-body quoted" dangerouslySetInnerHTML={{ __html: html(quote) }} />}
      </>}
    </div>
  )
}
