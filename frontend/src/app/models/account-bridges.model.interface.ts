export interface ISetupRequirement {
  step: string
  detail: string
}

export interface IBridgeContract {
  provider: string
  displayName: string
  connectorPreference: string[]
  readOnly: boolean
  requiredScopes: string[]
  credentialEnv?: string
  itemTypes: string[]
  setupRequirements: ISetupRequirement[]
  connectionStatus: string
}

export interface IAccountPermission {
  provider: string
  displayName: string
  readOnly: boolean
  declaredScopes: string[]
  credentialEnv?: string
  granted: boolean
  status: string
}

export interface IFeed {
  id: string
  name: string
  provider: string
  accountLabel: string
  sourceType: string
  path?: string
  url?: string
  operationType?: string
  enabled: boolean
}

export interface IFeedHealth {
  feed: IFeed
  connectionStatus: string
  lastSyncedAt?: string
  lastAttemptAt?: string
  lastItemsRead: number
  syncState?: 'idle' | 'running_or_interrupted'
  syncStartedAt?: string
}

export interface ISyncReport {
  feedId: string
  itemsRead: number
  operationsCreated: number
  operationsRefreshed: number
  privacyFlagged: number
  cursor?: string
  errors?: string[]
  recorded?: boolean
}

export interface IFeedAudit {
  id: string
  feedId: string
  eventType: string
  message: string
  createdAt: string
}

export type FeedIdentityState = 'unseen' | 'canonical' | 'historical' | 'coexisting'

export interface IFeedIdentityItem {
  externalId: string
  title: string
  state: FeedIdentityState
  canonicalOperationId?: string
  historicalOperationId?: string
}

export interface IFeedIdentityPreview {
  feedId: string
  // RFC3339 UTC observation time, not a transaction-wide snapshot timestamp.
  observedAt: string
  scope: 'current_local_feed_active_operations'
  historicalInventoryComplete: false
  itemsObserved: number
  itemsInspected: number
  truncated: boolean
  counts: Record<FeedIdentityState, number>
  items: IFeedIdentityItem[]
}
