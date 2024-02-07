package types

import "time"

type Runner struct {
	Id                 int              `json:"id,omitempty"`
	Name               string           `json:"name,omitempty"`
	RunnerGroupId      int              `json:"runnerGroupId,omitempty"`
	RunnerGroupName    string           `json:"runnerGroupName,omitempty"`
	Labels             []RunnerLabel    `json:"labels,omitempty"`
	RunnerSetting      RunnerSetting    `json:"RunnerSetting,omitempty"`
	CreatedOn          time.Time        `json:"createdOn,omitempty"`
	RunnerJitConfigUrl string           `json:"runnerJitConfigUrl,omitempty"`
	Statistics         *RunnerStatistic `json:"statistics,omitempty"`
}

type RunnerLabel struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type RunnerSetting struct {
	Ephemeral     bool `json:"ephemeral,omitempty"`
	IsElastic     bool `json:"isElastic,omitempty"`
	DisableUpdate bool `json:"disableUpdate,omitempty"`
}

type RunnerStatistic struct {
	TotalAvailableJobs     int `json:"totalAvailableJobs"`
	TotalAcquiredJobs      int `json:"totalAcquiredJobs"`
	TotalAssignedJobs      int `json:"totalAssignedJobs"`
	TotalRunningJobs       int `json:"totalRunningJobs"`
	TotalRegisteredRunners int `json:"totalRegisteredRunners"`
	TotalBusyRunners       int `json:"totalBusyRunners"`
	TotalIdleRunners       int `json:"totalIdleRunners"`
}

type RunnersListResponse struct {
	Count   int      `json:"count"`
	Runners []Runner `json:"value"`
}
