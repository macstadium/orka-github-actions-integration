package exec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func ExecStringCommand(command string, args []string) (string, error) {
	cmd := exec.Command(command, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command '%s %s' failed with output: %q, error: %v", command, strings.Join(args, " "), out, err)
	}

	return string(out), nil
}

func ExecJSONCommand[Res any](command string, args []string) (*Res, error) {
	cmd := exec.Command(command, args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("command '%s %s' failed with output: %q, error: %v", command, strings.Join(args, " "), out, err)
	}

	responseModel := new(Res)
	if err = json.NewDecoder(bytes.NewReader(out)).Decode(&responseModel); err != nil {
		return nil, err
	}

	return responseModel, err
}
