package dto

type ConfirmPasswordResetDTO struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}
