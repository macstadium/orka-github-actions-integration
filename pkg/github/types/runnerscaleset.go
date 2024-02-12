package types

import (
	"time"

	"github.com/google/uuid"
)

type RunnerScaleSet struct {
	Id                 int                      `json:"id,omitempty"`
	Name               string                   `json:"name,omitempty"`
	RunnerGroupId      int                      `json:"runnerGroupId,omitempty"`
	RunnerGroupName    string                   `json:"runnerGroupName,omitempty"`
	Labels             []RunnerScaleSetLabel    `json:"labels,omitempty"`
	RunnerSetting      RunnerScaleSetSetting    `json:"RunnerSetting,omitempty"`
	CreatedOn          time.Time                `json:"createdOn,omitempty"`
	RunnerJitConfigUrl string                   `json:"runnerJitConfigUrl,omitempty"`
	Statistics         *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

type RunnerScaleSetLabel struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type RunnerScaleSetSetting struct {
	Ephemeral     bool `json:"ephemeral,omitempty"`
	IsElastic     bool `json:"isElastic,omitempty"`
	DisableUpdate bool `json:"disableUpdate,omitempty"`
}

type RunnersListResponse struct {
	Count   int              `json:"count"`
	Runners []RunnerScaleSet `json:"value"`
}

type RunnerScaleSetSession struct {
	SessionId               *uuid.UUID               `json:"sessionId,omitempty"`
	OwnerName               string                   `json:"ownerName,omitempty"`
	RunnerScaleSet          *RunnerScaleSet          `json:"runnerScaleSet,omitempty"`
	MessageQueueUrl         string                   `json:"messageQueueUrl,omitempty"`
	MessageQueueAccessToken string                   `json:"messageQueueAccessToken,omitempty"`
	Statistics              *RunnerScaleSetStatistic `json:"statistics,omitempty"`
}

type RunnerScaleSetMessage struct {
	MessageId   int64                    `json:"messageId"`
	MessageType string                   `json:"messageType"`
	Body        string                   `json:"body"`
	Statistics  *RunnerScaleSetStatistic `json:"statistics"`
}

type RunnerScaleSetStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

type RunnerScaleSetJitRunnerSetting struct {
	Name       string `json:"name"`
	WorkFolder string `json:"workFolder"`
}

type RunnerScaleSetJitRunnerConfig struct {
	Runner           *RunnerReference `json:"runner"`
	EncodedJITConfig string           `json:"encodedJITConfig"`
}

type RunnerReference struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	RunnerScaleSetId int    `json:"runnerScaleSetId"`
}
