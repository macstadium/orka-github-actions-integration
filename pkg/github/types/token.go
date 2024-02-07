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
