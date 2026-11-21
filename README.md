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

  * The main entry point is `cmd/ultrasource/main.go` (`agent`, `monitor`, `analyze`, `query`, `set`).
  * The core agent package is `pkg/agent` (`agent.go`, `settings.go`, `agent_test.go`).

To cross-compile the CLI on a regular Linux machine for the Raspberry's ARM chip, use something like:

```shell
$ env GOOS=linux GOARCH=arm GOARM=5 go build -o raspi-ultrasource ./cmd/ultrasource
```

## Deployment

The `deploy/` folder contains template `systemd` files (`80-can.network` for 50 kbps CAN bus setup and `ultrasource-agent.service`).
The agent expects your Google API credentials in a file called `google-api-credentials.json` (or `--google-api-credentials-file`) and your Google Sheet ID via `--google-sheet-id`.

## Tools

Subcommands useful for debugging or reverse-engineering Hoval's protocol:

  * `ultrasource analyze --log <candump.log>` parses [`candump`](https://manpages.debian.org/testing/can-utils/candump.1.en.html) output.
  * `ultrasource monitor` queries and prints live answers from the CAN bus.
  * `ultrasource query <setting>` and `ultrasource set <setting> <value>` send single CAN frames.
