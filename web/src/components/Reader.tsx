// One conversation: a calm toolbar, each message (latest and unread open), images, invitations, attachments, and what the Pocket apps and AI can do with it.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useMail } from '../lib/mail-context'
import { APP_ICON, APP_NAME, accountColor, act, attURL, fileIcon, fmtAddr, joinAddrs, loadApps, longDate, openCompose, textToHTML, useLive, useOnChange, who } from '../lib/mail'
import { fmtBytes, fmtDateTime, fmtTime } from '../lib/format'
import type { ActionKind, AISummary, AppLink, AppSlug, AppsStatus, Attachment, ContactCard, DayEvent, Invite, MessageFull, Sender, Thread, UnsubscribeResult } from '../lib/types'
import { Icon } from './icons'
import { Avatar, Empty, Loading, Menu, type MenuItem, Popover, Spinner, Switch, rectOf, useIsMobile, useToast, type Anchor } from './ui'
import { HTMLBody, TextBody, frameDoc } from './MessageBody'
import { ExpenseDialog, MoveDialog, PickTimeDialog, PreviewModal, SnoozeMenu, TaskDialog } from './dialogs'
import { EventDialog } from './EventDialog'

export function Reader({ id, conversations, onClose, onLeave, onPatch }: { id: number; conversations: boolean; onClose: () => void; onLeave: (ids: number[]) => void; onPatch: (ids: number[], a: ActionKind) => void }) {
  const { settings } = useApp()
  const mail = useMail()
  const { toast } = useToast()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const [t, setT] = useState<Thread | null>(null)
  const [err, setErr] = useState('')
  const [open, setOpen] = useState<Set<number>>(new Set())
  const [apps, setApps] = useState<AppsStatus | null>(null)
  const scroller = useRef<HTMLDivElement>(null)

  const load = useCallback(async (quiet = false) => {
    try {
      const th = await api.get<Thread>(`${V1}/threads/${id}`)
      setT(prev => {
        if (!prev || !quiet) {
          const ms = th.messages
          const want = new Set(ms.filter((m, i) => i === ms.length - 1 || !m.message.seen || m.message.id === id).map(m => m.message.id))
          if (!conversations) { want.clear(); want.add(id) }
          setOpen(want)
        }
        return th
      })
      setErr('')
    } catch (e) { setErr((e as Error).message) }
  }, [id, conversations])
  useEffect(() => { setT(null); load() }, [load])
  useEffect(() => { loadApps().then(setApps) }, [])
  useOnChange(() => load(true))
  useLive(e => { if (e.type === 'change' && t && e.account_id === t.account_id) load(true) })

  // mark as read after a moment
  useEffect(() => {
    if (!t) return
    const unread = t.messages.filter(m => !m.message.seen).map(m => m.message.id)
    const after = settings?.mark_read_after ?? 1
    if (!unread.length || after < 0) return
    const timer = setTimeout(() => {
      onPatch([id], 'read')
      act({ ids: unread, action: 'read' }, toast, { quiet: true })
    }, after * 1000)
    return () => clearTimeout(timer)
  }, [t?.thread_key, t?.messages.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const latest = t?.messages[t.messages.length - 1]
  const reply = (kind: 'reply' | 'reply_all' | 'forward', m?: MessageFull, body?: string) => {
    const target = m || latest
    if (!target) return
    openCompose({ kind, message_id: target.message.id, body_html: body })
  }
  useEffect(() => {
    const h = (e: Event) => { const k = (e as CustomEvent<string>).detail; reply(k === 'r' ? (settings?.reply_all ? 'reply_all' : 'reply') : k === 'a' ? 'reply_all' : 'forward') }
    window.addEventListener('pml:reader-key', h)
    return () => window.removeEventListener('pml:reader-key', h)
  })

  // ---- toolbar actions (on the whole conversation) ----
  const box = t?.messages.find(m => m.message.id === id)?.message.folder_role || latest?.message.folder_role || ''
  const flagged = !!t?.messages.some(m => m.message.flagged)
  const pinned = !!t?.messages.some(m => m.message.pinned)
  const leaving = (a: ActionKind) => ['archive', 'trash', 'spam', 'delete', 'move', 'snooze', 'notspam', 'inbox'].includes(a)
  const run = async (a: ActionKind, extra: { folder_id?: number; until?: string } = {}) => {
    if (leaving(a) && !(a === 'inbox' && box === 'inbox')) onLeave([id])
    else onPatch([id], a)
    const r = await act({ ids: [id], action: a, thread: conversations, ...extra }, toast, { quiet: ['star', 'unstar', 'read'].includes(a) })
    if (r && a === 'unread') onClose()
  }
  const [snooze, setSnooze] = useState<DOMRect | null>(null)
  const [pickTime, setPickTime] = useState(false)
  const [moving, setMoving] = useState(false)
  const [more, setMore] = useState<Anchor | null>(null)
  const [dialog, setDialog] = useState<null | 'task' | 'expense' | 'event'>(null)
  const [preview, setPreview] = useState<Attachment | null>(null)

  const appOK = (s: AppSlug) => !!apps?.apps[s]?.configured
  const appWhy = (s: AppSlug) => apps?.apps[s]?.detail || (apps?.core ? `Switch on ${APP_NAME[s]} in Settings → Connected apps` : 'Needs Pocket Core or a direct connection (Settings → Connected apps)')
  const linkMade = (msg: string, url?: string) => { toast(msg, 'ok', url ? { label: 'Open', run: () => window.open(url, '_blank', 'noopener') } : undefined); load(true) }
  const postApp = async (path: string, body: object, done: string) => {
    if (!latest) return
    try {
      const r = await api.post<{ link?: AppLink; links?: AppLink[]; message?: string }>(`${V1}/messages/${latest.message.id}/${path}`, body)
      const l = r.link || r.links?.[0]
      linkMade(r.message || done, l?.item_url)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const sender = latest?.message.from
  const setSender = async (patch: Partial<Sender>, msg: string) => {
    if (!sender) return
    try { await api.put(`${V1}/senders/${encodeURIComponent(sender.addr)}`, patch); toast(msg); load(true) } catch (e) { toast((e as Error).message, 'err') }
  }
  const lastSender = latest?.sender
  const moreItems = (): MenuItem[] => {
    const dis = (s: AppSlug) => !appOK(s)
    const hint = (s: AppSlug) => appOK(s) ? undefined : 'not set up'
    return [
      { heading: 'Pocket apps' },
      { label: 'Make a task', icon: 'check-square', disabled: dis('tasks'), hint: hint('tasks'), onSelect: () => setDialog('task') },
      { label: 'Add to calendar', icon: 'calendar-plus', disabled: dis('calendar'), hint: hint('calendar'), onSelect: () => latest?.invite ? postApp('event', { use_invite: true }, 'Added to your calendar') : setDialog('event') },
      { label: 'Save email to Documents', icon: 'file-text', disabled: dis('documents'), hint: hint('documents'), onSelect: () => postApp('save', { app: 'documents', whole: true }, 'Saved to Pocket Documents') },
      { label: 'Save email to Notes', icon: 'note', disabled: dis('notes'), hint: hint('notes'), onSelect: () => postApp('note', {}, 'Saved to Pocket Notes') },
      { label: 'Record an expense', icon: 'wallet', disabled: dis('money'), hint: hint('money'), onSelect: () => setDialog('expense') },
      { label: `Add ${sender ? who(sender) : 'sender'} to Contacts`, icon: 'user-plus', disabled: dis('contacts'), hint: hint('contacts'), onSelect: () => postApp('contact', { addr: sender?.addr, name: sender?.name }, 'Added to Pocket Contacts') },
      'sep',
      { label: pinned ? 'Unpin' : 'Pin to the top', icon: 'pin-top', onSelect: () => run(pinned ? 'unpin' : 'pin') },
      { label: 'Move to a folder…', icon: 'folder-move', hint: 'V', onSelect: () => setMoving(true) },
      { label: box === 'junk' ? 'Not spam' : 'Report spam', icon: 'spam', hint: '!', onSelect: () => run(box === 'junk' ? 'notspam' : 'spam') },
      'sep',
      { label: lastSender?.vip ? 'Not a VIP any more' : 'Mark sender as VIP', icon: 'vip', onSelect: () => setSender({ vip: !lastSender?.vip }, lastSender?.vip ? 'No longer a VIP' : 'VIP: their mail is never missed') },
      { label: lastSender?.muted ? 'Unmute sender' : 'Mute sender', icon: 'volume-off', onSelect: () => setSender({ muted: !lastSender?.muted }, lastSender?.muted ? 'Unmuted' : 'Muted: no notifications from them') },
      { label: 'All mail from this sender', icon: 'search', onSelect: () => sender && navigate(`/search?q=${encodeURIComponent('from:' + sender.addr)}`) },
      'sep',
      { label: 'Show original', icon: 'code', onSelect: () => latest && window.open(`${V1}/messages/${latest.message.id}/raw`, '_blank') },
      { label: 'Print', icon: 'printer', onSelect: () => t && printThread(t) },
    ]
  }

  if (err) return <div className="reader"><ReaderBar onClose={onClose} mobile={mobile} />
    <Empty title="This message is not here any more" icon="mail">{err === 'not found' ? 'It was moved or deleted, maybe in another app.' : err}</Empty></div>
  if (!t) return <div className="reader"><ReaderBar onClose={onClose} mobile={mobile} /><Loading /></div>

  const acct = mail.account(t.account_id)
  const inTrash = box === 'trash'
  return (
    <div className="reader">
      <ReaderBar onClose={onClose} mobile={mobile}>
        <TB icon="archive" label={box === 'archive' || box === 'all' ? 'Move to inbox' : 'Archive (e)'} onClick={() => run(box === 'archive' || box === 'all' ? 'inbox' : 'archive')} />
        <TB icon="trash" label={inTrash ? 'Delete for good' : 'Delete (#)'} onClick={() => run(inTrash ? 'delete' : 'trash')} />
        <TB icon="alarm" label="Snooze (b)" onClick={e => setSnooze(e.currentTarget.getBoundingClientRect())} />
        {!mobile && <TB icon="folder-move" label="Move (v)" onClick={() => setMoving(true)} />}
        <TB icon="mail" label="Mark as unread" onClick={() => run('unread')} />
        <TB icon={flagged ? 'star-fill' : 'star'} label={flagged ? 'Remove star (s)' : 'Star (s)'} on={flagged} onClick={() => run(flagged ? 'unstar' : 'star')} />
        <span className="spacer" />
        {!mobile && <>
          <TB icon="reply" label="Reply (r)" onClick={() => reply('reply')} />
          <TB icon="reply-all" label="Reply all (a)" onClick={() => reply('reply_all')} />
          <TB icon="forward" label="Forward (f)" onClick={() => reply('forward')} />
        </>}
        <button type="button" className="btn ghost icon-only" aria-label="More" title="More" aria-expanded={!!more} onClick={e => setMore(more ? null : rectOf(e.currentTarget))}><Icon name="more" size={18} /></button>
      </ReaderBar>
      <div className="reader-scroll" ref={scroller}>
        <div className="reader-inner">
          <h2 className="reader-subject">{t.subject || '(no subject)'}</h2>
          <div className="reader-meta">
            {acct && mail.accounts.length > 1 && <span className="chip acct-chip"><span className="acct-dot" style={{ background: accountColor(acct) }} />{acct.label || acct.email}</span>}
            {latest?.message.folder_role && latest.message.folder_role !== 'inbox' && <span className="chip">{latest.message.folder_name}</span>}
            {latest?.message.category && latest.message.category !== 'people' && <span className="chip">{latest.message.category}</span>}
            {t.messages.length > 1 && <span className="muted small">{t.messages.length} messages</span>}
          </div>
          {t.links.length > 0 && <div className="link-chips">{t.links.map(l => <LinkChip key={l.id} l={l} onGone={() => load(true)} />)}</div>}
          <AIBar t={t} apps={apps} onReply={(body) => reply('reply', undefined, body)} />
          {t.messages.map((m, i) => (
            <MessageCard key={m.message.id} m={m} open={open.has(m.message.id) || t.messages.length === 1} last={i === t.messages.length - 1}
              onToggle={() => setOpen(s => { const n = new Set(s); if (n.has(m.message.id)) n.delete(m.message.id); else n.add(m.message.id); return n })}
              onReply={k => reply(k, m)} apps={apps} appOK={appOK} appWhy={appWhy} onPreview={setPreview} onReload={() => load(true)}
              onReplace={full => setT(x => x ? { ...x, messages: x.messages.map(y => y.message.id === full.message.id ? full : y) } : x)}
              onLink={linkMade} />
          ))}
          <div className="reader-replies">
            <button type="button" className="btn" onClick={() => reply('reply')}><Icon name="reply" size={15} />Reply</button>
            {latest && (latest.message.to.length + latest.message.cc.length > 1) && <button type="button" className="btn" onClick={() => reply('reply_all')}><Icon name="reply-all" size={15} />Reply all</button>}
            <button type="button" className="btn" onClick={() => reply('forward')}><Icon name="forward" size={15} />Forward</button>
          </div>
        </div>
      </div>
      {more && <Menu anchor={more} align="right" width={270} items={moreItems()} onClose={() => setMore(null)} />}
      {snooze && <SnoozeMenu anchor={snooze} onClose={() => setSnooze(null)} onPick={u => { setSnooze(null); if (u) run('snooze', { until: u }); else setPickTime(true) }} />}
      {pickTime && <PickTimeDialog title="Snooze until" onClose={() => setPickTime(false)} onPick={u => { setPickTime(false); run('snooze', { until: u }) }} />}
      {moving && latest && <MoveDialog ids={[id]} messages={[latest.message]} onClose={() => setMoving(false)} onMove={fid => { setMoving(false); run('move', { folder_id: fid }) }} />}
      {dialog === 'task' && latest && <TaskDialog m={latest.message} onClose={() => setDialog(null)} onDone={url => { setDialog(null); linkMade('Task added to Pocket Tasks', url) }} />}
      {dialog === 'expense' && latest && <ExpenseDialog m={latest.message} aiReady={!!apps?.ai.ready} onClose={() => setDialog(null)} onDone={url => { setDialog(null); linkMade('Expense recorded in Pocket Money', url) }} />}
      {dialog === 'event' && latest && <EventDialog m={latest.message} aiReady={!!apps?.ai.ready} onClose={() => setDialog(null)} onDone={url => { setDialog(null); linkMade('Added to your calendar', url) }} />}
      {preview && <PreviewModal a={preview} onClose={() => setPreview(null)} />}
    </div>
  )
}

function ReaderBar({ onClose, mobile, children }: { onClose: () => void; mobile: boolean; children?: React.ReactNode }) {
  return (
    <div className="reader-bar">
      <button type="button" className="btn ghost icon-only" onClick={onClose} aria-label="Back to the list" title="Back to the list (u)"><Icon name={mobile ? 'arrow-left' : 'x'} size={18} /></button>
      {children}
    </div>
  )
}

function TB({ icon, label, onClick, on }: { icon: string; label: string; onClick: (e: React.MouseEvent<HTMLButtonElement>) => void; on?: boolean }) {
  return <button type="button" className={`btn ghost icon-only ${on ? 'tb-on' : ''}`} aria-label={label} title={label} onClick={onClick}><Icon name={icon} size={17} /></button>
}

function LinkChip({ l, onGone }: { l: AppLink; onGone: () => void }) {
  const verb: Record<string, string> = { documents: 'Saved to Documents', files: 'Saved to Files', photos: 'Saved to Photos', tasks: 'Task', calendar: 'On your calendar', contacts: 'In Contacts', notes: 'Saved to Notes', money: 'Expense' }
  const { toast } = useToast()
  return (
    <span className="link-chip">
      <Icon name={APP_ICON[l.app] || 'link'} size={13} />
      <span className="ellipsis">{verb[l.app] || APP_NAME[l.app]}{l.label ? ` · ${l.label}` : ''}</span>
      {l.item_url && <a href={l.item_url} target="_blank" rel="noreferrer">Open</a>}
      <button type="button" aria-label="Forget this link" title="Forget this link (the item stays)" onClick={async () => { try { await api.del(`${V1}/links/${l.id}`); onGone() } catch (e) { toast((e as Error).message, 'err') } }}><Icon name="x" size={11} /></button>
    </span>
  )
}

// ---- AI: summary, quick replies, draft a reply ----

function AIBar({ t, apps, onReply }: { t: Thread; apps: AppsStatus | null; onReply: (body: string) => void }) {
  const { toast } = useToast()
  const ready = !!apps?.ai.ready
  const latest = t.messages[t.messages.length - 1]
  const [sum, setSum] = useState<AISummary | null>(null)
  const [busy, setBusy] = useState('')
  const [replies, setReplies] = useState<string[] | null>(null)
  const [ask, setAsk] = useState(false)
  const [instr, setInstr] = useState('')
  const people = latest?.message.category === 'people'
  useEffect(() => {
    setSum(null); setReplies(null)
    if (!ready || !people || !latest) return
    let live = true
    api.post<{ replies: string[] }>(`${V1}/ai/suggest`, { message_id: latest.message.id }).then(r => { if (live) setReplies((r.replies || []).slice(0, 3)) }).catch(() => {})
    return () => { live = false }
  }, [t.thread_key, ready]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!latest) return null
  const summarize = async () => {
    setBusy('sum')
    try { setSum(await api.post<AISummary>(`${V1}/ai/summarize`, { message_id: latest.message.id, thread: true })) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const draft = async () => {
    setBusy('draft')
    try { const r = await api.post<{ body_html: string }>(`${V1}/ai/reply`, { message_id: latest.message.id, instruction: instr }); setAsk(false); setInstr(''); onReply(r.body_html) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const why = apps?.ai.detail || 'Connect Pocket AI or your own OpenAI key in Settings → AI'
  return (
    <div className={`ai-bar ${ready ? '' : 'off'}`}>
      <div className="ai-row">
        <Icon name="sparkles" size={14} className="ai-ico" />
        <button type="button" className="btn sm ghost" disabled={!ready || !!busy} title={ready ? 'A short summary and what needs doing' : why} onClick={summarize}>{busy === 'sum' ? <Spinner size={12} /> : null}Summarize</button>
        <button type="button" className="btn sm ghost" disabled={!ready || !!busy} title={ready ? 'Tell the AI what to answer' : why} onClick={() => setAsk(a => !a)}>Draft a reply…</button>
        {ready && replies?.map(r => <button key={r} type="button" className="chip reply-chip" onClick={() => onReply(textToHTML(r))} title="Reply with this">{r}</button>)}
        {!ready && <span className="muted xsmall ai-why">{why}</span>}
      </div>
      {ask && <form className="ai-ask" onSubmit={e => { e.preventDefault(); draft() }}>
        <input value={instr} onChange={e => setInstr(e.target.value)} placeholder="e.g. say yes, Thursday works, ask about the price" autoFocus aria-label="What should the reply say" />
        <button className="btn primary sm" disabled={busy === 'draft'}>{busy === 'draft' ? <Spinner size={12} /> : <Icon name="sparkles" size={13} />}Write it</button>
      </form>}
      {sum && <div className="ai-summary">
        <button type="button" className="ai-x" aria-label="Close the summary" onClick={() => setSum(null)}><Icon name="x" size={13} /></button>
        <p>{sum.summary}</p>
        {sum.points?.length > 0 && <ul>{sum.points.map((p, i) => <li key={i}>{p}</li>)}</ul>}
        {sum.actions?.length > 0 && <><div className="ai-h">To do</div><ul className="ai-actions">{sum.actions.map((p, i) => <li key={i}><Icon name="check-square" size={13} />{p}</li>)}</ul></>}
      </div>}
    </div>
  )
}

// ---- one message ----

function MessageCard({ m, open, last, onToggle, onReply, apps, appOK, appWhy, onPreview, onReload, onReplace, onLink }: {
  m: MessageFull; open: boolean; last: boolean; onToggle: () => void; onReply: (k: 'reply' | 'reply_all' | 'forward') => void
  apps: AppsStatus | null; appOK: (s: AppSlug) => boolean; appWhy: (s: AppSlug) => string; onPreview: (a: Attachment) => void
  onReload: () => void; onReplace: (m: MessageFull) => void; onLink: (msg: string, url?: string) => void
}) {
  const { toast } = useToast()
  const mail = useMail()
  const msg = m.message
  const [card, setCard] = useState<Anchor | null>(null)
  const [details, setDetails] = useState(false)
  const [busyImg, setBusyImg] = useState(false)
  const files = m.attachments.filter(a => !a.inline)
  const fromMe = mail.mine.has(msg.from.addr.toLowerCase())
  const showImages = async (always: boolean) => {
    setBusyImg(true)
    try {
      if (always) await api.put(`${V1}/senders/${encodeURIComponent(msg.from.addr)}`, { allow_images: true })
      onReplace(await api.get<MessageFull>(`${V1}/messages/${msg.id}?images=1`))
      if (always) toast(`Images from ${who(msg.from)} are shown from now on`)
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusyImg(false) }
  }
  const to = [...msg.to, ...msg.cc]
  if (!open) return (
    <div className="msg-card collapsed" onClick={onToggle} role="button" tabIndex={0} onKeyDown={e => { if (e.key === 'Enter') onToggle() }}>
      <Avatar name={msg.from.name} email={msg.from.addr} size={30} />
      <span className={`mc-from ${msg.seen ? '' : 'unread'}`}>{fromMe ? 'Me' : who(msg.from)}</span>
      <span className="mc-snippet ellipsis muted">{msg.snippet}</span>
      {files.length > 0 && <Icon name="paperclip" size={13} className="muted" />}
      <span className="mc-date muted small tnum">{fmtDateTime(msg.date, false)}</span>
    </div>
  )
  return (
    <article className={`msg-card ${last ? 'last' : ''}`}>
      <header className="mc-head">
        <button type="button" className="mc-avatar" onClick={e => setCard(rectOf(e.currentTarget))} aria-label={`About ${who(msg.from)}`}><Avatar name={msg.from.name} email={msg.from.addr} size={38} /></button>
        <div className="mc-who" onClick={onToggle}>
          <div className="mc-line">
            <button type="button" className="mc-name" onClick={e => { e.stopPropagation(); setCard(rectOf(e.currentTarget)) }}>{who(msg.from)}</button>
            {msg.from.name && <span className="mc-addr muted small ellipsis">{msg.from.addr}</span>}
            {m.sender.vip && <span className="badge accent" title="VIP">VIP</span>}
          </div>
          <div className="mc-to muted small">
            <button type="button" className="link-btn muted" onClick={e => { e.stopPropagation(); setDetails(d => !d) }}>
              to {to.length ? to.slice(0, 3).map(a => mail.mine.has(a.addr.toLowerCase()) ? 'me' : who(a)).join(', ') : 'undisclosed'}{to.length > 3 ? ` +${to.length - 3}` : ''} <Icon name={details ? 'chevron-up' : 'chevron-down'} size={11} />
            </button>
          </div>
        </div>
        <span className="mc-date muted small tnum" title={longDate(msg.date)}>{fmtDateTime(msg.date, new Date(msg.date).getFullYear() !== new Date().getFullYear())}</span>
        <button type="button" className="btn ghost icon-only sm" aria-label="Reply" title="Reply" onClick={() => onReply('reply')}><Icon name="reply" size={15} /></button>
      </header>
      {details && <dl className="mc-details small">
        <div><dt>From</dt><dd>{fmtAddr(msg.from)}</dd></div>
        {msg.reply_to?.length > 0 && <div><dt>Reply to</dt><dd>{msg.reply_to.map(fmtAddr).join(', ')}</dd></div>}
        {msg.to.length > 0 && <div><dt>To</dt><dd>{msg.to.map(fmtAddr).join(', ')}</dd></div>}
        {msg.cc.length > 0 && <div><dt>Cc</dt><dd>{msg.cc.map(fmtAddr).join(', ')}</dd></div>}
        {msg.bcc.length > 0 && <div><dt>Bcc</dt><dd>{joinAddrs(msg.bcc)}</dd></div>}
        <div><dt>Date</dt><dd>{longDate(msg.date)}</dd></div>
        {msg.list_id && <div><dt>List</dt><dd>{msg.list_id}</dd></div>}
      </dl>}

      {m.images_blocked > 0 && <div className="banner">
        <Icon name="eye-off" size={15} />
        <span className="grow">{m.images_blocked === 1 ? 'One image is' : `${m.images_blocked} images are`} hidden to keep senders from knowing when you read this.</span>
        <button type="button" className="btn sm" disabled={busyImg} onClick={() => showImages(false)}>Show images</button>
        <button type="button" className="btn sm ghost" disabled={busyImg} onClick={() => showImages(true)}>Always for this sender</button>
      </div>}
      {m.unsubscribe.method !== 'none' && !m.sender.unsubscribed_at && msg.category !== 'people' && <Unsub m={m} onDone={onReload} />}
      {m.invite && <InviteCard m={m} invite={m.invite} ok={appOK('calendar')} why={appWhy('calendar')} onLink={onLink} />}

      <div className="mc-body">
        {m.html ? <HTMLBody html={m.html} remote={m.images_blocked === 0} title={msg.subject} /> : <TextBody text={m.text} />}
      </div>

      {files.length > 0 && <Attachments m={m} files={files} apps={apps} appOK={appOK} appWhy={appWhy} onPreview={onPreview} onLink={onLink} />}
      {card && <SenderCard m={m} anchor={card} onClose={() => setCard(null)} onReload={onReload} contactsOK={appOK('contacts')} onLink={onLink} />}
    </article>
  )
}

function Unsub({ m, onDone }: { m: MessageFull; onDone: () => void }) {
  const { toast } = useToast()
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    try {
      const r = await api.post<UnsubscribeResult>(`${V1}/subscriptions/unsubscribe`, { addr: m.message.from.addr })
      if (r.method === 'url' && r.url) { window.open(r.url, '_blank', 'noopener'); toast('The sender’s page opened in a new tab: finish there.') }
      else toast(r.message || 'Unsubscribed')
      onDone()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="banner soft">
      <Icon name="rss" size={15} />
      <span className="grow">This is a mailing list.</span>
      <button type="button" className="btn sm" disabled={busy} onClick={go}>{busy ? <Spinner size={12} /> : null}Unsubscribe</button>
    </div>
  )
}

function InviteCard({ m, invite, ok, why, onLink }: { m: MessageFull; invite: Invite; ok: boolean; why: string; onLink: (msg: string, url?: string) => void }) {
  const { toast } = useToast()
  const [others, setOthers] = useState<DayEvent[] | null>(null)
  const [busy, setBusy] = useState(false)
  const day = invite.start.slice(0, 10)
  const added = m.links.some(l => l.app === 'calendar')
  useEffect(() => {
    if (!ok || !day) return
    api.get<{ events: DayEvent[] }>(`${V1}/apps/calendar/day?date=${day}`).then(r => setOthers((r.events || []).filter(e => e.title !== invite.summary))).catch(() => {})
  }, [ok, day, invite.summary])
  const start = new Date(invite.start), end = invite.end ? new Date(invite.end) : null
  const when = invite.all_day ? start.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
    : `${start.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}, ${fmtTime(start)}${end ? ` – ${fmtTime(end)}` : ''}`
  const add = async () => {
    setBusy(true)
    try { const r = await api.post<{ link?: AppLink }>(`${V1}/messages/${m.message.id}/event`, { use_invite: true }); onLink('Added to your calendar', r.link?.item_url) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const cancelled = invite.method === 'CANCEL'
  return (
    <div className={`invite ${cancelled ? 'cancelled' : ''}`}>
      <div className="invite-date"><span>{start.toLocaleDateString(undefined, { month: 'short' })}</span><b>{start.getDate()}</b></div>
      <div className="grow">
        <div className="invite-title">{cancelled && <span className="badge danger">Cancelled</span>} {invite.summary || 'Invitation'}</div>
        <div className="small">{when}</div>
        {invite.location && <div className="small muted row"><Icon name="pin" size={12} />{invite.location}</div>}
        {invite.organizer?.addr && <div className="xsmall muted">From {who(invite.organizer)}{invite.attendees?.length ? ` · ${invite.attendees.length} invited` : ''}</div>}
        {others && <div className="xsmall muted mt-s">{others.length === 0 ? 'Nothing else on your calendar that day.' : `You have ${others.length} other ${others.length === 1 ? 'event' : 'events'} that day: ${others.slice(0, 3).map(e => (e.all_day ? '' : fmtTime(new Date(e.start)) + ' ') + e.title).join(', ')}${others.length > 3 ? '…' : ''}`}</div>}
      </div>
      {!cancelled && <button type="button" className="btn sm primary" disabled={!ok || busy || added} title={ok ? '' : why} onClick={add}>{added ? <><Icon name="check" size={13} />On your calendar</> : busy ? <Spinner size={12} /> : <><Icon name="calendar-plus" size={13} />Add to calendar</>}</button>}
    </div>
  )
}

function Attachments({ m, files, appOK, appWhy, onPreview, onLink }: { m: MessageFull; files: Attachment[]; apps: AppsStatus | null; appOK: (s: AppSlug) => boolean; appWhy: (s: AppSlug) => string; onPreview: (a: Attachment) => void; onLink: (msg: string, url?: string) => void }) {
  const { toast } = useToast()
  const [menu, setMenu] = useState<{ a: Attachment | null; at: Anchor } | null>(null)
  const total = files.reduce((n, a) => n + a.size, 0)
  const save = async (app: 'documents' | 'files' | 'photos', list: Attachment[]) => {
    try {
      const r = await api.post<{ links?: AppLink[]; message?: string }>(`${V1}/messages/${m.message.id}/save`, { app, attachment_ids: list.map(a => a.id) })
      onLink(r.message || `Saved to ${APP_NAME[app]}`, r.links?.[0]?.item_url)
    } catch (e) { toast((e as Error).message, 'err') }
  }
  const saveItems = (list: Attachment[]): MenuItem[] => {
    const images = list.every(a => a.mime.startsWith('image/'))
    const item = (app: 'documents' | 'files' | 'photos', label: string, icon: string) => ({ label, icon, disabled: !appOK(app), hint: appOK(app) ? undefined : 'not set up', onSelect: () => save(app, list) })
    return [
      { heading: list.length > 1 ? `Save ${list.length} files to` : 'Save to' },
      item('documents', 'Pocket Documents', 'file-text'),
      item('files', 'Pocket Files', 'folder'),
      ...(images ? [item('photos', 'Photos', 'image')] : []),
      ...(!appOK('documents') && !appOK('files') ? [{ heading: appWhy('documents') }] : []),
    ]
  }
  return (
    <div className="atts">
      <div className="atts-head small muted">
        <Icon name="paperclip" size={13} />{files.length} {files.length === 1 ? 'attachment' : 'attachments'} · {fmtBytes(total)}
        <span className="spacer" />
        {files.length > 1 && <button type="button" className="link-btn small" onClick={e => setMenu({ a: null, at: rectOf(e.currentTarget) })}>Save all to…</button>}
      </div>
      <div className="atts-grid">
        {files.map(a => {
          const img = a.mime.startsWith('image/')
          const canPreview = img || a.mime === 'application/pdf'
          return (
            <div key={a.id} className="att">
              <button type="button" className="att-main" onClick={() => canPreview ? onPreview(a) : window.open(attURL(a.id, true))} title={canPreview ? 'Preview' : 'Download'}>
                {img ? <span className="att-thumb" style={{ backgroundImage: `url(${attURL(a.id)})` }} /> : <span className="att-ico"><Icon name={fileIcon(a.mime, a.filename)} size={18} /></span>}
                <span className="att-text"><span className="att-name ellipsis">{a.filename || 'file'}</span><span className="att-size">{fmtBytes(a.size)}</span></span>
              </button>
              <div className="att-acts">
                <a className="btn ghost icon-only sm" href={attURL(a.id, true)} aria-label="Download" title="Download"><Icon name="download" size={14} /></a>
                <button type="button" className="btn ghost icon-only sm" aria-label="Save to a Pocket app" title="Save to…" onClick={e => setMenu({ a, at: rectOf(e.currentTarget) })}><Icon name="upload" size={14} /></button>
              </div>
            </div>
          )
        })}
      </div>
      {menu && <Menu anchor={menu.at} align="right" width={240} items={saveItems(menu.a ? [menu.a] : files)} onClose={() => setMenu(null)} />}
    </div>
  )
}

function SenderCard({ m, anchor, onClose, onReload, contactsOK, onLink }: { m: MessageFull; anchor: Anchor; onClose: () => void; onReload: () => void; contactsOK: boolean; onLink: (msg: string, url?: string) => void }) {
  const { toast } = useToast()
  const navigate = useNavigate()
  const from = m.message.from
  const [card, setCard] = useState<ContactCard | null>(null)
  const [sd, setSd] = useState<Sender>(m.sender)
  useEffect(() => {
    if (!contactsOK) return
    api.get<ContactCard>(`${V1}/apps/contacts/card?addr=${encodeURIComponent(from.addr)}`).then(setCard).catch(() => {})
  }, [from.addr, contactsOK])
  const put = async (patch: Partial<Sender>) => {
    const next = { ...sd, ...patch }
    setSd(next)
    try { await api.put(`${V1}/senders/${encodeURIComponent(from.addr)}`, patch); onReload() } catch (e) { setSd(sd); toast((e as Error).message, 'err') }
  }
  const addContact = async () => {
    try { const r = await api.post<{ link?: AppLink }>(`${V1}/messages/${m.message.id}/contact`, { addr: from.addr, name: from.name }); onClose(); onLink('Added to Pocket Contacts', r.link?.item_url) } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <Popover anchor={anchor} onClose={onClose} prefer="below" width={320} label="Sender">
      <div className="sender-card">
        <div className="row">
          {card?.photo_url ? <img src={card.photo_url} alt="" className="sender-photo" /> : <Avatar name={from.name} email={from.addr} size={44} />}
          <div className="grow">
            <div className="sender-name">{card?.found ? card.name : who(from)}</div>
            <div className="muted small ellipsis">{from.addr}</div>
            {card?.found && (card.title || card.org) && <div className="small">{[card.title, card.org].filter(Boolean).join(', ')}</div>}
            {card?.found && card.phone && <div className="small"><a href={`tel:${card.phone}`}>{card.phone}</a></div>}
          </div>
        </div>
        <div className="sender-toggles">
          <label className="check-row"><Switch size="sm" checked={sd.vip} onChange={v => put({ vip: v })} label="VIP" />VIP — never miss their mail</label>
          <label className="check-row"><Switch size="sm" checked={sd.allow_images} onChange={v => put({ allow_images: v })} label="Images" />Always show their images</label>
          <label className="check-row"><Switch size="sm" checked={sd.muted} onChange={v => put({ muted: v })} label="Mute" />Mute notifications</label>
        </div>
        <div className="row wrap">
          <button type="button" className="btn sm" onClick={() => { onClose(); openCompose({ kind: 'new', to: [from] }) }}><Icon name="edit" size={13} />Write</button>
          <button type="button" className="btn sm" onClick={() => { onClose(); navigate(`/search?q=${encodeURIComponent('from:' + from.addr)}`) }}><Icon name="search" size={13} />Their mail</button>
          {card?.found ? card.url && <a className="btn sm" href={card.url} target="_blank" rel="noreferrer"><Icon name="user" size={13} />Contact</a>
            : <button type="button" className="btn sm" disabled={!contactsOK} title={contactsOK ? '' : 'Connect Pocket Contacts in Settings → Connected apps'} onClick={addContact}><Icon name="user-plus" size={13} />Add to Contacts</button>}
        </div>
      </div>
    </Popover>
  )
}

/** Print the open conversation (each message with its header). */
function printThread(t: Thread) {
  const w = window.open('', '_blank')
  if (!w) return
  const esc = (s: string) => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c] as string))
  const parts = t.messages.map(m => `<div style="border-top:1px solid #ddd;padding:12px 0;font:13px sans-serif">
    <b>${esc(fmtAddr(m.message.from))}</b><br>To: ${esc(m.message.to.map(fmtAddr).join(', '))}<br>${esc(longDate(m.message.date))}</div>
    <div>${m.html || `<pre style="white-space:pre-wrap;font:14px sans-serif">${esc(m.text)}</pre>`}</div>`).join('')
  w.document.write(frameDoc(`<h2 style="font:600 20px sans-serif">${esc(t.subject)}</h2>${parts}`, true).replace('<body>', '<body onload="print()">').replace("default-src 'none';", "default-src 'none'; script-src 'unsafe-inline';"))
  w.document.close()
}
