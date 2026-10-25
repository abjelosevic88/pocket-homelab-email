// A small rich-text editor for mail: bold, italic, underline, links, lists, quotes. Pasted text is cleaned to safe HTML.
import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react'
import { Icon } from './icons'

export interface EditorHandle { focus: () => void; insertHTML: (html: string) => void; html: () => string; setHTML: (html: string) => void }

const ALLOWED = new Set(['P', 'BR', 'B', 'STRONG', 'I', 'EM', 'U', 'A', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'DIV', 'SPAN', 'PRE', 'CODE', 'H1', 'H2', 'H3', 'H4', 'TABLE', 'THEAD', 'TBODY', 'TR', 'TD', 'TH', 'HR', 'S', 'SUB', 'SUP', 'IMG'])
const DROP = new Set(['SCRIPT', 'STYLE', 'IFRAME', 'OBJECT', 'EMBED', 'FORM', 'INPUT', 'BUTTON', 'SELECT', 'TEXTAREA', 'META', 'LINK', 'TITLE', 'HEAD', 'SVG'])

/** Keep only harmless markup: no scripts, no styles, no event handlers; links stay links. */
export function cleanHTML(html: string): string {
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  const walk = (el: Element) => {
    for (const c of [...el.children]) {
      if (DROP.has(c.tagName)) { c.remove(); continue }
      walk(c)
      if (!ALLOWED.has(c.tagName)) { c.replaceWith(...c.childNodes); continue }
      for (const a of [...c.attributes]) {
        const keep = (c.tagName === 'A' && a.name === 'href' && /^(https?:|mailto:|tel:)/i.test(a.value))
          || (c.tagName === 'IMG' && a.name === 'src' && /^(https?:|data:image\/|\/api\/)/i.test(a.value))
          || (c.tagName === 'IMG' && a.name === 'alt')
          || (a.name === 'class' && /^(signature|quote|pml-quote)$/.test(a.value))
          || a.name === 'data-signature' || a.name === 'data-quote'
        if (!keep) c.removeAttribute(a.name)
      }
      if (c.tagName === 'A') { c.setAttribute('target', '_blank'); c.setAttribute('rel', 'noopener noreferrer') }
    }
  }
  walk(doc.body)
  return doc.body.innerHTML
}

const exec = (cmd: string, value?: string) => document.execCommand(cmd, false, value)

export const RichEditor = forwardRef<EditorHandle, { value: string; onChange: (html: string) => void; placeholder?: string; onFiles?: (files: File[]) => void; onSubmit?: () => void; minHeight?: number; toolbar?: boolean; className?: string; ariaLabel?: string }>(
  function RichEditor({ value, onChange, placeholder, onFiles, onSubmit, minHeight = 160, toolbar = true, className = '', ariaLabel = 'Message' }, ref) {
    const el = useRef<HTMLDivElement>(null)
    const last = useRef(value)
    useEffect(() => {
      if (el.current && value !== last.current) { el.current.innerHTML = value; last.current = value }
    }, [value])
    useEffect(() => { if (el.current) el.current.innerHTML = value }, []) // eslint-disable-line react-hooks/exhaustive-deps
    const emit = () => { const h = el.current?.innerHTML || ''; last.current = h; onChange(h) }
    useImperativeHandle(ref, () => ({
      focus: () => el.current?.focus(),
      html: () => el.current?.innerHTML || '',
      setHTML: (h: string) => { if (el.current) { el.current.innerHTML = h; emit() } },
      insertHTML: (h: string) => {
        const node = el.current
        if (!node) return
        const sel = window.getSelection()
        if (!sel || !sel.rangeCount || !node.contains(sel.anchorNode)) {
          // no caret in the editor: put it above the signature / quote
          const stop = node.querySelector('[data-signature], .signature, blockquote, [data-quote]')
          const tmp = document.createElement('div'); tmp.innerHTML = h
          const frag = document.createDocumentFragment(); while (tmp.firstChild) frag.appendChild(tmp.firstChild)
          if (stop) node.insertBefore(frag, stop); else node.appendChild(frag)
        } else exec('insertHTML', h)
        emit()
      },
    }))
    const link = () => {
      const url = window.prompt('Link address', 'https://')
      if (url && /^(https?:|mailto:)/i.test(url)) exec('createLink', url)
      emit()
    }
    const btn = (icon: string, label: string, run: () => void) => (
      <button type="button" className="ed-btn" aria-label={label} title={label} onMouseDown={e => { e.preventDefault(); run(); emit() }}><Icon name={icon} size={15} /></button>
    )
    return (
      <div className={`editor ${className}`}>
        {toolbar && <div className="ed-bar" role="toolbar" aria-label="Formatting">
          {btn('bold', 'Bold (⌘B)', () => exec('bold'))}
          {btn('italic', 'Italic (⌘I)', () => exec('italic'))}
          {btn('underline', 'Underline (⌘U)', () => exec('underline'))}
          <span className="ed-sep" />
          {btn('link', 'Link', link)}
          {btn('list-ul', 'Bulleted list', () => exec('insertUnorderedList'))}
          {btn('list-ol', 'Numbered list', () => exec('insertOrderedList'))}
          {btn('quote', 'Quote', () => exec('formatBlock', 'blockquote'))}
          <span className="ed-sep" />
          {btn('eraser', 'Clear formatting', () => { exec('removeFormat'); exec('formatBlock', 'p') })}
        </div>}
        <div ref={el} className="ed-area" contentEditable suppressContentEditableWarning role="textbox" aria-multiline="true" aria-label={ariaLabel}
          data-placeholder={placeholder} style={{ minHeight }} onInput={emit} onBlur={emit}
          onKeyDown={e => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); emit(); onSubmit?.() }
            if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); e.stopPropagation(); link() }
          }}
          onPaste={e => {
            const files = [...e.clipboardData.files]
            if (files.length && onFiles) { e.preventDefault(); onFiles(files); return }
            const html = e.clipboardData.getData('text/html')
            const text = e.clipboardData.getData('text/plain')
            e.preventDefault()
            if (html) exec('insertHTML', cleanHTML(html))
            else exec('insertText', text)
            emit()
          }}
          onDrop={e => {
            const files = [...e.dataTransfer.files]
            if (files.length && onFiles) { e.preventDefault(); onFiles(files) }
          }} />
      </div>
    )
  })
