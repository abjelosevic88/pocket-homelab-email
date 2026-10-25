// "Catch me up": a short AI summary of what came in from people, collapsed until you ask.
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useMail } from '../lib/mail-context'
import { loadApps } from '../lib/mail'
import type { AIDigest } from '../lib/types'
import { Icon } from './icons'
import { Spinner, useToast } from './ui'

const HIDE = 'pml-digest-hidden'

export function DigestCard() {
  const { settings } = useApp()
  const { counts } = useMail()
  const { toast } = useToast()
  const navigate = useNavigate()
  const [ready, setReady] = useState(false)
  const [d, setD] = useState<AIDigest | null>(null)
  const [busy, setBusy] = useState(false)
  const [hidden, setHidden] = useState(() => sessionStorage.getItem(HIDE) === String(new Date().toDateString()))
  useEffect(() => { loadApps().then(a => setReady(a.ai.ready)) }, [])
  const unread = counts.people || 0
  if (!settings?.ai_digest || !ready || hidden || (unread < 2 && !d)) return null
  const run = async () => {
    setBusy(true)
    try { setD(await api.post<AIDigest>(`${V1}/ai/digest`, {})) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const hide = () => { sessionStorage.setItem(HIDE, new Date().toDateString()); setHidden(true) }
  return (
    <div className={`digest ${d ? 'open' : ''}`}>
      <div className="digest-head">
        <Icon name="sparkles" size={14} className="ai-ico" />
        {d ? <b className="grow">Catch me up</b> : <button type="button" className="link-btn grow digest-ask" onClick={run} disabled={busy}>{busy ? <><Spinner size={12} /> Reading your mail…</> : <>Catch me up on {unread} unread {unread === 1 ? 'message' : 'messages'} from people</>}</button>}
        <button type="button" className="btn ghost icon-only sm" aria-label="Hide for today" title="Hide for today" onClick={hide}><Icon name="x" size={13} /></button>
      </div>
      {d && <div className="digest-body">
        <p>{d.digest}</p>
        {d.items?.length > 0 && <ul>{d.items.map(i => <li key={i.message_id}><button type="button" className="link-btn" onClick={() => navigate(`/m/inbox/${i.message_id}`)}><b>{i.from}</b> · {i.subject}</button>{i.why && <span className="muted"> — {i.why}</span>}</li>)}</ul>}
      </div>}
    </div>
  )
}
