package remotedesktop

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/gorilla/websocket"
	guac "github.com/wwt/guac"
	"golang.org/x/crypto/ssh"
)

type jumpConnection struct {
	net.Conn
	jump *ssh.Client
}

func (c *jumpConnection) Close() error {
	err := c.Conn.Close()
	if jumpErr := c.jump.Close(); err == nil {
		err = jumpErr
	}
	return err
}

func dialSSHContext(ctx context.Context, client *ssh.Client, address string) (net.Conn, error) {
	type result struct {
		connection net.Conn
		err        error
	}
	ready := make(chan result, 1)
	go func() {
		connection, err := client.Dial("tcp", address)
		ready <- result{connection: connection, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = client.Close()
		return nil, ctx.Err()
	case result := <-ready:
		return result.connection, result.err
	}
}

func bridgeWebSocket(client *websocket.Conn, target net.Conn) error {
	errorsChannel := make(chan error, 2)
	go func() {
		for {
			messageType, data, err := client.ReadMessage()
			if err != nil {
				errorsChannel <- err
				return
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			if _, err = target.Write(data); err != nil {
				errorsChannel <- err
				return
			}
		}
	}()
	go func() {
		buffer := make([]byte, 32*1024)
		for {
			count, err := target.Read(buffer)
			if count > 0 {
				if writeErr := client.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
					errorsChannel <- writeErr
					return
				}
			}
			if err != nil {
				errorsChannel <- err
				return
			}
		}
	}()
	err := <-errorsChannel
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

type oneShotProxy struct {
	listener net.Listener
	target   net.Conn
	once     sync.Once
}

func newOneShotProxy(address string, target net.Conn) (*oneShotProxy, error) {
	listener, err := net.Listen("tcp4", net.JoinHostPort(address, "0"))
	if err != nil {
		return nil, err
	}
	proxy := &oneShotProxy{listener: listener, target: target}
	go proxy.accept()
	return proxy, nil
}

func (p *oneShotProxy) Port() int {
	return p.listener.Addr().(*net.TCPAddr).Port
}

func (p *oneShotProxy) accept() {
	connection, err := p.listener.Accept()
	if err != nil {
		_ = p.Close()
		return
	}
	defer connection.Close()
	_ = p.listener.Close()
	go func() {
		_, _ = io.Copy(connection, p.target)
		_ = p.Close()
	}()
	_, _ = io.Copy(p.target, connection)
	_ = p.Close()
}

func (p *oneShotProxy) Close() error {
	var result error
	p.once.Do(func() {
		if err := p.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = err
		}
		if err := p.target.Close(); result == nil {
			result = err
		}
	})
	return result
}

type managedTunnel struct {
	guac.Tunnel
	proxy   *oneShotProxy
	cleanup func()
	once    sync.Once
}

func (t *managedTunnel) Close() error {
	var result error
	t.once.Do(func() {
		result = t.Tunnel.Close()
		if proxyErr := t.proxy.Close(); result == nil {
			result = proxyErr
		}
		if t.cleanup != nil {
			t.cleanup()
		}
	})
	return result
}
