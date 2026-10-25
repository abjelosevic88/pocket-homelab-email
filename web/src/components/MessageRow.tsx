// One row of the message list: who, subject, a preview, when, and quick actions on hover.
import { memo } from 'react'
import { useMail } from '../lib/mail-context'
import { accountColor, mailDate, rowSender, searchWords } from '../lib/mail'
import type { ActionKind, Message } from '../lib/types'
import { Icon } from './icons'
import { Avatar } from './ui'

/** Wrap the searched words in <mark>. */
export function Highlight({ text, q }: { text: string; q: string }) {
  const words = q ? searchWords(q) : []
  if (!words.length || !text) return <>{text}</>
  const re = new RegExp(`(${words.map(w => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')})`, 'gi')
  const parts = text.split(re)
  return <>{parts.map((p, i) => i % 2 ? <mark key={i}>{p}</mark> : p)}</>
}

export const MessageRow = memo(function MessageRow({ m, i, active, focused, selected, selecting, showAccount, highlight, showPreview, box, onOpen, onSelect, onAction, onSnooze }: {
  m: Message; i: number; active: boolean; focused: boolean; selected: boolean; selecting: boolean; showAccount: boolean; highlight: string; showPreview: boolean; box: string
  onOpen: () => void; onSelect: (range: boolean) => void; onAction: (a: ActionKind) => void; onSnooze: (el: DOMRect) => void
}) {
  const mail = useMail()
  const unread = m.thread_count ? (m.thread_unread || 0) > 0 : !m.seen
  const acct = showAccount ? mail.account(m.account_id) : undefined
  const sender = rowSender(m, mail.mine)
  const isSent = sender.startsWith('To:') || sender === 'Me'
  const snoozed = m.snooze_until && new Date(m.snooze_until) > new Date()
  const stop = (e: React.SyntheticEvent) => e.stopPropagation()
  return (
    <li data-row={i} role="option" aria-selected={selected || active}
      className={`msg-row ${unread ? 'unread' : ''} ${active ? 'active' : ''} ${focused ? 'focused' : ''} ${selected ? 'selected' : ''} ${selecting ? 'selecting' : ''}`}
      onClick={e => { if (e.shiftKey || e.metaKey || e.ctrlKey || selecting) { e.preventDefault(); onSelect(e.shiftKey) } else onOpen() }}>
      {acct && <span className="acct-stripe" style={{ background: accountColor(acct) }} title={acct.label || acct.email} />}
      <button type="button" className="row-avatar" aria-label={selected ? 'Unselect' : 'Select'} onClick={e => { stop(e); onSelect(e.shiftKey) }}>
        {selected || selecting ? <span className={`sel-box ${selected ? 'on' : ''}`}>{selected && <Icon name="check" size={12} strokeWidth={3} />}</span>
          : <Avatar name={isSent ? m.to[0]?.name : m.from.name} email={isSent ? m.to[0]?.addr : m.from.addr} size={34} />}
      </button>
      <div className="row-main">
        <div className="row-top">
          {unread && <span className="unread-dot" aria-label="Unread" />}
          <span className="row-from ellipsis"><Highlight text={sender} q={highlight} /></span>
          {(m.thread_count || 0) > 1 && <span className="row-count">{m.thread_count}</span>}
          <span className="row-icons">
            {m.pinned && <Icon name="pin-top" size={13} className="muted" title="Pinned" />}
            {m.has_invite && <Icon name="calendar" size={13} className="muted" title="Invitation" />}
            {m.has_attachments && <Icon name="paperclip" size={13} className="muted" title="Attachments" />}
          </span>
          <span className="row-date tnum">{snoozed ? <span className="snooze-tag"><Icon name="alarm" size={11} />{mailDate(m.snooze_until)}</span> : mailDate(m.date)}</span>
        </div>
        <div className="row-sub">
          <span className="row-subject ellipsis"><Highlight text={m.subject || '(no subject)'} q={highlight} /></span>
          {m.flagged && <Icon name="star-fill" size={13} className="star-on" title="Starred" />}
        </div>
        {showPreview && m.snippet && <div className="row-snippet"><Highlight text={m.snippet} q={highlight} /></div>}
      </div>
      <div className="row-quick" onClick={stop}>
        <button type="button" className="qa" aria-label={box === 'archive' ? 'Move to inbox' : 'Archive'} title={box === 'archive' ? 'Move to inbox' : 'Archive (e)'} onClick={() => onAction(box === 'archive' ? 'inbox' : 'archive')}><Icon name={box === 'archive' ? 'inbox' : 'archive'} size={15} /></button>
        <button type="button" className="qa" aria-label="Delete" title="Delete (#)" onClick={() => onAction(box === 'trash' ? 'delete' : 'trash')}><Icon name="trash" size={15} /></button>
        <button type="button" className="qa" aria-label={unread ? 'Mark as read' : 'Mark as unread'} title={unread ? 'Mark as read' : 'Mark as unread'} onClick={() => onAction(unread ? 'read' : 'unread')}><Icon name={unread ? 'mail-open' : 'mail'} size={15} /></button>
        <button type="button" className="qa" aria-label="Snooze" title="Snooze (b)" onClick={e => onSnooze(e.currentTarget.getBoundingClientRect())}><Icon name="alarm" size={15} /></button>
        <button type="button" className={`qa ${m.flagged ? 'on' : ''}`} aria-label={m.flagged ? 'Remove star' : 'Star'} title="Star (s)" onClick={() => onAction(m.flagged ? 'unstar' : 'star')}><Icon name={m.flagged ? 'star-fill' : 'star'} size={15} /></button>
      </div>
    </li>
  )
})
