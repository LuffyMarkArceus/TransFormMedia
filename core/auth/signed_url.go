package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const shareTokenVersion = "v1"

func GenerateShareToken(mediaID, userID, secret string, ttl time.Duration) string {
	expires := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s:%s:%s:%d", shareTokenVersion, mediaID, userID, expires)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return body + "." + sig
}

func VerifyShareToken(token, secret string) (mediaID, userID string, err error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid token format")
	}

	bodyB64, sigB64 := parts[0], parts[1]

	payload, err := base64.RawURLEncoding.DecodeString(bodyB64)
	if err != nil {
		return "", "", fmt.Errorf("invalid token encoding: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sigB64), []byte(expectedSig)) {
		return "", "", fmt.Errorf("invalid token signature")
	}

	segments := strings.SplitN(string(payload), ":", 4)
	if len(segments) != 4 || segments[0] != shareTokenVersion {
		return "", "", fmt.Errorf("invalid token payload")
	}

	mediaID = segments[1]
	userID = segments[2]
	expUnix, err := strconv.ParseInt(segments[3], 10, 64)
	if err != nil {
		return "", "", fmt.Errorf("invalid token expiration: %w", err)
	}

	if time.Now().Unix() > expUnix {
		return "", "", fmt.Errorf("token has expired")
	}

	return mediaID, userID, nil
}
