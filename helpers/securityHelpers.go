package helpers

import (
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/models"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"time"
)

func GeneratePassword(length int) string {
	const charset = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ0123456789!@#$%^&*(){}[]~`;:'+=.,/<>?-_"
	charsetLen := big.NewInt(int64(len(charset)))
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			panic(err)
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

func GenerateToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func CreateInviteToken(role string) (string, error) {
	token := GenerateToken(32)
	expires := time.Now().Add(24 * time.Hour)
	if err := initializers.DB.Create(&models.InviteToken{Token: token, Role: role, Expires: expires}).Error; err != nil {
		return "", err
	}
	return token, nil
}

func ValidateInviteToken(token string) (bool, string) {
	var inviteToken models.InviteToken

	if err := initializers.DB.First(&inviteToken, "token = ?", token).Error; err != nil {
		// Treat any DB error (including ErrRecordNotFound) as invalid token
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
