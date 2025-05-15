// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package types

import "time"

type AcquirableJobList struct {
	Count int             `json:"count"`
	Jobs  []AcquirableJob `json:"value"`
}

type AcquirableJob struct {
	AcquireJobUrl   string   `json:"acquireJobUrl"`
	MessageType     string   `json:"messageType"`
	RunnerRequestId int64    `json:"runnerRequestId"`
	RepositoryName  string   `json:"repositoryName"`
	OwnerName       string   `json:"ownerName"`
	JobWorkflowRef  string   `json:"jobWorkflowRef"`
	EventName       string   `json:"eventName"`
	RequestLabels   []string `json:"requestLabels"`
}

type JobAvailable struct {
	AcquireJobUrl string `json:"acquireJobUrl"`
	JobMessageBase
}

type JobAssigned struct {
	JobMessageBase
}

type JobStarted struct {
	RunnerId   int    `json:"runnerId"`
	RunnerName string `json:"runnerName"`
	JobMessageBase
}

type JobCompleted struct {
	Result     string `json:"result"`
	RunnerId   int    `json:"runnerId"`
	RunnerName string `json:"runnerName"`
	JobMessageBase
}

type JobMessageType struct {
	MessageType string `json:"messageType"`
}

type JobMessageBase struct {
	JobMessageType
	JobId              string    `json:"jobId"`
	RunnerRequestId    int64     `json:"runnerRequestId"`
	RepositoryName     string    `json:"repositoryName"`
	OwnerName          string    `json:"ownerName"`
	JobWorkflowRef     string    `json:"jobWorkflowRef"`
	JobDisplayName     string    `json:"jobDisplayName"`
	WorkflowRunId      int64     `json:"workflowRunId"`
	EventName          string    `json:"eventName"`
	RequestLabels      []string  `json:"requestLabels"`
	QueueTime          time.Time `json:"queueTime"`
	ScaleSetAssignTime time.Time `json:"scaleSetAssignTime"`
	RunnerAssignTime   time.Time `json:"runnerAssignTime"`
	FinishTime         time.Time `json:"finishTime"`
}

type Int64List struct {
	Count int     `json:"count"`
	Value []int64 `json:"value"`
}
