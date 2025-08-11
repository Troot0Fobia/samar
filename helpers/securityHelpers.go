package helpers

import (
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

func GeneratePassword(length int) string {
	const charset = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ0123456789!@#$%^&*(){}[]~`;:'+=.,/<>?-_"
	b := make([]byte, length)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

func GenerateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)

	token := hex.EncodeToString(b)
	return token
}

func CreateInviteToken(role string) string {
	token := GenerateToken(32)
	expires := time.Now().Add(24 * time.Hour)
	initializers.DB.Create(&models.InviteToken{Token: token, Role: role, Expires: expires})

	return token
}

func ValidateInviteToken(token string) (bool, string) {
	var inviteToken models.InviteToken

	if err := initializers.DB.First(&inviteToken, "token = ?", token).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return false, ""
	}

	if time.Now().After(inviteToken.Expires) || inviteToken.Used {
		return false, ""
	}

	return true, inviteToken.Role
}

func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
