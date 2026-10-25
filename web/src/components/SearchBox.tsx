// The search field: words and Gmail-style operators, people as you type, a help card with the operators.
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Recipient } from '../lib/types'
import { Icon } from './icons'
import { Avatar, useDebounce } from './ui'

export const OPERATORS: [string, string][] = [
  ['from:ana', 'From someone (name or address)'],
  ['to:marko@x.org', 'Sent to someone'],
  ['subject:invoice', 'Words in the subject'],
  ['has:attachment', 'With attachments'],
  ['filename:pdf', 'An attachment named like this'],
  ['is:unread', 'Unread (or is:read, is:starred)'],
  ['in:sent', 'In a view: inbox, sent, archive, spam, trash…'],
  ['after:2026-01-01', 'On or after a date (before: too)'],
  ['newer_than:7d', 'In the last 7 days (d, w, m, y; older_than: too)'],
  ['account:work', 'In one account'],
]

export function SearchBox({ initial = '', autoFocus = false, onDone, className = '' }: { initial?: string; autoFocus?: boolean; onDone?: () => void; className?: string }) {
  const navigate = useNavigate()
  const [q, setQ] = useState(initial)
  const [open, setOpen] = useState(false)
  const [help, setHelp] = useState(false)
  const [people, setPeople] = useState<Recipient[]>([])
  const [hi, setHi] = useState(-1)
  const ref = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { setQ(initial) }, [initial])
  const last = q.split(/\s+/).pop() || ''
  const dq = useDebounce(last, 180)
  useEffect(() => {
    if (!open || dq.length < 2 || dq.includes(':')) { setPeople([]); return }
    let live = true
    api.get<{ people: Recipient[] }>(`${V1}/recipients?q=${encodeURIComponent(dq)}`).then(r => { if (live) setPeople((r.people || []).slice(0, 5)) }).catch(() => {})
    return () => { live = false }
  }, [dq, open])
  useEffect(() => setHi(-1), [people])
  useEffect(() => {
    const h = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) { setOpen(false); setHelp(false) } }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])
  useEffect(() => {
    const focus = () => { input.current?.focus(); input.current?.select() }
    window.addEventListener('pml:search', focus)
    return () => window.removeEventListener('pml:search', focus)
  }, [])
  const go = (text: string) => {
    const t = text.trim()
    setOpen(false); setHelp(false)
    input.current?.blur()
    if (!t) return
    navigate(`/search?q=${encodeURIComponent(t)}`)
    onDone?.()
  }
  const pickPerson = (p: Recipient) => {
    const words = q.split(/\s+/); words.pop()
    const next = [...words, `from:${p.addr}`].join(' ').trim() + ' '
    setQ(next); go(next)
  }
  return (
    <div className={`search-box ${className}`} ref={ref}>
      <Icon name="search" size={16} className="search-ico" />
      <input ref={input} value={q} autoFocus={autoFocus} placeholder="Search mail" aria-label="Search mail" enterKeyHint="search"
        onFocus={() => setOpen(true)} onChange={e => { setQ(e.target.value); setOpen(true) }}
        onKeyDown={e => {
          if (e.key === 'ArrowDown' && people.length) { e.preventDefault(); setHi(h => Math.min(people.length - 1, h + 1)) }
          else if (e.key === 'ArrowUp' && people.length) { e.preventDefault(); setHi(h => Math.max(-1, h - 1)) }
          else if (e.key === 'Enter') { e.preventDefault(); if (hi >= 0 && people[hi]) pickPerson(people[hi]); else go(q) }
          else if (e.key === 'Escape') { e.preventDefault(); setOpen(false); setHelp(false); input.current?.blur() }
        }} />
      {q && <button type="button" className="search-clear" aria-label="Clear" onClick={() => { setQ(''); input.current?.focus() }}><Icon name="x" size={14} /></button>}
      <button type="button" className={`search-help-btn ${help ? 'on' : ''}`} aria-label="Search help" title="How to search" onClick={() => setHelp(h => !h)}><Icon name="help" size={15} /></button>
      {(help || (open && people.length > 0)) && (
        <div className="search-pop" role="listbox">
          {!help && people.map((p, i) => (
            <button key={p.addr} type="button" className={`search-person ${i === hi ? 'hi' : ''}`} onMouseEnter={() => setHi(i)} onClick={() => pickPerson(p)}>
              <Avatar name={p.name} email={p.addr} size={24} />
              <span className="grow ellipsis"><b>{p.name || p.addr}</b>{p.name && <span className="muted"> · {p.addr}</span>}</span>
              <span className="muted xsmall">Mail from</span>
            </button>
          ))}
          {help && <div className="search-help">
            <div className="search-help-h">Search by words, or narrow it down:</div>
            {OPERATORS.map(([op, what]) => (
              <button key={op} type="button" className="search-op" onClick={() => { const n = (q.trim() + ' ' + op.split(':')[0] + ':').trim(); setQ(n); setHelp(false); input.current?.focus() }}>
                <code>{op}</code><span className="muted">{what}</span>
              </button>
            ))}
            <div className="muted xsmall" style={{ padding: '6px 10px 2px' }}>Combine them: <code>from:ana has:attachment newer_than:1m</code></div>
          </div>}
        </div>
      )}
    </div>
  )
}
