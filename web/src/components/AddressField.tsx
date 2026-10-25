// To / Cc / Bcc: addresses as chips, suggestions from Pocket Contacts and your mail as you type, paste many at once.
import { useEffect, useRef, useState } from 'react'
import { api, V1 } from '../lib/api'
import { parseAddrs, validEmail, who } from '../lib/mail'
import type { Address, Recipient } from '../lib/types'
import { Icon } from './icons'
import { Avatar, useDebounce } from './ui'

export function AddressField({ label, value, onChange, autoFocus, after }: { label: string; value: Address[]; onChange: (v: Address[]) => void; autoFocus?: boolean; after?: React.ReactNode }) {
  const [text, setText] = useState('')
  const [sugg, setSugg] = useState<Recipient[]>([])
  const [hi, setHi] = useState(0)
  const [focus, setFocus] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const dq = useDebounce(text.trim(), 150)
  useEffect(() => {
    if (!dq || !focus) { setSugg([]); return }
    let live = true
    api.get<{ people: Recipient[] }>(`${V1}/recipients?q=${encodeURIComponent(dq)}`).then(r => {
      if (!live) return
      const have = new Set(value.map(a => a.addr.toLowerCase()))
      setSugg((r.people || []).filter(p => !have.has(p.addr.toLowerCase())).slice(0, 6)); setHi(0)
    }).catch(() => {})
    return () => { live = false }
  }, [dq, focus]) // eslint-disable-line react-hooks/exhaustive-deps
  const add = (list: Address[]) => {
    const have = new Set(value.map(a => a.addr.toLowerCase()))
    const fresh = list.filter(a => !have.has(a.addr.toLowerCase()) && (have.add(a.addr.toLowerCase()), true))
    if (fresh.length) onChange([...value, ...fresh])
  }
  const commit = () => {
    if (!text.trim()) return true
    const { list, rest } = parseAddrs(text)
    add(list)
    setText(rest)
    return !rest
  }
  const pick = (p: Recipient) => { add([{ name: p.name, addr: p.addr }]); setText(''); setSugg([]); input.current?.focus() }
  return (
    <div className="addr-field" onClick={() => input.current?.focus()}>
      <span className="addr-label">{label}</span>
      <div className="addr-chips">
        {value.map((a, i) => (
          <span key={a.addr + i} className={`addr-chip ${validEmail(a.addr) ? '' : 'bad'}`} title={a.addr}>
            <span className="ellipsis">{who(a)}</span>
            <button type="button" aria-label={`Remove ${who(a)}`} onClick={e => { e.stopPropagation(); onChange(value.filter((_, j) => j !== i)) }}><Icon name="x" size={11} /></button>
          </span>
        ))}
        <input ref={input} value={text} autoFocus={autoFocus} aria-label={label} autoComplete="off" spellCheck={false}
          onFocus={() => setFocus(true)}
          onBlur={() => { setTimeout(() => { setFocus(false); setSugg([]) }, 150); commit() }}
          onChange={e => { const v = e.target.value; if (/[,;]\s*$/.test(v)) { const { list, rest } = parseAddrs(v); add(list); setText(rest) } else setText(v) }}
          onPaste={e => { const t = e.clipboardData.getData('text'); if (/[,;\n]/.test(t) || validEmail(t)) { e.preventDefault(); const { list, rest } = parseAddrs(text + ' ' + t); add(list); setText(rest) } }}
          onKeyDown={e => {
            if (sugg.length && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); setHi(h => (h + (e.key === 'ArrowDown' ? 1 : -1) + sugg.length) % sugg.length); return }
            if ((e.key === 'Enter' || e.key === 'Tab') && sugg.length && text.trim()) { e.preventDefault(); pick(sugg[hi]); return }
            if (e.key === 'Enter' || e.key === ',' || e.key === ';') { if (text.trim()) { e.preventDefault(); commit() } return }
            if (e.key === 'Backspace' && !text && value.length) onChange(value.slice(0, -1))
          }} />
      </div>
      {after}
      {focus && sugg.length > 0 && (
        <div className="addr-pop" role="listbox">
          {sugg.map((p, i) => (
            <button key={p.addr} type="button" role="option" aria-selected={i === hi} className={`addr-opt ${i === hi ? 'hi' : ''}`} onMouseDown={e => { e.preventDefault(); pick(p) }} onMouseEnter={() => setHi(i)}>
              {p.photo_url ? <img src={p.photo_url} alt="" className="addr-photo" /> : <Avatar name={p.name} email={p.addr} size={26} />}
              <span className="grow ellipsis"><b>{p.name || p.addr}</b>{p.name && <span className="muted"> {p.addr}</span>}</span>
              {p.source === 'contacts' && <Icon name="user" size={12} className="muted" title="From Pocket Contacts" />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
