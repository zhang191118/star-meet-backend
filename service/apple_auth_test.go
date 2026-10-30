package service

import (
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestHexSHA256(t *testing.T) {
	// 已知向量：sha256("starmeet-nonce")
	got := hexSHA256("starmeet-nonce")
	want := "5ae3adaddca1444080689a6a52fa5a8b7daf4eabc7bbd28bb22cee4e823b54db"
	if got != want {
		t.Fatalf("hexSHA256 = %q, want %q", got, want)
	}
}

func TestAppleTokenFailureClass(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "none"},
		{"expired", jwt.ErrTokenExpired, "expired"},
		{"issuer", jwt.ErrTokenInvalidIssuer, "issuer_mismatch"},
		{"audience", jwt.ErrTokenInvalidAudience, "audience_mismatch"},
		{"signature", jwt.ErrTokenSignatureInvalid, "signature_invalid"},
		{"malformed", jwt.ErrTokenMalformed, "malformed"},
		{"kid not found", errAppleKeyNotFound, "kid_not_found"},
		{"jwks fetch", errAppleKeyFetch, "jwks_fetch_failed"},
		{"wrapped expired", errors.Join(errors.New("ctx"), jwt.ErrTokenExpired), "expired"},
		{"other", errors.New("boom"), "other"},
	}
	for _, tc := range cases {
		if got := appleTokenFailureClass(tc.err); got != tc.want {
			t.Errorf("%s: appleTokenFailureClass = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestValidateAppleIdentityTokenNotConfigured(t *testing.T) {
	t.Setenv("APPLE_CLIENT_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "")
	if _, err := validateAppleIdentityToken("token", "user", ""); !errors.Is(err, ErrThirdPartyUnavailable) {
		t.Fatalf("err = %v, want ErrThirdPartyUnavailable", err)
	}
}

func TestValidateAppleIdentityTokenMalformed(t *testing.T) {
	t.Setenv("APPLE_CLIENT_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "com.guoliang.starmeet")
	if _, err := validateAppleIdentityToken("not-a-jwt", "user", ""); err == nil || err.Error() != "invalid Apple identity token" {
		t.Fatalf("err = %v, want invalid Apple identity token", err)
	}
}

// 非 RS256 签名的 token 必须被拒绝，且不应触发 JWKS 拉取
func TestValidateAppleIdentityTokenRejectsHS256(t *testing.T) {
	t.Setenv("APPLE_CLIENT_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "com.guoliang.starmeet")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://appleid.apple.com",
		"aud": "com.guoliang.starmeet",
		"sub": "user1",
	})
	signed, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}
	if _, err := validateAppleIdentityToken(signed, "user1", ""); err == nil || err.Error() != "invalid Apple identity token" {
		t.Fatalf("err = %v, want invalid Apple identity token", err)
	}
}
