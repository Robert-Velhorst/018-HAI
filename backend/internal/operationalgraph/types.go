package operationalgraph

import "time"

const ContractVersion = 1

type Node struct {
	ID                 string            `json:"id"`
	Kind               string            `json:"kind"`
	Layer              string            `json:"layer"`
	Label              string            `json:"label"`
	Summary            string            `json:"summary,omitempty"`
	Status             string            `json:"status,omitempty"`
	Weight             float64           `json:"weight"`
	ParentID           string            `json:"parentId,omitempty"`
	ProjectKeys        []string          `json:"projectKeys,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	VerificationStatus string            `json:"verificationStatus,omitempty"`
	Sensitivity        string            `json:"sensitivity,omitempty"`
	LocalOnly          bool              `json:"localOnly"`
	SourceCount        int               `json:"sourceCount"`
	Details            map[string]string `json:"details,omitempty"`
	UpdatedAt          time.Time         `json:"updatedAt,omitempty"`
}

type Link struct {
	ID       string  `json:"id"`
	SourceID string  `json:"sourceId"`
	TargetID string  `json:"targetId"`
	Type     string  `json:"type"`
	Label    string  `json:"label,omitempty"`
	Weight   float64 `json:"weight"`
}

type Quality struct {
	OrphanNodes       int `json:"orphanNodes"`
	SourceBackedNodes int `json:"sourceBackedNodes"`
	NeedsReviewNodes  int `json:"needsReviewNodes"`
	LocalOnlyNodes    int `json:"localOnlyNodes"`
	BlockedNodes      int `json:"blockedNodes"`
}

type Snapshot struct {
	ContractVersion int            `json:"contractVersion"`
	GeneratedAt     time.Time      `json:"generatedAt"`
	RootID          string         `json:"rootId"`
	Nodes           []Node         `json:"nodes"`
	Links           []Link         `json:"links"`
	LayerCounts     map[string]int `json:"layerCounts"`
	Quality         Quality        `json:"quality"`
	Truncated       bool           `json:"truncated"`
	Warnings        []string       `json:"warnings,omitempty"`
	Scope           string         `json:"scope"`
}

type SearchResult struct {
	Query       string `json:"query"`
	Results     []Node `json:"results"`
	Total       int    `json:"total"`
	Truncated   bool   `json:"truncated"`
	Explanation string `json:"explanation"`
}

type Neighborhood struct {
	RootID      string `json:"rootId"`
	Depth       int    `json:"depth"`
	Nodes       []Node `json:"nodes"`
	Links       []Link `json:"links"`
	Truncated   bool   `json:"truncated"`
	Explanation string `json:"explanation"`
}

type PathResult struct {
	FromID      string   `json:"fromId"`
	ToID        string   `json:"toId"`
	Found       bool     `json:"found"`
	NodeIDs     []string `json:"nodeIds"`
	Links       []Link   `json:"links"`
	Explanation string   `json:"explanation"`
}

type AgentBootContext struct {
	ContractVersion                int                `json:"contractVersion"`
	GeneratedAt                    time.Time          `json:"generatedAt"`
	AgentID                        string             `json:"agentId"`
	AgentName                      string             `json:"agentName"`
	State                          string             `json:"state"`
	Health                         string             `json:"health"`
	RuntimeID                      string             `json:"runtimeId"`
	RuntimeType                    string             `json:"runtimeType"`
	Capabilities                   []string           `json:"capabilities"`
	Teams                          []AgentTeamContext `json:"teams"`
	AuthorityCeiling               int                `json:"authorityCeiling"`
	AutonomyCeiling                int                `json:"autonomyCeiling"`
	RiskCeiling                    string             `json:"riskCeiling"`
	ToolAllowlist                  []string           `json:"toolAllowlist"`
	DataAllowlist                  []string           `json:"dataAllowlist"`
	FolderAllowlist                []string           `json:"folderAllowlist"`
	ProhibitedActions              []string           `json:"prohibitedActions"`
	EvidenceRequirements           []string           `json:"evidenceRequirements"`
	GrantsExecutionAuthority       bool               `json:"grantsExecutionAuthority"`
	ExecutionAuthorizationRequired bool               `json:"executionAuthorizationRequired"`
	Explanation                    string             `json:"explanation"`
}

type AgentTeamContext struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	Status           string   `json:"status"`
	RoleIDs          []string `json:"roleIds"`
	CapabilityIDs    []string `json:"capabilityIds"`
	AuthorityCeiling int      `json:"authorityCeiling"`
	RiskCeiling      string   `json:"riskCeiling"`
	AdvisoryOnly     bool     `json:"advisoryOnly"`
}

type MemoryWriteRequest struct {
	ProjectKey  string   `json:"projectKey,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Content     string   `json:"content"`
	Summary     string   `json:"summary,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	SourceURI   string   `json:"sourceUri,omitempty"`
	SourceLabel string   `json:"sourceLabel,omitempty"`
}

type ReportWriteRequest struct {
	AgentID    string   `json:"agentId,omitempty"`
	ProjectKey string   `json:"projectKey,omitempty"`
	Status     string   `json:"status"`
	Summary    string   `json:"summary"`
	Details    string   `json:"details,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	SourceURI  string   `json:"sourceUri,omitempty"`
}
