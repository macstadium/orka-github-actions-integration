package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func FetchAccessToken(ctx context.Context, envData *env.Data, httpClient *retryablehttp.Client) (*types.AccessToken, error) {
	accessTokenJWT, err := createJWTForGitHubApp(envData.GitHubAppID, envData.GitHubAppPrivateKeyPath)
	if err != nil {
		return nil, err
	}

	httpClient.Transport = &retryablehttp.ClientTransport{
		Token:       accessTokenJWT,
		ContentType: "application/vnd.github+json",
	}

	path := fmt.Sprintf("%s/app/installations/%v/access_tokens", constants.BaseGitHubAPIPath, envData.GitHubAppInstallationID)

	return api.RequestJSON[any, types.AccessToken](ctx, httpClient, http.MethodPost, path, nil)
}

func createJWTForGitHubApp(appID int64, privateKeyPath string) (string, error) {
	// Encode as JWT
	// See https://docs.github.com/en/developers/apps/building-github-apps/authenticating-with-github-apps#authenticating-as-a-github-app

	// Going back in time a bit helps with clock skew.
	issuedAt := time.Now().Add(-60 * time.Second)
	// Max expiration date is 10 minutes.
	expiresAt := issuedAt.Add(9 * time.Minute)
	claims := &jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		Issuer:    strconv.FormatInt(appID, 10),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)

	privateKeyContent, err := os.ReadFile(privateKeyPath)
	if err != nil {
		panic(err)
	}

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyContent))
	if err != nil {
		return "", err
	}

	return token.SignedString(privateKey)
}
