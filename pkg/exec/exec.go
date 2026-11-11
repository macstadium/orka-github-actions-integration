package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ExecStringCommand runs a command and returns its combined output. The process is killed when ctx
// is done, and the returned error then wraps ctx.Err() so callers can tell cancellation apart from
// a command that failed on its own.
func ExecStringCommand(ctx context.Context, command string, args []string) (string, error) {
	out, err := run(ctx, command, args)
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// ExecJSONCommand runs a command and decodes its output as JSON into Res. Cancellation behaves as
// for ExecStringCommand.
func ExecJSONCommand[Res any](ctx context.Context, command string, args []string) (*Res, error) {
	out, err := run(ctx, command, args)
	if err != nil {
		return nil, err
	}

	responseModel := new(Res)
	if err = json.NewDecoder(bytes.NewReader(out)).Decode(&responseModel); err != nil {
		return nil, err
	}

	return responseModel, nil
}

func run(ctx context.Context, command string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)

	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}

	// A killed process reports only "signal: killed", which hides why it was killed. A process that
	// exited on its own just before ctx was done failed for its own reasons, so it keeps its output.
	if ctxErr := ctx.Err(); ctxErr != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
		return nil, fmt.Errorf("command '%s %s' was interrupted: %w", command, strings.Join(args, " "), ctxErr)
	}

	return nil, fmt.Errorf("command '%s %s' failed with output: %q, error: %v", command, strings.Join(args, " "), out, err)
}
