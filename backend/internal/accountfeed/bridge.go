package accountfeed

import (
	"fmt"
	"os"
	"strings"
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
	// ConnCredentialsRequired: an API provider with no credentials configured.
	ConnCredentialsRequired ConnectionStatus = "credentials_required"
	// ConnCredentialsPresentUnverified: credentials are configured but no real
	// read smoke has proven a live connection — NOT "connected".
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

// ConnectionStatus computes the truthful status from the environment. It never
// returns "connected" without a real read (none is wired for API providers in
// this phase).
func (b BridgeContract) ConnectionStatus() ConnectionStatus {
	switch b.Provider {
	case ProviderGenericJSONFeed, ProviderLocalFolder:
		return ConnAvailable
	case ProviderUpworkAssisted, ProviderChatExport, ProviderBrowserCapture:
		return ConnContractOnly
	default:
		if b.hasConfiguredCredentials() {
			return ConnCredentialsPresentUnverified
		}
		return ConnCredentialsRequired
	}
}

func (b BridgeContract) hasConfiguredCredentials() bool {
	values := strings.Split(b.CredentialEnv, ",")
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" || strings.TrimSpace(os.Getenv(name)) == "" {
			return false
		}
	}
	return true
}

// ReportsCredentialGrant is intentionally false for Google OAuth bootstrap
// variables. A client id and secret enable the owner-scoped consent flow, but
// do not prove that this owner has granted a read-only source connection.
func (b BridgeContract) ReportsCredentialGrant() bool {
	switch b.Provider {
	case ProviderGmail, ProviderGoogleDrive, ProviderGoogleCalendar:
		return false
	default:
		return true
	}
}

// bridgeContracts is the registry of all provider bridge contracts (§14). API
// providers are read-only and require real credentials + a real read smoke for
// live status; HAI does not fake OAuth or connected status.
func bridgeContracts() []BridgeContract {
	apiSetup := func(name, env string) []SetupRequirement {
		return []SetupRequirement{
			{Step: "Provide read-only credentials", Detail: "Set " + env + " to a real read-only " + name + " token/credential; HAI never performs a fake OAuth flow."},
			{Step: "Run a real read smoke", Detail: "A real read against " + name + " must succeed before status becomes connected; configuration alone never claims connected."},
			{Step: "Prefer official API or normalized feed", Detail: "Use the official API or a normalized JSON export before any browser-assisted capture."},
		}
	}
	return []BridgeContract{
		{Provider: ProviderGenericJSONFeed, DisplayName: "Generic JSON Feed", ConnectorPreference: []string{"local_export", "official_api"}, ReadOnly: true,
			ItemTypes: allItemTypes(), SetupRequirements: []SetupRequirement{{Step: "Point at a JSON feed", Detail: "Register a feed with a local JSON file (or enabled HTTP JSON URL) in the generic feed format."}}},
		{Provider: ProviderLocalFolder, DisplayName: "Local Folder", ConnectorPreference: []string{"local_export"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemFile, ItemDocument}, SetupRequirements: []SetupRequirement{{Step: "Configure a feeds folder", Detail: "Files must live under the allowlisted feeds root."}}},
		{Provider: ProviderGmail, DisplayName: "Gmail (read-only)", ConnectorPreference: []string{"official_api", "local_export", "browser_read_only"}, ReadOnly: true,
			RequiredScopes: []string{"gmail.readonly"}, CredentialEnv: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET", ItemTypes: []ItemType{ItemEmail}, SetupRequirements: googleOAuthSetup("Gmail", "gmail.readonly")},
		{Provider: ProviderGoogleDrive, DisplayName: "Google Drive (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"drive.readonly"}, CredentialEnv: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET", ItemTypes: []ItemType{ItemFile, ItemDocument}, SetupRequirements: googleOAuthSetup("Google Drive", "drive.readonly")},
		{Provider: ProviderGoogleCalendar, DisplayName: "Google Calendar (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"calendar.readonly"}, CredentialEnv: "GOOGLE_OAUTH_CLIENT_ID, GOOGLE_OAUTH_CLIENT_SECRET", ItemTypes: []ItemType{ItemCalendarEvent}, SetupRequirements: googleOAuthSetup("Google Calendar", "calendar.readonly")},
		{Provider: ProviderGitHub, DisplayName: "GitHub (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"repo:read"}, CredentialEnv: "GITHUB_SOURCE_TOKEN", ItemTypes: []ItemType{ItemIssue, ItemPullRequest}, SetupRequirements: apiSetup("GitHub", "GITHUB_SOURCE_TOKEN")},
		{Provider: ProviderTrello, DisplayName: "Trello (read-only)", ConnectorPreference: []string{"official_api", "local_export"}, ReadOnly: true,
			RequiredScopes: []string{"read"}, CredentialEnv: "TRELLO_API_KEY, TRELLO_READ_TOKEN", ItemTypes: []ItemType{ItemCard}, SetupRequirements: apiSetup("Trello", "TRELLO_API_KEY and TRELLO_READ_TOKEN")},
		{Provider: ProviderUpworkAssisted, DisplayName: "Upwork (assisted)", ConnectorPreference: []string{"human_manual", "browser_read_only"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Assisted only", Detail: "Upwork has no automated connector; items arrive via human/manual export. No fake access."}}},
		{Provider: ProviderChatExport, DisplayName: "Chat Export", ConnectorPreference: []string{"local_export"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemChat, ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Export a chat log", Detail: "Provide a normalized chat export in the generic feed format."}}},
		{Provider: ProviderBrowserCapture, DisplayName: "Browser Capture (read-only)", ConnectorPreference: []string{"browser_read_only", "guarded_browser"}, ReadOnly: true,
			ItemTypes: []ItemType{ItemDocument, ItemMessage}, SetupRequirements: []SetupRequirement{{Step: "Bounded read-only capture", Detail: "Only allowlisted-domain read-only capture; no state changes. No executor is faked."}}},
	}
}

func googleOAuthSetup(name, scope string) []SetupRequirement {
	return []SetupRequirement{
		{Step: "Configure the OAuth client", Detail: "Set GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET for the read-only " + name + " connector."},
		{Step: "Connect this owner account", Detail: "Complete the owner-scoped OAuth consent flow in Connected Sources for the " + scope + " scope. Client bootstrap alone does not grant account access."},
		{Step: "Run a real read smoke", Detail: "A bounded read against " + name + " must succeed before it is treated as connected."},
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
