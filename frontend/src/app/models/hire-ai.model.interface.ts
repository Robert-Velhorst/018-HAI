export interface IHireAIStatus {
  enabled: boolean
  configured: boolean
  endpoint?: string
  configError?: string
  capabilities: string[]
  restrictions: string[]
}

export interface IHireAIFilters {
  query?: string
  location?: string
  jobType?: string
  platformId?: number
  salaryRange?: [number, number]
  salaryCurrency?: string
  experienceLevel?: string
  applicationProcess?: string
  postedWithin?: string
  visaSponsorshipOnly?: boolean
  openHiringSupportOnly?: boolean
  diversityFriendlyOnly?: boolean
  salaryDisclosedOnly?: boolean
}

export interface IHireAICursor {
  postedDate?: string | null
  createdAt: string
  id: number
}

export interface IHireAIJob {
  id: number
  title?: string | null
  company?: string | null
  location?: string | null
  jobType?: string | null
  salaryMin?: number | null
  salaryMax?: number | null
  salaryCurrency?: string | null
  skills?: string | null
  description?: string | null
  requirements?: string | null
  platformId: number
  platformName?: string | null
  sourceUrl?: string | null
  applicationUrl?: string | null
  postedAt?: string | null
}

export interface IHireAISearchResult {
  generatedAt: string
  policy: {
    remoteOnly: boolean
    listingSafety: string
    canonicalListingsOnly: boolean
  }
  jobs: IHireAIJob[]
  nextCursor?: IHireAICursor | null
}
