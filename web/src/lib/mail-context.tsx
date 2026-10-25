// The mail state every page shares: accounts with folders, the numbers next to the views.
import { createContext, useContext } from 'react'
import type { AccountView, Counts, Folder } from './types'

export interface MailState {
  accounts: AccountView[]
  counts: Counts
  loaded: boolean
  /** your own addresses (lower case), to show "To: …" for what you sent */
  mine: Set<string>
  reload: () => void
  folder: (id: number) => Folder | undefined
  account: (id: number) => AccountView | undefined
}

export const MailCtx = createContext<MailState>({ accounts: [], counts: {}, loaded: false, mine: new Set(), reload: () => {}, folder: () => undefined, account: () => undefined })
export const useMail = () => useContext(MailCtx)
