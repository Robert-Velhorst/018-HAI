package accountfeed

import (
	"fmt"
)

// Provider is a supported account-bridge provider contract (§14). Metadata and
// contracts may exist for all of these, but live status requires real
// credentials and a real read — HAI never fakes OAuth or connected status.
type Provider string

const (
	ProviderGenericJSONFeed Provider = "generic_json_feed"
	ProviderLocalFolder     Provider = "local_folder"
	ProviderGmail           Provider = "gmail"
	ProviderGoogleDrive     Provider = "google_drive"
	ProviderGoogleCalendar  Provider = "google_calendar"
	ProviderGitHub          Provider = "github"
	ProviderTrello          Provider = "trello"
	ProviderUpworkAssisted  Provider = "upwork_assisted"
	ProviderChatExport      Provider = "chat_export"
	ProviderBrowserCapture  Provider = "browser_capture"
)

func allProviders() []Provider {
	return []Provider{
		ProviderGenericJSONFeed, ProviderLocalFolder, ProviderGmail, ProviderGoogleDrive,
		ProviderGoogleCalendar, ProviderGitHub, ProviderTrello, ProviderUpworkAssisted,
		ProviderChatExport, ProviderBrowserCapture,
	}
}

func (p Provider) String() string { return string(p) }
func (p Provider) IsValid() bool {
	for _, x := range allProviders() {
		if x == p {
			return true
		}
	}
	return false
}

// ParseProvider parses a provider string.
func ParseProvider(v string) (Provider, error) {
	p := Provider(v)
	if !p.IsValid() {
		return "", fmt.Errorf("accountfeed: invalid provider %q", v)
	}
	return p, nil
}

// ConnectionStatus is a bridge's truthful connection status (§2D: no fake
// connected status). "connected" is reserved for a provider that has actually
// performed a real read; it is never claimed from configuration alone.
type ConnectionStatus string

const (
	// ConnAvailable: a local provider whose reads work now (generic feed, folder).
	ConnAvailable ConnectionStatus = "available"
	// ConnCredentialsRequired remains for API-backed bridge implementations. The
	// current account-feed registry does not implement any such provider reads.
	ConnCredentialsRequired ConnectionStatus = "credentials_required"
	// ConnCredentialsPresentUnverified remains for API-backed bridge
	// implementations after a credential is configured but before a real read.
	ConnCredentialsPresentUnverified ConnectionStatus = "credentials_present_unverified"
	// ConnContractOnly: an assisted/manual/browser provider with no automated connector.
	ConnContractOnly ConnectionStatus = "contract_only"
)

// SetupRequirement is one exact step to make a bridge usable.
type SetupRequirement struct {
	Step   string `json:"step"`
	Detail string `json:"detail"`
}

// BridgeContract describes a provider bridge: its connector preference order,
// declared read-only scopes, credential requirement, and setup steps.
type BridgeContract struct {
	Provider            Provider           `json:"provider"`
	DisplayName         string             `json:"displayName"`
	ConnectorPreference []string           `json:"connectorPreference"`
	ReadOnly            bool               `json:"readOnly"`
	RequiredScopes      []string           `json:"requiredScopes"`
	CredentialEnv       string             `json:"credentialEnv,omitempty"`
	ItemTypes           []ItemType         `json:"itemTypes"`
	SetupRequirements   []SetupRequirement `json:"setupRequirements"`
}

// ConnectionStatus describes this registry's capabilities, not the broader HAI
// source connector catalog. Account feeds only ingest the bounded normalized
// generic-feed format; they do not use provider credentials or make Gmail,
// Google, GitHub, or Trello API calls. Provider-native status belongs to the
// owner-scoped Connected Sources API.
func (b BridgeContract) ConnectionStatus() ConnectionStatus {
	switch b.Provider {
	case ProviderGenericJSONFeed, ProviderLocalFolder:
		return ConnAvailable
	default:
		return ConnContractOnly
	}
}

// bridgeContracts is the registry of all provider bridge contracts (§14). API
// providers are read-only and require real credentials + a real read smoke for
// live status; HAI does not fake OAuth or connected status.
func bridgeContracts() []BridgeContract {
	providerImportSetup := func(name string) []SetupRequirement {
		return []SetupRequirement{
			{Step: "Use Connected Sources for the live provider", Detail: "Provider-native " + name + " health, consent, and sync belong to Connected Sources; this registry never reads provider credentials."},
			{Step: "Import a normalized read-only export", Detail: "To route " + name + " data here, register a local or explicitly enabled HTTP generic JSON feed."},
		}
	}
	return []BridgeContract{
		{Provider: ProviderGenericJSONFeed, DisplayName: "Generic JSON Feed", ConnectorPreference: []string{"local_export", "official_api"}, ReadOnly: true,
			ItemTypes: allItemTypes(), SetupRequirements: []SetupRequirement{{Step: "Point at a JSON feed", Detail: "Register a feed with a local JSON file (or enabled HTTP JSON URL) in the generic feed format."}}},
		{Provider: ProviderLocalFolder, DisplayName: "Local Folder", ConnectorPreference: []string{"local_export"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemFile, ItemDocument}, SetupRequirements: []SetupRequirement{{Step: "Configure a feeds folder", Detail: "Files must live under the allowlisted feeds root."}}},
		{Provider: ProviderGmail, DisplayName: "Gmail (read-only)", ConnectorPreference: []string{"official_api", "local_export", "browser_read_only"}, ReadOnly: true,
			RequiredScopes: []string{"gmail.readonly"}, ItemTypes: []ItemType{ItemEmail}, SetupRequirements: providerImportSetup("Gmail")},
		{Provider: ProviderGoogleDrive, DisplayName: "Google Drive (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"drive.readonly"}, ItemTypes: []ItemType{ItemFile, ItemDocument}, SetupRequirements: providerImportSetup("Google Drive")},
		{Provider: ProviderGoogleCalendar, DisplayName: "Google Calendar (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"calendar.readonly"}, ItemTypes: []ItemType{ItemCalendarEvent}, SetupRequirements: providerImportSetup("Google Calendar")},
		{Provider: ProviderGitHub, DisplayName: "GitHub (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"repo:read"}, ItemTypes: []ItemType{ItemIssue, ItemPullRequest}, SetupRequirements: providerImportSetup("GitHub")},
		{Provider: ProviderTrello, DisplayName: "Trello (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"read"}, ItemTypes: []ItemType{ItemCard}, SetupRequirements: providerImportSetup("Trello")},
		{Provider: ProviderUpworkAssisted, DisplayName: "Upwork (assisted)", ConnectorPreference: []string{"human_manual", "browser_read_only"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Assisted only", Detail: "Upwork has no automated connector; items arrive via human/manual export. No fake access."}}},
		{Provider: ProviderChatExport, DisplayName: "Chat Export", ConnectorPreference: []string{"local_export"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemChat, ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Export a chat log", Detail: "Provide a normalized chat export in the generic feed format."}}},
		{Provider: ProviderBrowserCapture, DisplayName: "Browser Capture (read-only)", ConnectorPreference: []string{"browser_read_only", "guarded_browser"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemDocument, ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Bounded read-only capture", Detail: "Only allowlisted-domain read-only capture; no state changes. No executor is faked."}}},
	}
}

// Bridges returns every provider bridge contract with its truthful status.
func Bridges() []BridgeContract { return bridgeContracts() }

// Bridge returns a single provider's bridge contract.
func Bridge(p Provider) (BridgeContract, bool) {
	for _, b := range bridgeContracts() {
		if b.Provider == p {
			return b, true
		}
	}
	return BridgeContract{}, false
}
