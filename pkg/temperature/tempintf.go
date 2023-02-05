package temperature

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"periph.io/x/conn/v3/driver/driverreg"
	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	_ "periph.io/x/host/v3/rpi"
)

type (
	TemperatureReading struct {
		Id          string
		Temperature float32
		Error       error
	}

	Client interface {
		RequestTemp(id string)
		TemperatureReadings() <-chan TemperatureReading
	}
)

// Connect 3.3V of the sensors to this pin. This allows us to reset the bus.
const powerPinName = "GPIO17"

type clientImpl struct {
	powerPin            gpio.PinIO
	ch                  chan TemperatureReading
	temperatureReadings <-chan TemperatureReading
}

func NewClient() Client {
	if _, err := driverreg.Init(); err != nil {
		log.Fatal(err)
	}
	powerPin := gpioreg.ByName(powerPinName)
	if powerPin == nil {
		log.Fatalf("Failed to find %v\n", powerPinName)
	}
	powerPin.Out(gpio.High)
	ch := make(chan TemperatureReading)
	return &clientImpl{powerPin: powerPin, ch: ch, temperatureReadings: ch}
}

func (c *clientImpl) TemperatureReadings() <-chan TemperatureReading {
	return c.temperatureReadings
}

func (c *clientImpl) RequestTemp(id string) {
	go func() {
		t, err := c.readTemp(id)
		c.ch <- TemperatureReading{Id: id, Temperature: t, Error: err}
	}()
}

func (c *clientImpl) readTemp(id string) (float32, error) {
	fn := fmt.Sprintf("/sys/bus/w1/devices/%s/temperature", id)
	bs, err := os.ReadFile(fn)
	if os.IsNotExist(err) {
		err = c.resetOnewireBus()
		if err != nil {
			return 0, fmt.Errorf("failed to reset 1-wire bus: %v", err)
		}
		bs, err = os.ReadFile(fn)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read temperature file %v: %v", fn, err)
	}
	s := strings.TrimSpace(string(bs))
	i, err := strconv.ParseInt(string(s), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to parse temperature %v from %v: %v", s, fn, err)
	}
	return float32(i) / 1000, nil
}

// https://forums.raspberrypi.com/viewtopic.php?t=164059
func (c *clientImpl) resetOnewireBus() (err error) {
	log.Println("Resetting 1-wire bus")
	err = c.powerPin.Out(gpio.Low)
	if err != nil {
		return
	}
	time.Sleep(time.Second * 3)
	err = c.powerPin.Out(gpio.High)
	return
}
