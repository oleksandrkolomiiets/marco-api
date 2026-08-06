package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func newTestJWTService() *JWTService {
	return NewJWTService(testSecret, 15*time.Minute, 30*24*time.Hour)
}

func TestJWT_GenerateAndValidateRoundTrip(t *testing.T) {
	svc := newTestJWTService()
	userID := uuid.New()

	token, err := svc.GenerateAccessToken(userID, "premium", uuid.New())
	require.NoError(t, err)

	claims, err := svc.ValidateAccessToken(token)
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, "premium", claims.Plan)
}

func TestJWT_RejectsExpiredToken(t *testing.T) {
	expired := NewJWTService(testSecret, -time.Minute, time.Hour)
	token, err := expired.GenerateAccessToken(uuid.New(), "free", uuid.New())
	require.NoError(t, err)

	_, err = newTestJWTService().ValidateAccessToken(token)
	assert.Error(t, err)
}

func TestJWT_RejectsWrongSecret(t *testing.T) {
	other := NewJWTService("ffffffffffffffffffffffffffffffff", time.Minute, time.Hour)
	token, err := other.GenerateAccessToken(uuid.New(), "free", uuid.New())
	require.NoError(t, err)

	_, err = newTestJWTService().ValidateAccessToken(token)
	assert.Error(t, err)
}

func TestJWT_RejectsNoneAlgorithm(t *testing.T) {
	// Classic alg-confusion: an attacker strips the signature and claims
	// "alg":"none". ValidateAccessToken must only accept HMAC.
	claims := Claims{
		UserID: uuid.New(),
		Plan:   "premium",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	token, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = newTestJWTService().ValidateAccessToken(token)
	assert.Error(t, err)
}

func TestJWT_RejectsGarbage(t *testing.T) {
	for _, tc := range []string{"", "not.a.jwt", "aaaa.bbbb.cccc"} {
		_, err := newTestJWTService().ValidateAccessToken(tc)
		assert.Error(t, err, "token %q must be rejected", tc)
	}
}

func TestGenerateRefreshToken_RawAndHashMatch(t *testing.T) {
	svc := newTestJWTService()
	raw, hash, err := svc.GenerateRefreshToken()
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	sum := sha256.Sum256([]byte(raw))
	assert.Equal(t, hex.EncodeToString(sum[:]), hash)
	assert.Equal(t, hash, HashRefreshToken(raw))
}

func TestGenerateRefreshToken_Unique(t *testing.T) {
	svc := newTestJWTService()
	raw1, _, err := svc.GenerateRefreshToken()
	require.NoError(t, err)
	raw2, _, err := svc.GenerateRefreshToken()
	require.NoError(t, err)
	assert.NotEqual(t, raw1, raw2)
}
