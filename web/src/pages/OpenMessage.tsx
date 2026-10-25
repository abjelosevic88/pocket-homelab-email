// /message/:id — a link to one message (from a notification or another app) opens its conversation in the right view.
import { useEffect, useState } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { MessageFull } from '../lib/types'
import { Empty, Loading } from '../components/ui'

const VIEW: Record<string, string> = { inbox: 'inbox', sent: 'sent', drafts: 'drafts', junk: 'spam', trash: 'trash', archive: 'archive', all: 'archive' }

export default function OpenMessage() {
  const { id } = useParams()
  const [to, setTo] = useState('')
  const [err, setErr] = useState('')
  useEffect(() => {
    api.get<MessageFull>(`${V1}/messages/${id}`).then(r => {
      const m = r.message
      const view = m.snooze_until && new Date(m.snooze_until) > new Date() ? 'snoozed' : VIEW[m.folder_role || '']
      setTo(view ? `/m/${view}/${m.id}` : `/f/${m.folder_id}/${m.id}`)
    }).catch(e => setErr((e as Error).message))
  }, [id])
  if (to) return <Navigate to={to} replace />
  if (err) return <Empty title="This message is not here any more" icon="mail">It was moved or deleted, maybe in another app.</Empty>
  return <Loading />
}
