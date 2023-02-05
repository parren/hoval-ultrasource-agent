package ultrasource

import (
	"context"
	"log"
	"net"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
	_ "periph.io/x/host/v3/rpi"
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
}

type clientImpl struct {
	conn net.Conn
	xmit *socketcan.Transmitter
	recv *socketcan.Receiver
}

func NewClient(ctx context.Context) Client {
	conn, err := socketcan.DialContext(ctx, "can", "can0")
	if err != nil {
		log.Fatalf("Unable to dial can0: %v", err)
	}
	return &clientImpl{conn: conn,
		xmit: socketcan.NewTransmitter(conn),
		recv: socketcan.NewReceiver(conn)}
}

func (c *clientImpl) TransmitFrame(ctx context.Context, f can.Frame) error {
	return c.xmit.TransmitFrame(ctx, f)
}
func (c *clientImpl) Receive() bool    { return c.recv.Receive() }
func (c *clientImpl) Frame() can.Frame { return c.recv.Frame() }
