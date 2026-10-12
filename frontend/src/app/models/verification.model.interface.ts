export interface IEvidenceInput {
  sourceType: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  snippet: string;
  authority?: string;
  freshness?: string;
  official?: boolean;
  primary?: boolean;
  generated?: boolean;
}

export interface IAnswerRequest {
  question: string;
  projectKey?: string;
  pursuitId?: string;
  mode?: string;
  externalEvidence?: IEvidenceInput[];
  includeSensitive?: boolean;
  includeRagflowCandidates?: boolean;
  includeResearchCandidates?: boolean;
  humanApproved?: boolean;
  allowMemoryUpdate?: boolean;
}

export interface IVerificationRun {
  id: string;
  mode: string;
  question: string;
  projectKey?: string;
  answer: string;
  status: string;
  researchQuestions?: string;
  sourcesSearched?: string;
  sourcesUsed?: string;
  sourcesRejected?: string;
  missingSources?: string;
  createdAt: string;
}

export interface IVerificationEvidence {
  id: string;
  runId: string;
  sourceType: string;
  sourceId?: string;
  sourceUri?: string;
  sourceLabel?: string;
  snippet: string;
  authority?: string;
  freshness?: string;
  qualityScore: number;
  used: boolean;
  rejected: boolean;
  rejectReason?: string;
}

export interface IVerificationClaim {
  id: string;
  runId: string;
  claimText: string;
  status: string;
  sourceRefs?: string;
  supportExplanation?: string;
  confidence: number;
  needsReview: boolean;
  highRisk: boolean;
}

export interface IRAGFlowCandidateEvidence {
  sourceType: string;
  sourceUri: string;
  sourceLabel: string;
  snippet: string;
  datasetId: string;
  documentId?: string;
  chunkId: string;
  similarity?: number;
  status: 'unverified_candidate';
  restrictions: string;
}

export interface IResearchCandidateEvidence {
  sourceType: string;
  sourceUri: string;
  sourceLabel: string;
  snippet: string;
  status: 'unverified_candidate';
  restrictions: string;
}

export interface IEvidenceConflict {
  topic: string;
  status: 'needs_review';
  reason: string;
  evidenceRefs: string[];
}

export interface IVerificationResult {
  run: IVerificationRun;
  pursuitId?: string;
  pursuitLinked?: boolean;
  pursuitLinkError?: string;
  claims: IVerificationClaim[];
  evidence: IVerificationEvidence[];
  conflicts?: IEvidenceConflict[];
  ragflowCandidates?: IRAGFlowCandidateEvidence[];
  researchCandidates?: IResearchCandidateEvidence[];
  unsupportedClaims: IVerificationClaim[];
  researchQuestions: string[];
  logs: string[];
  auditWarnings?: string[];
}
