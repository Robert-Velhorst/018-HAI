package authentication

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"automation-hub-idp/internal/app/dto"
	"automation-hub-idp/internal/app/repositories/irepository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const refreshRotationReplayGrace = 5 * time.Second

const maxRefreshRotationPayloadBytes = 16 * 1024

func sessionIdentityFromClaims(claims jwt.MapClaims) (irepository.SessionIdentity, error) {
	var id irepository.SessionIdentity
	user, ok := claims["user_id"].(string)
	if !ok {
		return id, ErrRevokedSession
	}
	var err error
	id.UserID, err = uuid.Parse(user)
	if err != nil || id.UserID == uuid.Nil {
		return id, ErrRevokedSession
	}
	family, ok := refreshFamilyFromClaims(claims)
	if !ok {
		return id, ErrRevokedSession
	}
	id.FamilyID, err = uuid.Parse(family)
	if err != nil || id.FamilyID == uuid.Nil {
		return id, ErrRevokedSession
	}
	refresh, ok := claims["refresh_uuid"].(string)
	if !ok {
		return id, ErrRevokedSession
	}
	id.RefreshUUID, err = uuid.Parse(refresh)
	if err != nil || id.RefreshUUID == uuid.Nil {
		return id, ErrRevokedSession
	}
	id.SessionVersion, err = sessionVersionFromClaims(claims)
	if err != nil {
		return id, ErrRevokedSession
	}
	expKey := "exp"
	if hasTokenType(claims, tokenTypeAccess) {
		expKey = "refresh_exp"
	}
	expires, ok := unixClaim(claims, expKey)
	if !ok || expires <= time.Now().Unix() {
		return id, ErrRevokedSession
	}
	id.ExpiresAt = time.Unix(expires, 0).UTC()
	return id, nil
}

type refreshRotationPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	RefreshUUID  string `json:"refresh_uuid"`
	AtExpires    int64  `json:"access_expires"`
	RtExpires    int64  `json:"refresh_expires"`
}

func refreshFamilyFromClaims(claims jwt.MapClaims) (string, bool) {
	if value, exists := claims["family_uuid"]; exists {
		familyUUID, ok := value.(string)
		return strings.TrimSpace(familyUUID), ok && strings.TrimSpace(familyUUID) != ""
	}
	refreshUUID, ok := claims["refresh_uuid"].(string)
	return strings.TrimSpace(refreshUUID), ok && strings.TrimSpace(refreshUUID) != ""
}

func positiveTokenTTL(expiresAt int64) time.Duration {
	remaining := time.Until(time.Unix(expiresAt, 0))
	if remaining < time.Second {
		return time.Second
	}
	return remaining
}

func (a *service) encryptRefreshRotationPayload(details *dto.TokenDetails, predecessorUUID string) (string, error) {
	if strings.TrimSpace(predecessorUUID) == "" || details == nil || details.AccessToken == "" || details.RefreshToken == "" || details.RefreshUUID == "" {
		return "", errors.New("refresh rotation payload is incomplete")
	}
	plain, err := json.Marshal(refreshRotationPayload{
		AccessToken:  details.AccessToken,
		RefreshToken: details.RefreshToken,
		RefreshUUID:  details.RefreshUUID,
		AtExpires:    details.AtExpires,
		RtExpires:    details.RtExpires,
	})
	if err != nil {
		return "", err
	}
	if len(plain) > maxRefreshRotationPayloadBytes {
		return "", errors.New("refresh rotation payload is too large")
	}
	aead, err := a.refreshRotationAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, plain, refreshRotationAssociatedData(predecessorUUID))
	sealed := append(nonce, ciphertext...)
	return "v2." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (a *service) decryptRefreshRotationPayload(encoded, predecessorUUID string) (*refreshRotationPayload, error) {
	if strings.TrimSpace(predecessorUUID) == "" || !strings.HasPrefix(encoded, "v2.") || len(encoded) > maxRefreshRotationPayloadBytes*2 {
		return nil, errors.New("invalid refresh rotation payload")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, "v2."))
	if err != nil {
		return nil, errors.New("invalid refresh rotation payload")
	}
	aead, err := a.refreshRotationAEAD()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() || len(sealed) > maxRefreshRotationPayloadBytes+aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("invalid refresh rotation payload size")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, refreshRotationAssociatedData(predecessorUUID))
	if err != nil || len(plain) > maxRefreshRotationPayloadBytes {
		return nil, errors.New("invalid refresh rotation payload authentication")
	}
	var payload refreshRotationPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, errors.New("invalid refresh rotation payload")
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" || payload.RefreshUUID == "" || payload.AtExpires <= 0 || payload.RtExpires <= 0 {
		return nil, errors.New("incomplete refresh rotation payload")
	}
	return &payload, nil
}

// A valid encrypted response from another generation is not a valid replay.
func refreshRotationAssociatedData(predecessorUUID string) []byte {
	return []byte("hai-idp-refresh-rotation-v2\x00" + predecessorUUID)
}

func (a *service) refreshRotationAEAD() (cipher.AEAD, error) {
	if len(a.jwtSecret) < 32 {
		return nil, errors.New("refresh rotation encryption key is unavailable")
	}
	key := sha256.Sum256([]byte("hai-idp-refresh-rotation-key-v1\x00" + a.jwtSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// A cached winner is not necessarily still usable: its child may have rotated,
// the account may have changed, or logout may have completed during the call.
func (a *service) validateRefreshRotationResult(result *refreshRotationPayload, original jwt.MapClaims) error {
	family, ok := refreshFamilyFromClaims(original)
	if !ok {
		return ErrRevokedSession
	}
	originalExp, ok := unixClaim(original, "exp")
	if !ok || result.RtExpires != originalExp || result.AtExpires > originalExp ||
		result.AtExpires <= time.Now().Unix() || result.RtExpires <= time.Now().Unix() {
		return ErrRevokedSession
	}
	originalVersion, err := sessionVersionFromClaims(original)
	if err != nil {
		return ErrRevokedSession
	}
	var accessClaims jwt.MapClaims
	for _, item := range []struct {
		token string
		kind  string
		exp   int64
	}{
		{result.AccessToken, tokenTypeAccess, result.AtExpires},
		{result.RefreshToken, tokenTypeRefresh, result.RtExpires},
	} {
		_, claims, err := a.parseAndValidateToken(item.token)
		if err != nil || !hasTokenType(claims, item.kind) {
			return ErrRevokedSession
		}
		resultFamily, validFamily := refreshFamilyFromClaims(claims)
		exp, validExp := unixClaim(claims, "exp")
		version, versionErr := sessionVersionFromClaims(claims)
		if !validFamily || resultFamily != family || !validExp || exp != item.exp ||
			claims["user_id"] != original["user_id"] || claims["refresh_uuid"] != result.RefreshUUID ||
			versionErr != nil || version != originalVersion {
			return ErrRevokedSession
		}
		if item.kind == tokenTypeAccess {
			refreshExp, valid := unixClaim(claims, "refresh_exp")
			if !valid || refreshExp != originalExp {
				return ErrRevokedSession
			}
			accessClaims = claims
		}
	}
	if _, err = a.activeUserFromClaims(accessClaims); err != nil {
		return err
	}
	id, err := sessionIdentityFromClaims(accessClaims)
	if err != nil {
		return err
	}
	predecessor, ok := original["refresh_uuid"].(string)
	if !ok {
		return ErrRevokedSession
	}
	predecessorID, err := uuid.Parse(predecessor)
	if err != nil {
		return ErrRevokedSession
	}
	if a.sessionRepository == nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	ctx, cancel := sessionAuthorityContext()
	defer cancel()
	user, err := a.sessionRepository.ValidateRotation(ctx, id, predecessorID)
	if err != nil {
		return sessionAuthorityError(err)
	}
	if user == nil {
		return irepository.ErrSessionAuthorityUnavailable
	}
	if !accessRoleMatchesUser(accessClaims, user) {
		return ErrRevokedSession
	}
	return nil
}
