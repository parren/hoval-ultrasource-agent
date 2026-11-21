package temperature

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	_ "periph.io/x/host/v3/rpi"
)

type Client interface {
	ReadTemp(id string) (float32, error)
}

// Connect 3.3V of the sensors to this pin. This allows us to reset the bus.
const powerPinName = "GPIO17"

type clientImpl struct {
	mu       sync.Mutex
	powerPin gpio.PinIO
}

func NewClient() (Client, error) {
	if _, err := driverreg.Init(); err != nil {
		return nil, fmt.Errorf("init periph drivers: %w", err)
	}
	powerPin := gpioreg.ByName(powerPinName)
	if powerPin == nil {
		return nil, fmt.Errorf("failed to find GPIO pin %v", powerPinName)
	}
	if err := powerPin.Out(gpio.High); err != nil {
		return nil, fmt.Errorf("set %v high: %w", powerPinName, err)
	}
	return &clientImpl{powerPin: powerPin}, nil
}

func (c *clientImpl) ReadTemp(id string) (float32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fn := fmt.Sprintf("/sys/bus/w1/devices/%s/temperature", id)
	bs, err := os.ReadFile(fn)
	if os.IsNotExist(err) {
		if resetErr := c.resetOnewireBus(); resetErr != nil {
			return 0, fmt.Errorf("failed to reset 1-wire bus: %w", resetErr)
		}
		bs, err = os.ReadFile(fn)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read temperature file %v: %w", fn, err)
	}
	s := strings.TrimSpace(string(bs))
	i, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse temperature %v from %v: %w", s, fn, err)
	}
	return float32(i) / 1000, nil
}

// https://forums.raspberrypi.com/viewtopic.php?t=164059
func (c *clientImpl) resetOnewireBus() error {
	log.Println("Resetting 1-wire bus")
	if err := c.powerPin.Out(gpio.Low); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	if err := c.powerPin.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(time.Second)
	return nil
}
