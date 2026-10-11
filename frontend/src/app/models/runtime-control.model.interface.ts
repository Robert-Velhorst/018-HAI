export interface IEmergencyStopState {
  engaged: boolean
  reason?: string
  actor?: string
  engagedAt?: string
  updatedAt: string
}

export interface IDockerStatus {
  cliAvailable: boolean
  daemonRunning: boolean
  required: boolean
  detail: string
}

export interface IBackgroundStatus {
  mode: string
  storedMode: string
  modeStateError?: string
  emergencyStop: IEmergencyStopState
  backgroundProcessingActive: boolean
  docker: IDockerStatus
  completedOperations: number
  awaitingApproval: number
}

export interface IReadinessGate {
  name: string
  status: string
  evidence: string
  remediation?: string
}

export interface IReadiness {
  operatingSystem: string
  isWindows: boolean
  overallReady: boolean
  targetMachineVerificationPending: boolean
  backgroundMode: string
  emergencyStop: IEmergencyStopState
  docker: IDockerStatus
  gates: IReadinessGate[]
}

export interface IEmergencyStopVerification {
  engagedDuringTest: boolean
  operationsProcessedDuringStop: number
  halted: boolean
  restoredEngagedState: boolean
  detail: string
}

export interface IRecoveryReport {
  scannedRunning: number
  scannedVerifying: number
  recovered: number
  details?: string[]
  ranAt: string
}
