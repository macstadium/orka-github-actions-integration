package orka

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type VMCommandExecutor struct {
	VMIP       string
	VMPort     int
	VMName     string
	VMUsername string
	VMPassword string
}

const (
	maxRetries = 20
)

func (executor *VMCommandExecutor) ExecuteCommands(ctx context.Context, commands ...string) error {
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
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	format := func(out string) string {
		return fmt.Sprintf("[VM] - %s - %s: %s\n", time.Now().Format(time.RFC3339), executor.VMName, out)
	}

	go printFormattedOutput(stdout, format)
	go printFormattedOutput(stderr, format)

	stdinBuf, err := session.StdinPipe()
	if err != nil {
		return err
	}

	err = session.Shell()
	if err != nil {
		return err
	}

	_, err = stdinBuf.Write([]byte(strings.Join(commands, "\n") + "\nexit\n"))
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	case err := <-done:
		return err
	}
}

type FormatFunc func(string) string

func printFormattedOutput(reader io.Reader, format FormatFunc) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		fmt.Print(format(scanner.Text()))
	}
}

func (executor *VMCommandExecutor) connectWithRetries(ctx context.Context, cfg *ssh.ClientConfig, addr string) (*ssh.Client, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		client, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			return client, nil
		}

		fmt.Printf("Failed to connect to VM (attempt %d/%d): %v\n", attempt, maxRetries, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	return nil, fmt.Errorf("failed to connect to VM after %d attempts", maxRetries)
}
