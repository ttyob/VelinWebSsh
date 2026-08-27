package terminal

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

// commandRunner keeps one non-interactive shell open for short tmux control
// commands. Opening a fresh SSH channel for every scrollbar movement adds a
// full network round trip before tmux can redraw the pane.
type commandRunner struct {
	mu      sync.Mutex
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	marker  string
	closed  bool
}

func newCommandRunner(client *ssh.Client) (*commandRunner, error) {
	runner := &commandRunner{
		client: client,
		marker: "VELIN_" + uuid.NewString(),
	}
	if err := runner.start(); err != nil {
		return nil, err
	}
	return runner, nil
}

func (r *commandRunner) start() error {
	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return err
	}
	if err = session.Start("exec /bin/sh"); err != nil {
		_ = session.Close()
		return err
	}
	r.session = session
	r.stdin = stdin
	r.stdout = bufio.NewReader(stdout)
	return nil
}

func (r *commandRunner) Run(command string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.session == nil || r.stdin == nil || r.stdout == nil {
		return nil, errors.New("persistent command channel is closed")
	}

	// Commands are generated internally and already shell-escaped. Redirect the
	// command's stderr into stdout so one ordered stream can be read to a unique
	// completion marker.
	wrapped := fmt.Sprintf(
		"{ %s; } 2>&1; __velin_status=$?; printf '\\036%s:%%d\\037\\n' \"$__velin_status\"\n",
		command,
		r.marker,
	)
	if _, err := io.WriteString(r.stdin, wrapped); err != nil {
		r.closeLocked()
		return nil, err
	}

	prefix := []byte("\x1e" + r.marker + ":")
	var output bytes.Buffer
	for {
		line, err := r.stdout.ReadBytes('\n')
		if len(line) > 0 {
			if markerAt := bytes.Index(line, prefix); markerAt >= 0 {
				output.Write(line[:markerAt])
				statusStart := markerAt + len(prefix)
				statusEnd := bytes.IndexByte(line[statusStart:], '\x1f')
				if statusEnd < 0 {
					r.closeLocked()
					return output.Bytes(), errors.New("invalid persistent command response")
				}
				status, parseErr := strconv.Atoi(string(line[statusStart : statusStart+statusEnd]))
				if parseErr != nil {
					r.closeLocked()
					return output.Bytes(), errors.New("invalid persistent command status")
				}
				if status != 0 {
					return output.Bytes(), fmt.Errorf("remote command exited with status %d", status)
				}
				return output.Bytes(), nil
			}
			output.Write(line)
		}
		if err != nil {
			r.closeLocked()
			return output.Bytes(), err
		}
	}
}

func (r *commandRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closeLocked()
}

func (r *commandRunner) closeLocked() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.stdin != nil {
		_ = r.stdin.Close()
	}
	if r.session != nil {
		return r.session.Close()
	}
	return nil
}
