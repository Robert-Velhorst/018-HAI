package dto

type AuthSessionPermissions struct {
	CanRead       bool `json:"canRead"`
	CanOperate    bool `json:"canOperate"`
	CanApprove    bool `json:"canApprove"`
	CanAdminister bool `json:"canAdminister"`
}

type AuthSession struct {
	Authenticated  bool                   `json:"authenticated"`
	Subject        string                 `json:"subject"`
	Role           string                 `json:"role"`
	Permissions    AuthSessionPermissions `json:"permissions"`
	SessionVersion int64                  `json:"-"`
}
