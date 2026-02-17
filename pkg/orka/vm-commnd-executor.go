package orka

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type VMCommandExecutor struct {
	VMIP       string
	VMPort     int
	VMName     string
	VMUsername string
	VMPassword string
	Logger     *zap.SugaredLogger
}

const (
	maxRetries = 20
)

func (executor *VMCommandExecutor) ExecuteCommands(ctx context.Context, commands ...string) error {
	executor.Logger.Infof("Starting execution on VM: %s (%s:%d)", executor.VMName, executor.VMIP, executor.VMPort)

	sshConfig := &ssh.ClientConfig{
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		User:    executor.VMUsername,
		Auth:    []ssh.AuthMethod{ssh.Password(executor.VMPassword)},
		Timeout: time.Second * 10,
	}

	client, err := executor.connectWithRetries(ctx, sshConfig, fmt.Sprintf("%s:%d", executor.VMIP, executor.VMPort))
	if err != nil {
		executor.Logger.Errorf("Failed to establish SSH connection to VM %s: %v", executor.VMName, err)
		return err
	}
	defer client.Close()

	executor.Logger.Infof("SSH connection established to VM %s", executor.VMName)

	session, err := client.NewSession()
	if err != nil {
		executor.Logger.Errorf("Failed to create SSH session on VM %s: %v", executor.VMName, err)
		return err
	}
	defer session.Close()

	executor.Logger.Infof("SSH session successfully created for VM %s", executor.VMName)

	stdout, err := session.StdoutPipe()
	if err != nil {
		executor.Logger.Errorf("Failed to setup stdout pipe: %v", err)
		return err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		executor.Logger.Errorf("Failed to setup stderr pipe: %v", err)
		return err
	}

	format := func(out string) string {
		return fmt.Sprintf("[VM] - %s - %s: %s\n", time.Now().Format(time.RFC3339), executor.VMName, out)
	}

	go printFormattedOutput(executor.Logger, "stdout", stdout, format)
	go printFormattedOutput(executor.Logger, "stderr", stderr, format)

	stdinBuf, err := session.StdinPipe()
	if err != nil {
		executor.Logger.Errorf("Failed to setup stdin pipe: %v", err)
		return err
	}

	err = session.Shell()
	if err != nil {
		executor.Logger.Errorf("Failed to start remote shell: %v", err)
		return err
	}
	executor.Logger.Infof("Remote shell started for VM %s", executor.VMName)

	_, err = stdinBuf.Write([]byte(strings.Join(commands, "\n") + "\nexit\n"))
	if err != nil {
		executor.Logger.Errorf("Failed to write commands to stdin: %v", err)
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	executor.Logger.Infof("Waiting for commands to finish execution on VM %s...", executor.VMName)

	select {
	case <-ctx.Done():
		executor.Logger.Warnf("Context canceled while waiting for execution on VM %s: %v", executor.VMName, ctx.Err())
		_ = session.Close()
		return ctx.Err()
	case err := <-done:
		if err != nil {
			var exitErr *ssh.ExitError

			if errors.As(err, &exitErr) {
				executor.Logger.Errorf("Command execution failed on VM %s with exit code %d: %v", executor.VMName, exitErr.ExitStatus(), err)
			} else {
				executor.Logger.Errorf("SSH connection dropped or protocol error on VM %s: %v", executor.VMName, err)
			}
		} else {
			executor.Logger.Infof("Execution completed successfully on VM %s", executor.VMName)
		}

		return err
	}
}

type FormatFunc func(string) string

func printFormattedOutput(logger *zap.SugaredLogger, streamName string, reader io.Reader, format FormatFunc) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		fmt.Print(format(scanner.Text()))
	}

	if err := scanner.Err(); err != nil {
		logger.Errorf("Error reading from %s: %v", streamName, err)
	} else {
		logger.Infof("Reached EOF for %s", streamName)
	}
}

func (executor *VMCommandExecutor) connectWithRetries(ctx context.Context, cfg *ssh.ClientConfig, addr string) (*ssh.Client, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			executor.Logger.Warnf("Context canceled during connection retry loop: %v", ctx.Err())
			return nil, ctx.Err()
		}

		client, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			executor.Logger.Infof("Connected to %s on attempt %d", addr, attempt)
			return client, nil
		}

		executor.Logger.Warnf("Failed to connect to VM (attempt %d/%d): %v", attempt, maxRetries, err)

		select {
		case <-ctx.Done():
			executor.Logger.Warnf("Context canceled while backing off before retry: %v", ctx.Err())
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	err := fmt.Errorf("failed to connect to VM after %d attempts", maxRetries)
	executor.Logger.Errorf("%v", err)
	return nil, err
}
