package frameworkregistry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const constitutionRuleVersion = "v1"

type constitutionRuleKind string

const (
	constitutionRuleDenyCapability   constitutionRuleKind = "deny-capability"
	constitutionRuleRequireApproval  constitutionRuleKind = "require-approval"
	constitutionRuleAuthorityCeiling constitutionRuleKind = "authority-ceiling"
)

const (
	capabilityMemoryRead            = "memory-read"
	capabilityDocumentRead          = "document-read"
	capabilityWebAccess             = "web-access"
	capabilityToolExecution         = "tool-execution"
	capabilityLocalExecution        = "local-execution"
	capabilityExecution             = "execution"
	capabilityExternalCommunication = "external-communication"
	capabilityLegalGovernmentAction = "legal-government-action"
	capabilityFinancialAction       = "financial-action"
	capabilityAccountChange         = "account-change"
	capabilityDestructiveAction     = "destructive-action"
	capabilityPublicPosting         = "public-posting"
	capabilityConsequentialAction   = "consequential-action"
)

var supportedConstitutionCapabilities = map[string]struct{}{
	capabilityMemoryRead:            {},
	capabilityDocumentRead:          {},
	capabilityWebAccess:             {},
	capabilityToolExecution:         {},
	capabilityLocalExecution:        {},
	capabilityExecution:             {},
	capabilityExternalCommunication: {},
	capabilityLegalGovernmentAction: {},
	capabilityFinancialAction:       {},
	capabilityAccountChange:         {},
	capabilityDestructiveAction:     {},
	capabilityPublicPosting:         {},
	capabilityConsequentialAction:   {},
}

// constitutionRule is intentionally restrictive. Constitution prose may add
// limits, but it cannot grant authority or weaken a protected overlay.
type constitutionRule struct {
	Kind       constitutionRuleKind `json:"kind"`
	Capability string               `json:"capability"`
	Level      int                  `json:"level"`
}

type effectiveConstitutionRules struct {
	Rules              []constitutionRule
	DeniedCapabilities map[string]struct{}
	ApprovalRequired   map[string]struct{}
	AuthorityCeiling   int
}

func compileEffectiveConstitutionRules(constitution Constitution) (effectiveConstitutionRules, error) {
	rules := append([]constitutionRule(nil), protectedTypedConstitutionRules()...)
	for _, value := range constitutionRuleSourceText(constitution) {
		rule, typed, err := parseConstitutionRule(value)
		if err != nil {
			return effectiveConstitutionRules{}, err
		}
		if typed {
			rules = append(rules, rule)
		}
	}

	rules = canonicalConstitutionRules(rules)
	effective := effectiveConstitutionRules{
		Rules:              rules,
		DeniedCapabilities: map[string]struct{}{},
		ApprovalRequired:   map[string]struct{}{},
		AuthorityCeiling:   10,
	}
	for _, rule := range rules {
		switch rule.Kind {
		case constitutionRuleDenyCapability:
			effective.DeniedCapabilities[rule.Capability] = struct{}{}
		case constitutionRuleRequireApproval:
			effective.ApprovalRequired[rule.Capability] = struct{}{}
		case constitutionRuleAuthorityCeiling:
			if rule.Level < effective.AuthorityCeiling {
				effective.AuthorityCeiling = rule.Level
			}
		}
	}
	return effective, nil
}

func protectedTypedConstitutionRules() []constitutionRule {
	return []constitutionRule{
		{Kind: constitutionRuleRequireApproval, Capability: capabilityLegalGovernmentAction},
		{Kind: constitutionRuleRequireApproval, Capability: capabilityFinancialAction},
		{Kind: constitutionRuleRequireApproval, Capability: capabilityAccountChange},
		{Kind: constitutionRuleRequireApproval, Capability: capabilityDestructiveAction},
		{Kind: constitutionRuleRequireApproval, Capability: capabilityPublicPosting},
	}
}

func constitutionRuleSourceText(constitution Constitution) []string {
	result := make([]string, 0)
	result = append(result, constitution.Values...)
	result = append(result, constitution.Prohibitions...)
	result = append(result, constitution.StandingPermissions...)
	result = append(result, constitution.Preferences...)
	result = append(result, constitution.RelationshipRules...)
	result = append(result, constitution.FinancialBoundaries...)
	result = append(result, constitution.CommunicationRules...)
	result = append(result, constitution.EscalationRules...)
	// ProtectedRules is deliberately excluded. Its exact prose is supplied by
	// protectedConstitutionRules, while its enforceable overlays come from
	// protectedTypedConstitutionRules and cannot be replaced by stored input.
	return result
}

func parseConstitutionRule(value string) (constitutionRule, bool, error) {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "hai-rule") {
		return constitutionRule{}, false, nil
	}

	fields := strings.Fields(lower)
	if len(fields) != 4 || fields[0] != "hai-rule" {
		return constitutionRule{}, true, fmt.Errorf(
			"invalid typed Constitution rule; expected HAI-RULE v1 <operation> <argument>",
		)
	}
	if fields[1] != constitutionRuleVersion {
		return constitutionRule{}, true, fmt.Errorf("unsupported typed Constitution rule version %q", fields[1])
	}

	switch constitutionRuleKind(fields[2]) {
	case constitutionRuleDenyCapability, constitutionRuleRequireApproval:
		capability, ok := strings.CutPrefix(fields[3], "capability=")
		if !ok || capability == "" {
			return constitutionRule{}, true, fmt.Errorf(
				"typed Constitution rule %q requires capability=<known-capability>",
				fields[2],
			)
		}
		if _, supported := supportedConstitutionCapabilities[capability]; !supported {
			return constitutionRule{}, true, fmt.Errorf("unknown Constitution capability %q", capability)
		}
		return constitutionRule{
			Kind:       constitutionRuleKind(fields[2]),
			Capability: capability,
		}, true, nil
	case constitutionRuleAuthorityCeiling:
		rawLevel, ok := strings.CutPrefix(fields[3], "level=")
		if !ok || rawLevel == "" {
			return constitutionRule{}, true, fmt.Errorf("authority-ceiling requires level=0..10")
		}
		level, err := strconv.Atoi(rawLevel)
		if err != nil || level < 0 || level > 10 {
			return constitutionRule{}, true, fmt.Errorf("authority-ceiling level must be between 0 and 10")
		}
		return constitutionRule{Kind: constitutionRuleAuthorityCeiling, Level: level}, true, nil
	default:
		return constitutionRule{}, true, fmt.Errorf(
			"unsupported typed Constitution rule operation %q; rules may only deny, require approval, or lower authority",
			fields[2],
		)
	}
}

func canonicalConstitutionRules(values []constitutionRule) []constitutionRule {
	byKey := make(map[string]constitutionRule, len(values))
	authorityCeiling := 10
	for _, value := range values {
		if value.Kind == constitutionRuleAuthorityCeiling {
			if value.Level < authorityCeiling {
				authorityCeiling = value.Level
			}
			continue
		}
		key := string(value.Kind) + "\x00" + value.Capability + "\x00" + strconv.Itoa(value.Level)
		byKey[key] = value
	}
	if authorityCeiling < 10 {
		value := constitutionRule{Kind: constitutionRuleAuthorityCeiling, Level: authorityCeiling}
		key := string(value.Kind) + "\x00\x00" + strconv.Itoa(value.Level)
		byKey[key] = value
	}
	result := make([]constitutionRule, 0, len(byKey))
	for _, value := range byKey {
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Capability != result[j].Capability {
			return result[i].Capability < result[j].Capability
		}
		return result[i].Level < result[j].Level
	})
	return result
}

func requestedConstitutionCapabilities(
	request SelectionRequest,
	taskText string,
	lifeDomain string,
	highRisk bool,
) map[string]struct{} {
	result := map[string]struct{}{}
	add := func(capability string) {
		result[capability] = struct{}{}
	}

	if request.NeedsMemory || containsAnyPhrase(taskText, []string{
		"memory context", "stored memory", "remembered preference", "past context",
	}) {
		add(capabilityMemoryRead)
	}
	if request.NeedsDocuments || containsAnyPhrase(taskText, []string{
		"source document", "uploaded document", "attachment", "pdf file",
	}) {
		add(capabilityDocumentRead)
	}
	if request.NeedsWebAccess || containsAnyPhrase(taskText, []string{
		"browse the web", "web access", "online source", "current official source",
	}) {
		add(capabilityWebAccess)
	}
	if request.NeedsTools {
		add(capabilityToolExecution)
		add(capabilityExecution)
	}
	if request.NeedsLocalExecution {
		add(capabilityLocalExecution)
		add(capabilityExecution)
	}
	if request.ExecuteRequested {
		add(capabilityExecution)
	}
	if containsRequestedConsequentialAction(taskText, []string{"send", "file with", "submit", "publish", "post publicly", "sign"}) ||
		(request.ExecuteRequested && containsAnyPhrase(taskText, []string{"email", "message", "reply", "communication"})) {
		add(capabilityExternalCommunication)
	}
	if lifeDomain == "legal_government" {
		add(capabilityLegalGovernmentAction)
	}
	if containsRequestedConsequentialAction(taskText, []string{
		"pay", "purchase", "transfer money", "accept price", "financial commitment",
	}) || (lifeDomain == "financial" && request.ExecuteRequested) {
		add(capabilityFinancialAction)
	}
	if containsRequestedConsequentialAction(taskText, []string{
		"account change", "change account", "reset account", "close account",
	}) {
		add(capabilityAccountChange)
	}
	if containsRequestedConsequentialAction(taskText, []string{"delete", "remove permanently", "destroy", "overwrite"}) {
		add(capabilityDestructiveAction)
	}
	if containsRequestedConsequentialAction(taskText, []string{"publish", "post publicly", "public post", "public accusation"}) {
		add(capabilityPublicPosting)
	}
	if highRisk || hasAnyCapability(result,
		capabilityExternalCommunication,
		capabilityFinancialAction,
		capabilityAccountChange,
		capabilityDestructiveAction,
		capabilityPublicPosting,
	) {
		add(capabilityConsequentialAction)
	}
	return result
}

func applyEffectiveConstitutionRules(
	effective effectiveConstitutionRules,
	capabilities map[string]struct{},
	requiresApproval bool,
	approvalReasons []string,
) (bool, []string, error) {
	for _, capability := range sortedCapabilitySet(capabilities) {
		if _, denied := effective.DeniedCapabilities[capability]; denied {
			return false, nil, fmt.Errorf("active Constitution denies requested capability %q", capability)
		}
		if _, required := effective.ApprovalRequired[capability]; required {
			requiresApproval = true
			approvalReasons = append(
				approvalReasons,
				fmt.Sprintf("the active Constitution requires approval for capability %s", capability),
			)
		}
	}
	return requiresApproval, sortedUnique(approvalReasons), nil
}

func sortedCapabilitySet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func hasAnyCapability(values map[string]struct{}, capabilities ...string) bool {
	for _, capability := range capabilities {
		if _, ok := values[capability]; ok {
			return true
		}
	}
	return false
}
