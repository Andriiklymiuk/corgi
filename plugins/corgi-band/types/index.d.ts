export type Waiting = { id: string; name: string; what: string }
export type Action = { label: string; isDone: boolean; why?: string }
export type PaneSession = { id: string; name: string; status: string; what: string; context?: number }
export type PaneCard = { ref: string; title: string; column: string; workspace: string; why?: string }
export type PaneService = { label: string; port?: number; isHealthy: boolean }
export type Snapshot = {
  sessions: PaneSession[]
  cards: PaneCard[]
  stack: PaneService[]
  budget?: string
  at: number
}

declare module 'claude-code' {
  interface PluginState {
    'corgi-band': { waiting: Waiting[]; actions: Action[]; hidden: string; snapshot: Snapshot | null }
  }
}
