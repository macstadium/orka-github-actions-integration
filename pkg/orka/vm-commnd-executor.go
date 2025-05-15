package orka

import (
	"bufio"
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

func (executor *VMCommandExecutor) ExecuteCommands(commands ...string) error {
	sshConfig := &ssh.ClientConfig{
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		User:    executor.VMUsername,
		Auth:    []ssh.AuthMethod{ssh.Password(executor.VMPassword)},
		Timeout: time.Second * 10,
	}

	client, err := executor.connectWithRetries(sshConfig, fmt.Sprintf("%s:%d", executor.VMIP, executor.VMPort))
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

	err = session.Wait()
	if err != nil {
		return err
	}

	return nil
}

type FormatFunc func(string) string

func printFormattedOutput(reader io.Reader, format FormatFunc) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		fmt.Print(format(scanner.Text()))
	}
}

func (executor *VMCommandExecutor) connectWithRetries(cfg *ssh.ClientConfig, addr string) (*ssh.Client, error) {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		client, err := ssh.Dial("tcp", addr, cfg)
		if err == nil {
			return client, nil
		}

		fmt.Printf("Failed to connect to VM (attempt %d/%d): %v\n", attempt, maxRetries, err)

		if attempt < maxRetries {
			time.Sleep(3 * time.Second)
		} else {
			return nil, fmt.Errorf("failed to connect to VM after %d attempts: %v", maxRetries, err)
		}
	}

	return nil, fmt.Errorf("failed to connect to VM")
}
