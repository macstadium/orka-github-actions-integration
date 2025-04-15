// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package types

import "time"

type AccessToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RegistrationToken struct {
	Token     string    `json:"token,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type RegistrationPayload struct {
	Url         string `json:"url"`
	RunnerEvent string `json:"runner_event"`
}

type AuthorizationInfo struct {
	AdminToken        string `json:"token,omitempty"`
	ActionsServiceUrl string `json:"url,omitempty"`
}
