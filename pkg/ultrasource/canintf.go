package ultrasource

import (
	"context"
	"fmt"
	"net"
	"sync"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
)

type Transmitter interface {
	TransmitFrame(context.Context, can.Frame) error
}

type Receiver interface {
	Receive() bool
	Frame() can.Frame
}

type Client interface {
	Transmitter
	Receiver
	Reconnect(context.Context) error
	Close() error
}

type clientImpl struct {
	iface string
	mu    sync.RWMutex
	conn  net.Conn
	xmit  *socketcan.Transmitter
	recv  *socketcan.Receiver
}

func NewClient(ctx context.Context) (Client, error) {
	return NewInterfaceClient(ctx, "can0")
}

func NewInterfaceClient(ctx context.Context, iface string) (Client, error) {
	c := &clientImpl{iface: iface}
	if err := c.Reconnect(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *clientImpl) Reconnect(ctx context.Context) error {
	conn, err := socketcan.DialContext(ctx, "can", c.iface)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.iface, err)
	}
	c.mu.Lock()
	old := c.conn
	c.conn = conn
	c.xmit = socketcan.NewTransmitter(conn)
	c.recv = socketcan.NewReceiver(conn)
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (c *clientImpl) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (c *clientImpl) TransmitFrame(ctx context.Context, f can.Frame) error {
	c.mu.RLock()
	xmit := c.xmit
	c.mu.RUnlock()
	if xmit == nil {
		return fmt.Errorf("can interface %s not connected", c.iface)
	}
	return xmit.TransmitFrame(ctx, f)
}

func (c *clientImpl) Receive() bool {
	c.mu.RLock()
	recv := c.recv
	c.mu.RUnlock()
	if recv == nil {
		return false
	}
	return recv.Receive()
}

func (c *clientImpl) Frame() can.Frame {
	c.mu.RLock()
	recv := c.recv
	c.mu.RUnlock()
	if recv == nil {
		return can.Frame{}
	}
	return recv.Frame()
}
