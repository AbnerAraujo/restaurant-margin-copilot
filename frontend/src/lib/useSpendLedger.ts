import { useEffect, useState } from 'react'

import {
  loadSpendLedger,
  subscribeToSpendLedger,
  type SpendEntry,
} from '@/lib/chatStorage'

/**
 * The durable record of every model call this browser has paid for, kept in
 * sync with storage and with any other open tab.
 *
 * Replaces the shell's old in-memory `interactions` array, which reset to
 * zero on every reload while the answers it had paid for were still sitting
 * on screen — a running total that contradicted the conversation directly
 * above it, and under-reported spend the owner had genuinely incurred. The
 * conversation is deliberately durable (see `chatStorage`'s doc comment on
 * why the browser owns "what I was looking at"); the cost that produced it
 * has to be exactly as durable, or the pill is telling a comfortable lie.
 *
 * Read synchronously in the initializer for the same reason the thread store
 * is: a total that mounts at $0.000 and corrects itself a frame later reads
 * as a flicker of "you've spent nothing", which is the wrong thing to say
 * even for one frame.
 */
export function useSpendLedger(): SpendEntry[] {
  const [entries, setEntries] = useState<SpendEntry[]>(() => loadSpendLedger())

  useEffect(() => subscribeToSpendLedger(setEntries), [])

  return entries
}
