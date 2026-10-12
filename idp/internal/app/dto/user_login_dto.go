package dto

type UserLoginDTO struct {
	Email      string `json:"email" binding:"required"`
	Password   string `json:"password" binding:"required"`
	RememberMe *bool  `json:"rememberMe,omitempty"`
}
