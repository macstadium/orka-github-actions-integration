package orka

type OrkaVMDeployResponseModel struct {
	Name         string  `json:"name"`
	Node         string  `json:"node"`
	Memory       string  `json:"memory"`
	IP           string  `json:"ip"`
	SSH          *int    `json:"ssh,omitempty"`
	VNC          *int    `json:"vnc,omitempty"`
	Screenshare  *int    `json:"screenshare,omitempty"`
	Status       VMPhase `json:"status"`
	PortWarnings string  `json:"portWarnings,omitempty"`
}

type VMPhase string

const (
	// VMRunning indicates that the VirtualMachineInstance is successfully deployed and running
	VMRunning VMPhase = "Running"
	// VMFailed indicates that the corresponding VirtualMachineInstance pod is NOT in a running phase and there are errors in its status field
	VMFailed VMPhase = "Failed"
	// VMPending indicates that the corresponding VirtualMachineInstance is currently deploying and still not running
	VMPending VMPhase = "Pending"
)

type OrkaVMConfigResponseModel struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	CPU   int    `json:"cpu"`
	Type  string `json:"type"`
}

type OrkaImageResponseModel struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type OrkaClusterInfoResponseModel struct {
	ApiEndpoint       string `json:"apiEndpoint"`
	CertData          string `json:"certData"`
	AppClientId       string `json:"appClientId"`
	BaseOauthEndpoint string `json:"baseOauthEndpoint"`
}
