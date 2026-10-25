// A view of mail (a unified box, one folder, or a search): the list and, next to it, the open conversation.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useMail } from '../lib/mail-context'
import { CATEGORIES, EMPTY, act, useLive, useOnChange } from '../lib/mail'
import type { ActionKind, Category, Message, MessageList as ListT } from '../lib/types'
import { Icon } from '../components/icons'
import { Empty, Segmented, Spinner, modalOpen, useIsMobile, useMediaQuery, useToast } from '../components/ui'
import { MessageRow } from '../components/MessageRow'
import { Reader } from '../components/Reader'
import { MoveDialog, PickTimeDialog, SnoozeMenu } from '../components/dialogs'
import { SearchBox } from '../components/SearchBox'
import { DigestCard } from '../components/DigestCard'

const LIST_W = 'pml-list-w'
const PAGE = 50

export default function Mailbox() {
  const { box: boxParam, folder: folderParam, id: idParam } = useParams()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const navigate = useNavigate()
  const { settings } = useApp()
  const mail = useMail()
  const { toast } = useToast()
  const mobile = useIsMobile()
  const wide = useMediaQuery('(min-width: 768px)')

  const isSearch = loc.pathname.startsWith('/search')
  const q = isSearch ? sp.get('q') || '' : ''
  const folderId = folderParam ? Number(folderParam) : 0
  const folder = folderId ? mail.folder(folderId) : undefined
  const box = isSearch ? '' : folderId ? '' : boxParam || 'inbox'
  const openId = idParam ? Number(idParam) : 0
  const base = isSearch ? '/search' : folderId ? `/f/${folderId}` : `/m/${box}`
  const keep = isSearch ? `?q=${encodeURIComponent(q)}` : sp.get('tab') ? `?tab=${sp.get('tab')}` : ''
  const conversations = settings?.conversations !== false && box !== 'drafts' && box !== 'attachments'
  const isInbox = box === 'inbox' || folder?.role === 'inbox'
  const tabsOn = box === 'inbox' && settings?.inbox_categories !== false
  const tab = (tabsOn ? sp.get('tab') || 'all' : 'all') as Category | 'all'

  const query = useMemo(() => {
    const p = new URLSearchParams()
    if (isSearch) p.set('q', q)
    else if (folderId) p.set('folder', String(folderId))
    else p.set('box', box)
    if (tab !== 'all') p.set('category', tab)
    if (conversations) p.set('conversations', '1')
    return p.toString()
  }, [isSearch, q, folderId, box, tab, conversations])

  // ---- the list ----
  const [list, setList] = useState<Message[]>([])
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [fresh, setFresh] = useState<Message[] | null>(null) // newer rows held back while scrolled down
  const scroller = useRef<HTMLDivElement>(null)
  const listRef = useRef<Message[]>([])
  listRef.current = list
  const qRef = useRef(query)
  qRef.current = query

  const load = useCallback(async (q0: string) => {
    setLoading(true); setError(''); setFresh(null)
    try {
      const r = await api.get<ListT>(`${V1}/messages?${q0}&limit=${PAGE}`)
      if (qRef.current !== q0) return
      setList(r.messages); setMore(r.more)
    } catch (e) { setError((e as Error).message) } finally { if (qRef.current === q0) setLoading(false) }
  }, [])
  useEffect(() => { setSel(new Set()); setCursor(-1); scroller.current?.scrollTo({ top: 0 }); load(query) }, [query, load])

  const loadMore = async () => {
    if (!more || loading) return
    setLoading(true)
    try {
      const r = await api.get<ListT>(`${V1}/messages?${query}&limit=${PAGE}&offset=${listRef.current.length}`)
      setList(l => { const ids = new Set(l.map(m => m.id)); return [...l, ...r.messages.filter(m => !ids.has(m.id))] }); setMore(r.more)
    } catch { /* the next scroll tries again */ } finally { setLoading(false) }
  }

  // quiet refresh: rows change in place, new ones only appear at the top when you are there
  const refreshing = useRef(false)
  const refresh = useCallback(async () => {
    if (refreshing.current) return
    refreshing.current = true
    const q0 = qRef.current
    try {
      const n = Math.min(200, Math.max(PAGE, listRef.current.length))
      const r = await api.get<ListT>(`${V1}/messages?${q0}&limit=${n}`)
      if (qRef.current !== q0) return
      const have = new Set(listRef.current.map(m => m.id))
      const newer = r.messages.filter(m => !have.has(m.id) && (!listRef.current[0] || m.date >= listRef.current[0].date))
      const atTop = (scroller.current?.scrollTop || 0) < 24
      if (newer.length && !atTop) {
        const byId = new Map(r.messages.map(m => [m.id, m]))
        setList(l => l.filter((m, i) => byId.has(m.id) || i >= n).map(m => byId.get(m.id) || m))
        setFresh(r.messages)
      } else {
        setList(r.messages); setMore(r.more); setFresh(null)
      }
    } catch { /* stay as is */ } finally { refreshing.current = false }
  }, [])
  useOnChange(refresh)
  useLive(e => { if (e.type === 'change') refresh() })
  const showFresh = () => { if (fresh) { setList(fresh); setFresh(null); scroller.current?.scrollTo({ top: 0, behavior: 'smooth' }) } }
  const freshCount = fresh ? fresh.filter(m => !list.some(x => x.id === m.id)).length : 0

  // ---- selection and the keyboard cursor ----
  const [sel, setSel] = useState<Set<number>>(new Set())
  const [cursor, setCursor] = useState(-1)
  const anchor = useRef(-1)
  const toggleSel = (i: number, range: boolean) => {
    const m = list[i]
    if (!m) return
    setSel(s => {
      const n = new Set(s)
      if (range && anchor.current >= 0) {
        const [a, b] = [Math.min(anchor.current, i), Math.max(anchor.current, i)]
        for (let k = a; k <= b; k++) n.add(list[k].id)
      } else if (n.has(m.id)) n.delete(m.id)
      else n.add(m.id)
      return n
    })
    anchor.current = i
  }
  useEffect(() => { if (openId) { const i = list.findIndex(m => m.id === openId); if (i >= 0) setCursor(i) } }, [openId, list])

  const open = (m: Message) => navigate(`${base}/${m.id}${keep}`)
  const close = () => navigate(`${base}${keep}`)

  // after a message leaves the view: the next one, the previous one, or back to the list
  const afterLeave = useCallback((ids: number[]) => {
    const l = listRef.current
    const idx = l.findIndex(m => ids.includes(m.id))
    setList(x => x.filter(m => !ids.includes(m.id)))
    setSel(s => { const n = new Set(s); ids.forEach(i => n.delete(i)); return n })
    if (!openId || !ids.includes(openId)) return
    const rest = l.filter(m => !ids.includes(m.id))
    const pref = settings?.after_archive || 'next'
    const target = pref === 'list' || mobile ? undefined : pref === 'previous' ? rest[idx - 1] || rest[idx] : rest[idx] || rest[idx - 1]
    if (target) navigate(`${base}/${target.id}${keep}`, { replace: true }); else close()
  }, [openId, settings?.after_archive, mobile, base, keep]) // eslint-disable-line react-hooks/exhaustive-deps

  const leaves = (a: ActionKind) => {
    if (['archive', 'trash', 'spam', 'delete', 'move', 'snooze'].includes(a)) return true
    if (a === 'inbox' || a === 'notspam') return !isInbox
    if ((a === 'read' && box === 'unread') || (a === 'unstar' && box === 'starred') || (a === 'unsnooze' && box === 'snoozed')) return true
    return false
  }
  const run = async (action: ActionKind, ids: number[], extra: { folder_id?: number; until?: string; category?: Category } = {}) => {
    if (!ids.length) return
    if (leaves(action)) afterLeave(ids)
    else setList(l => l.map(m => ids.includes(m.id) ? patchRow(m, action) : m))
    await act({ ids, action, thread: conversations, ...extra }, toast, { quiet: action === 'read' || action === 'unread' || action === 'star' || action === 'unstar' })
  }

  const [moving, setMoving] = useState<number[] | null>(null)
  const [snoozeAt, setSnoozeAt] = useState<{ ids: number[]; el: DOMRect } | null>(null)
  const [pickTime, setPickTime] = useState<number[] | null>(null)
  const targetIds = () => sel.size ? [...sel] : openId ? [openId] : cursor >= 0 && list[cursor] ? [list[cursor].id] : []

  // ---- keys ----
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"], .composer') || modalOpen() || document.querySelector('.menu, .popover, .select-pop')) return
      const ids = targetIds()
      const step = (d: number) => {
        e.preventDefault()
        const i = Math.max(0, Math.min(list.length - 1, (cursor < 0 ? (d > 0 ? -1 : 0) : cursor) + d))
        setCursor(i)
        document.querySelector(`[data-row="${i}"]`)?.scrollIntoView({ block: 'nearest' })
        if (openId && list[i] && wide) open(list[i])
        if (i >= list.length - 5) loadMore()
      }
      switch (e.key) {
        case 'j': case 'ArrowDown': if (e.key === 'ArrowDown' && openId) return; step(1); break
        case 'k': case 'ArrowUp': if (e.key === 'ArrowUp' && openId) return; step(-1); break
        case 'o': case 'Enter': if (cursor >= 0 && list[cursor]) { e.preventDefault(); open(list[cursor]) } break
        case 'u': case 'Escape': if (openId) { e.preventDefault(); close() } else if (sel.size) { e.preventDefault(); setSel(new Set()) } break
        case 'x': if (cursor >= 0) { e.preventDefault(); toggleSel(cursor, false) } break
        case 'e': e.preventDefault(); run(box === 'archive' ? 'inbox' : 'archive', ids); break
        case '#': case 'Delete': e.preventDefault(); run(box === 'trash' ? 'delete' : 'trash', ids); break
        case '!': e.preventDefault(); run(box === 'spam' ? 'notspam' : 'spam', ids); break
        case 's': { e.preventDefault(); const m = list.find(x => x.id === ids[0]); run(m?.flagged ? 'unstar' : 'star', ids); break }
        case 'I': e.preventDefault(); run('read', ids); break
        case 'U': e.preventDefault(); run('unread', ids); break
        case 'v': if (ids.length) { e.preventDefault(); setMoving(ids) } break
        case 'b': if (ids.length) { e.preventDefault(); setPickTime(ids) } break
        case 'r': case 'a': case 'f': if (openId) { e.preventDefault(); window.dispatchEvent(new CustomEvent('pml:reader-key', { detail: e.key })) } break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  })

  // ---- resizable list ----
  const [listW, setListW] = useState(() => Number(localStorage.getItem(LIST_W)) || 420)
  const drag = (e: React.PointerEvent) => {
    e.preventDefault()
    const x0 = e.clientX, w0 = listW
    const move = (ev: PointerEvent) => setListW(Math.max(300, Math.min(720, w0 + ev.clientX - x0)))
    const up = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up); setListW(w => { localStorage.setItem(LIST_W, String(w)); return w }) }
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', up)
  }

  const split = wide && settings?.reading_pane !== 'off'
  const showList = split || !openId
  const showReader = !!openId
  const several = mail.accounts.length > 1
  const emptyKey = isSearch ? 'search' : folderId ? 'folder' : box
  const [emptyTitle, emptySub] = EMPTY[emptyKey] || EMPTY.folder
  const allSelected = list.length > 0 && sel.size === list.length

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 400) loadMore()
    if (el.scrollTop < 24 && fresh) showFresh()
  }

  return (
    <div className={`mailbox ${split ? 'split' : 'single'} ${settings?.density === 'compact' ? 'compact' : ''}`} style={split ? { gridTemplateColumns: `${listW}px 6px minmax(0, 1fr)` } : undefined}>
      {showList && <section className="list-pane" aria-label="Messages">
        {isSearch && mobile && <div className="list-search"><button type="button" className="btn ghost icon-only" aria-label="Back" onClick={() => navigate('/m/inbox')}><Icon name="arrow-left" size={18} /></button><SearchBox initial={q} autoFocus={!q} /></div>}
        <div className={`list-bar ${sel.size ? 'selecting' : ''}`}>
          <button type="button" className={`sel-box ${allSelected ? 'on' : sel.size ? 'some' : ''}`} aria-label={allSelected ? 'Select none' : 'Select all'} title={allSelected ? 'Select none' : 'Select all'}
            onClick={() => setSel(allSelected || sel.size ? new Set() : new Set(list.map(m => m.id)))} disabled={!list.length}>
            {allSelected ? <Icon name="check" size={12} strokeWidth={3} /> : sel.size ? <Icon name="minus" size={12} strokeWidth={3} /> : null}
          </button>
          {sel.size > 0 ? <>
            <span className="sel-count">{sel.size} selected</span>
            <span className="spacer" />
            <BulkButton icon="archive" label={box === 'archive' ? 'Move to inbox' : 'Archive (e)'} onClick={() => run(box === 'archive' ? 'inbox' : 'archive', [...sel])} />
            <BulkButton icon="trash" label={box === 'trash' ? 'Delete for good' : 'Delete (#)'} onClick={() => run(box === 'trash' ? 'delete' : 'trash', [...sel])} />
            <BulkButton icon="mail-open" label="Mark as read" onClick={() => run('read', [...sel])} />
            <BulkButton icon="mail" label="Mark as unread" onClick={() => run('unread', [...sel])} />
            <BulkButton icon="star" label="Star" onClick={() => run('star', [...sel])} />
            <button type="button" className="btn ghost icon-only sm" aria-label="Snooze" title="Snooze (b)" onClick={e => setSnoozeAt({ ids: [...sel], el: e.currentTarget.getBoundingClientRect() })}><Icon name="alarm" size={16} /></button>
            <BulkButton icon="folder-move" label="Move (v)" onClick={() => setMoving([...sel])} />
            <BulkButton icon="spam" label={box === 'spam' ? 'Not spam' : 'Spam'} onClick={() => run(box === 'spam' ? 'notspam' : 'spam', [...sel])} />
          </> : <>
            {tabsOn ? <div className="cat-tabs scroll-x">
              <Segmented size="sm" value={tab} onChange={v => { const n = new URLSearchParams(sp); if (v === 'all') n.delete('tab'); else n.set('tab', v); setSp(n, { replace: true }) }}
                options={[{ value: 'all' as const, label: 'All' }, ...CATEGORIES.map(c => ({ value: c.id, label: <span className="cat-tab">{c.label}{!!mail.counts[c.id] && <span className="cat-n">{mail.counts[c.id]}</span>}</span> }))]} />
            </div> : <span className="list-title muted small">{isSearch ? (q ? `Results for “${q}”` : 'Search') : ''}</span>}
            <span className="spacer" />
            <span className="list-load">{loading && <Spinner size={13} />}</span>
            <button type="button" className="btn ghost icon-only sm" aria-label="Refresh" title="Check for new mail" onClick={() => { mail.accounts.forEach(a => api.post(`${V1}/accounts/${a.id}/sync`).catch(() => {})); refresh() }}><Icon name="refresh" size={15} /></button>
          </>}
        </div>
        <div className="list-scroll" ref={scroller} onScroll={onScroll}>
          {freshCount > 0 && <button type="button" className="fresh-pill" onClick={showFresh}><Icon name="arrow-up" size={13} />{freshCount} new</button>}
          {isInbox && !isSearch && tab === 'all' && <DigestCard />}
          {error && <div className="callout danger m"><Icon name="alert" size={15} /><span>{error}</span></div>}
          {!loading && !error && list.length === 0 && (
            <Empty title={emptyTitle} icon={isSearch ? 'search' : box === 'inbox' ? 'check-circle' : 'inbox'}>
              {emptySub}
              {!mail.accounts.length && mail.loaded && <div className="mt"><button type="button" className="btn primary" onClick={() => navigate('/settings/accounts?add=1')}><Icon name="plus" size={15} />Add a mail account</button></div>}
            </Empty>
          )}
          <ul className="msg-list" role="listbox" aria-multiselectable="true">
            {list.map((m, i) => (
              <MessageRow key={m.id} m={m} i={i} active={m.id === openId} focused={i === cursor} selected={sel.has(m.id)} selecting={sel.size > 0}
                showAccount={several} highlight={isSearch ? q : ''} showPreview={settings?.show_preview !== false}
                onOpen={() => { setCursor(i); open(m) }} onSelect={range => toggleSel(i, range)}
                onAction={a => run(a, [m.id])} onSnooze={el => setSnoozeAt({ ids: [m.id], el })} box={box} />
            ))}
          </ul>
          {more && <div className="list-more">{loading ? <Spinner /> : <button type="button" className="btn ghost sm" onClick={loadMore}>Show more</button>}</div>}
        </div>
      </section>}
      {split && <div className="pane-resizer" onPointerDown={drag} role="separator" aria-orientation="vertical" aria-label="Resize the list" />}
      {(split || showReader) && <section className="reader-pane" aria-label="Conversation">
        {showReader ? <Reader key={openId} id={openId} conversations={conversations} onClose={close} onLeave={afterLeave}
          onPatch={(ids, a) => setList(l => l.map(m => ids.includes(m.id) ? patchRow(m, a) : m))} />
          : <div className="reader-empty"><Icon name="mail" size={34} /><p>{list.length ? 'Pick a message to read it.' : ''}</p>
            {list.length > 0 && <p className="muted small"><kbd>j</kbd> <kbd>k</kbd> to move, <kbd>o</kbd> to open, <kbd>c</kbd> to write</p>}</div>}
      </section>}
      {moving && <MoveDialog ids={moving} messages={list.filter(m => moving.includes(m.id))} onClose={() => setMoving(null)} onMove={fid => { const ids = moving; setMoving(null); run('move', ids, { folder_id: fid }) }} />}
      {snoozeAt && <SnoozeMenu anchor={snoozeAt.el} onClose={() => setSnoozeAt(null)} onPick={until => { const ids = snoozeAt.ids; setSnoozeAt(null); if (until) run('snooze', ids, { until }); else setPickTime(ids) }} />}
      {pickTime && <PickTimeDialog title="Snooze until" onClose={() => setPickTime(null)} onPick={until => { const ids = pickTime; setPickTime(null); run('snooze', ids, { until }) }} />}
    </div>
  )
}

function BulkButton({ icon, label, onClick }: { icon: string; label: string; onClick: () => void }) {
  return <button type="button" className="btn ghost icon-only sm" aria-label={label} title={label} onClick={onClick}><Icon name={icon} size={16} /></button>
}

/** How a row looks right after an action, before the server answers. */
export function patchRow(m: Message, a: ActionKind): Message {
  switch (a) {
    case 'read': return { ...m, seen: true, thread_unread: 0 }
    case 'unread': return { ...m, seen: false, thread_unread: Math.max(1, m.thread_unread || 0) }
    case 'star': return { ...m, flagged: true }
    case 'unstar': return { ...m, flagged: false }
    case 'pin': return { ...m, pinned: true }
    case 'unpin': return { ...m, pinned: false }
  }
  return m
}
