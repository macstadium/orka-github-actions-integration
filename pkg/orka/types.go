package orka

import "fmt"

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

type OrkaVMInfo struct {
	Name   string  `json:"name"`
	IP     string  `json:"ip"`
	SSH    *int    `json:"ssh,omitempty"`
	Status VMPhase `json:"status"`
}

// EmulatorPhase mirrors the AndroidEmulatorInstance status phase. It shares its values with VMPhase
// but is a distinct enum in the cluster, so it is kept separate here.
type EmulatorPhase string

const (
	// EmulatorRunning indicates the emulator is deployed and its relay is reachable
	EmulatorRunning EmulatorPhase = "Running"
	// EmulatorFailed indicates the emulator pod is not running and its status carries an error
	EmulatorFailed EmulatorPhase = "Failed"
	// EmulatorPending indicates the emulator is still deploying
	EmulatorPending EmulatorPhase = "Pending"
)

// OrkaEmulatorResponseModel is one element of the JSON array returned by both
// 'orka3 emulator deploy -o json' and 'orka3 emulator list -o json'. Platform, image type, and
// device profile come back resolved from the emulator config that produced the emulator.
//
// Note the relay key is "relayIp" with a lowercase p, which differs from the CRD's relayIP field.
type OrkaEmulatorResponseModel struct {
	Name          string        `json:"name"`
	VM            string        `json:"vm"`
	Status        EmulatorPhase `json:"status"`
	Node          *string       `json:"node,omitempty"`
	Platform      string        `json:"platform"`
	ImageType     string        `json:"imageType"`
	DeviceProfile *string       `json:"deviceProfile,omitempty"`
	RelayIP       *string       `json:"relayIp,omitempty"`
	RelayPort     *int          `json:"relayPort,omitempty"`
}

// HasRelay reports whether the emulator has been assigned a relay address. A Pending emulator has
// neither, so this must be checked before reading the relay fields.
func (emulator *OrkaEmulatorResponseModel) HasRelay() bool {
	return emulator.RelayIP != nil && *emulator.RelayIP != "" && emulator.RelayPort != nil
}

// ADBTarget is the host:port a job running inside the paired VM connects to with 'adb connect'.
// The address is only routable from that VM, so it must never be translated through
// ORKA_NODE_IP_MAPPING the way a VM's own SSH address is.
func (emulator *OrkaEmulatorResponseModel) ADBTarget() string {
	if !emulator.HasRelay() {
		return ""
	}

	return fmt.Sprintf("%s:%d", *emulator.RelayIP, *emulator.RelayPort)
}

// OrkaEmulatorConfigResponseModel is one element of 'orka3 emulator-config list -o json'. Only the
// name is used, to validate the configured names at startup.
type OrkaEmulatorConfigResponseModel struct {
	Name      string `json:"name"`
	Platform  string `json:"platform,omitempty"`
	ImageType string `json:"imageType,omitempty"`
}

// EmulatorSpec says what to deploy. Config names an AndroidEmulatorConfig and is preferred, since
// the cluster then owns the platform, system image, device profile, and sizing. The inline fields
// are the fallback for clusters whose CLI predates named configs, and can be dropped once every
// cluster we target supports them.
type EmulatorSpec struct {
	Config string

	Platform      string
	ImageType     string
	DeviceProfile string
}

// deployArgs renders the spec as flags for 'orka3 emulator deploy'.
func (spec EmulatorSpec) deployArgs() []string {
	if spec.Config != "" {
		return []string{"--config", spec.Config}
	}

	args := []string{"--platform", spec.Platform, "--image-type", spec.ImageType}
	if spec.DeviceProfile != "" {
		args = append(args, "--device-profile", spec.DeviceProfile)
	}

	return args
}

// Label identifies the spec in logs and in the job-facing ORKA_EMULATOR_<n>_CONFIG variable: the
// config name where there is one, otherwise the inline values that stand in for it.
func (spec EmulatorSpec) Label() string {
	if spec.Config != "" {
		return spec.Config
	}

	if spec.DeviceProfile != "" {
		return fmt.Sprintf("%s/%s/%s", spec.Platform, spec.ImageType, spec.DeviceProfile)
	}

	return fmt.Sprintf("%s/%s", spec.Platform, spec.ImageType)
}
