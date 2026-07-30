package scalesetclient

import "github.com/macstadium/orka-github-actions-integration/pkg/github/actions"

var _ actions.ActionsService = (*Client)(nil)
