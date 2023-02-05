# Hoval Ultrasource Remote Control for Raspberry Pi

The goal is to remotely monitor and control a Hoval Ultrasource heat pump in a vacation home.
The controlled settings are desired room and water temperature.

The user interface is a Google Sheet.
See [this template](https://docs.google.com/spreadsheets/d/18_j9LVVCgPrRev3wAthHw0d9p4yPh64YbrP00OXtNOE/edit#gid=0).
The agent uploads readings to the sheet and reads new desired settings from the sheet.
Thus the agent doesn't need to be reachable from the internet.
It only does outbound connections to the Google Sheet.

The agent runs on a Raspberry Pi Zero (or better).
It talks to a Hoval Ultrasource heater via CAN bus (for example on the service port).
It can also read 1-wire DS18B20 temperature sensors (since my heat pump does not have its own room sensors).

## Google Sheet API

Configure a service account (https://cloud.google.com/iam/docs/creating-managing-service-accounts). 
Allow it to authenticate using a private key (https://cloud.google.com/iam/docs/creating-managing-service-account-keys).
Save the private key as `configs/hoval-ultrasource-service-key.json`.

Copy the [template sheet](https://docs.google.com/spreadsheets/d/18_j9LVVCgPrRev3wAthHw0d9p4yPh64YbrP00OXtNOE/edit#gid=0) to create your own.
Then give your service account's email write access to this new Google Sheet.

## Hoval Ultrasource CAN bus

The [front service port on the Ultrasource](https://docs.google.com/document/d/1T8LvJBhFbQpsEJV_q2CthpmyqUR-UleQVFUQvEvvX_k/edit#) is a Molex Mini-Fit Jr. connector.
The port is behind the panel that also hides the reset button.
The CAN signal is on  the two leftmost pins (upper pin is CAN high, lower pin is CAN low).
I use a [2-pin Molex 2451350210 cable](https://www.molex.com/molex/products/part-detail/cable_assemblies/2451350210).

> If you want the 12V power too, you can use a [4-pin Molex 2451350420 cable](https://www.molex.com/molex/products/part-detail/cable_assemblies/2451350420).
`GND` is the upper middle pin, `12V+` is the lower middle pin.
I tried with a [LM2596 DC-DC Step-Down converter](https://www.bastelgarage.ch/5v-3a-lm2596-dc-dc-step-down-mit-usb).
But attaching the Ultrasource’s `V+` to `IN+` and `GND` to `IN-` immediately reboots the Ultrasource’s display
(but not the core controller).
I never tried what happens if I leave it connected.

For CAN on the Raspberry Pi, I use a [WaveShare RS485/CAN hat](https://www.waveshare.com/wiki/RS485_CAN_HAT).

## Hoval Ultrasource API on the CAN bus

The Ultrasource's API on the CAN bus is handled in `pkg/ultrasource`.
Hoval implements something like ModBus over CAN (see `pkg/ultrasource/hovalmsg.go`),
so [their ModBus documentation](https://www.hoval.com/misc/TTE/TTE-GW-Modbus-datapoints.xlsx) applies.
The settings I extracted are in `pkg/ultrasource/ultramsg.go`.

This code is heavily inspired by https://github.com/zittix/Hoval-GW and https://github.com/chrishrb/hoval-gateway.

## Agent Code

  * The main entry point is `cmd/agent/main.go`. It defines all the flags.
  * The actual functionality is in `internal/agent.go`. It has an `agent_test.go` for the main scenarios.

To cross-compile the agent on a regular Linux machine for the Raspberry's ARM chip, use something like:

```shell
$ env GOOS=linux GOARCH=arm GOARM=5 go build -o raspi-agent cmd/agent/main.go
```

## Scripts

The `scripts/` folder has a bunch of useful bash scripts I use to configure the Raspberry Pi to:

  * Enable the CAN bus and run the agent on boot.
  * Reboot when WiFi is lost ([context](https://weworkweplay.com/play/rebooting-the-raspberry-pi-when-it-loses-wireless-connection-wifi/)).

## Tools

The other Go programs were useful when reverse-engineering Hoval's protocol:

  * `cmd/analyze/main.go` can be run over
    [`candump`](https://manpages.debian.org/testing/can-utils/candump.1.en.html) output.
  * `cmd/logger/main.go` can be run to monitor online what happens on the bus as you
    modify settings directly on the pump's control screen.
