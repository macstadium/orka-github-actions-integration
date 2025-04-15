// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package app

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func FetchAccessToken(ctx context.Context, envData *env.Data) (*types.AccessToken, error) {
	accessTokenJWT, err := createJWTForGitHubApp(envData.GitHubAppID, envData.GitHubAppPrivateKey)
	if err != nil {
		return nil, err
	}

	httpClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{
		Token:       accessTokenJWT,
		ContentType: "application/vnd.github+json",
	})
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/app/installations/%v/access_tokens", constants.BaseGitHubAPIPath, envData.GitHubAppInstallationID)

	return api.RequestJSON[any, types.AccessToken](ctx, httpClient.Client, http.MethodPost, path, nil)
}

func createJWTForGitHubApp(appID int64, privateKeyContent string) (string, error) {
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

	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyContent))
	if err != nil {
		return "", fmt.Errorf("error parsing PKCS#1 RSA private key: %v", err)
	}

	return token.SignedString(privateKey)
}
