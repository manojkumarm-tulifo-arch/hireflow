import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

interface SSEHandler {
  onItemEvent?: (eventName: string, data: Record<string, unknown>) => void
}

const DEFAULT_HANDLER: SSEHandler = {}

export function useBatchSSE(
  batchId: string | null,
  intentId: string,
  handler: SSEHandler = DEFAULT_HANDLER,
) {
  const qc = useQueryClient()

  useEffect(() => {
    if (!batchId) return
    const es = sourcingApi.subscribeBatchEvents(batchId)

    for (const eventName of ['item_accepted', 'item_failed', 'item_extracted', 'item_parsed']) {
      es.addEventListener(eventName, (raw) => {
        let data: Record<string, unknown> = {}
        try {
          data = JSON.parse((raw as MessageEvent).data)
        } catch {
          /* ignore parse errors */
        }
        handler.onItemEvent?.(eventName, data)
        if (eventName === 'item_parsed') {
          qc.invalidateQueries({ queryKey: ['applications', intentId] })
        }
      })
    }

    es.onerror = () => {
      /* EventSource auto-reconnects */
    }

    return () => es.close()
  }, [batchId, intentId, qc, handler])
}
