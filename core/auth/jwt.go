package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	ClerkJWKSURL = "https://learning-dingo-96.clerk.accounts.dev/.well-known/jwks.json"
	ClerkIssuer  = "https://learning-dingo-96.clerk.accounts.dev" // Replace with your Clerk instance URL
)

// ---- JWKS STRUCT ----

type jwks struct {
	Keys []jsonKey `json:"keys"`
}

type jsonKey struct {
	Kid string `json:"kid"`
	E   string `json:"e"`
	N   string `json:"n"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

// ---- CACHED KEYS ----

var (
	jwksCache     = make(map[string]*rsa.PublicKey)
	jwksCacheLock sync.RWMutex
	lastFetch     time.Time
)

// ---- CLAIMS ----

type ClerkClaims struct {
	Email    string `json:"email"`
	Sub      string `json:"sub"`
	Username string `json:"username,omitempty"`

	jwt.RegisteredClaims
}

// ---- FETCH JWKS ----

func fetchJWKS() error {
	jwksCacheLock.Lock()
	defer jwksCacheLock.Unlock()

	if time.Since(lastFetch) < 5*time.Minute {
		return nil
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", ClerkJWKSURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var jwksData jwks
	if err := json.NewDecoder(resp.Body).Decode(&jwksData); err != nil {
		return err
	}

	tempCache := make(map[string]*rsa.PublicKey)

	for _, key := range jwksData.Keys {
		pubKey, err := parseRSAKey(key.N, key.E)
		if err != nil {
			continue
		}
		tempCache[key.Kid] = pubKey
	}

	jwksCache = tempCache
	lastFetch = time.Now()

	return nil
}

// ---- PARSE RSA KEY ----

func parseRSAKey(nStr, eStr string) (*rsa.PublicKey, error) {
	parser := &jwt.Parser{}
	nBytes, err := parser.DecodeSegment(nStr)
	if err != nil {
		return nil, err
	}

	eBytes, err := parser.DecodeSegment(eStr)
	if err != nil {
		return nil, err
	}

	var eInt int
	if len(eBytes) == 3 {
		eInt = int(eBytes[0])<<16 | int(eBytes[1])<<8 | int(eBytes[2])
	} else if len(eBytes) == 1 {
		eInt = int(eBytes[0])
	} else {
		return nil, errors.New("invalid exponent length")
	}

	pubKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: eInt,
	}

	return pubKey, nil
}

// ---- VERIFY JWT ----

func VerifyClerkJWT(tokenStr string) (*ClerkClaims, error) {
	if err := fetchJWKS(); err != nil {
		return nil, err
	}

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		kidValue, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errors.New("missing kid")
		}

		jwksCacheLock.RLock()
		pubKey := jwksCache[kidValue]
		jwksCacheLock.RUnlock()

		if pubKey == nil {
			return nil, errors.New("public key not found for kid")
		}

		return pubKey, nil
	}

	claims := &ClerkClaims{}

	parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc)
	if err != nil {
		return nil, err
	}

	if !parsedToken.Valid {
		return nil, errors.New("invalid token")
	}

	// Validate issuer
	if claims.Issuer != ClerkIssuer {
		return nil, fmt.Errorf("invalid issuer: expected %s, got %s", ClerkIssuer, claims.Issuer)
	}

	// Expiration is automatically checked by jwt library due to RegisteredClaims

	return claims, nil
}
