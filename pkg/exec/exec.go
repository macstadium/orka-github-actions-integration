package exec

import (
	"bytes"
	"encoding/json"
	"os/exec"
)

func ExecStringCommand(command string, args []string) (string, error) {
	cmd := exec.Command(command, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

func ExecJSONCommand[Res any](command string, args []string) (*Res, error) {
	cmd := exec.Command(command, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	responseModel := new(Res)
	if err = json.NewDecoder(bytes.NewReader(out)).Decode(&responseModel); err != nil {
		return nil, err
	}

	return responseModel, err
}
