package accountfeed

// AccountPermission records the read-only scopes a bridge declares and whether
// a direct provider credential is present. OAuth client bootstrap is not an
// owner grant, so it never sets Granted. Granted never means "connected" — it
// only means a direct credential exists to attempt a real read.
type AccountPermission struct {
	Provider       Provider         `json:"provider"`
	DisplayName    string           `json:"displayName"`
	ReadOnly       bool             `json:"readOnly"`
	DeclaredScopes []string         `json:"declaredScopes"`
	CredentialEnv  string           `json:"credentialEnv,omitempty"`
	Granted        bool             `json:"granted"`
	Status         ConnectionStatus `json:"status"`
}

// PermissionRegistry is the account permission registry (§2D). It reports, per
// provider, the declared read-only scopes and whether a real credential is
// present — without ever fabricating OAuth or a connected status.
type PermissionRegistry struct{}

// NewPermissionRegistry builds the registry.
func NewPermissionRegistry() *PermissionRegistry { return &PermissionRegistry{} }

// Permissions returns the permission entry for every provider bridge.
func (r *PermissionRegistry) Permissions() []AccountPermission {
	var out []AccountPermission
	for _, b := range bridgeContracts() {
		status := b.ConnectionStatus()
		out = append(out, AccountPermission{
			Provider:       b.Provider,
			DisplayName:    b.DisplayName,
			ReadOnly:       b.ReadOnly,
			DeclaredScopes: b.RequiredScopes,
			CredentialEnv:  b.CredentialEnv,
			Granted:        status == ConnCredentialsPresentUnverified && b.ReportsCredentialGrant(),
			Status:         status,
		})
	}
	return out
}

// Permission returns a single provider's permission entry.
func (r *PermissionRegistry) Permission(p Provider) (AccountPermission, bool) {
	for _, perm := range r.Permissions() {
		if perm.Provider == p {
			return perm, true
		}
	}
	return AccountPermission{}, false
}

// WriteAllowed always reports false: every bridge in this phase is read-only.
// External writes are gated by the approval + execution path, not feed bridges.
func (r *PermissionRegistry) WriteAllowed(Provider) bool { return false }
