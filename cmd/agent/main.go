package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"parren.ch/ultrasource/internal/agent"
	"parren.ch/ultrasource/pkg/googlesheet"
	"parren.ch/ultrasource/pkg/temperature"
	"parren.ch/ultrasource/pkg/ultrasource"
)

const defaultLogInterval = time.Hour

type flagMap map[string]string

func (m *flagMap) String() string {
	return fmt.Sprintf("%v", map[string]string(*m))
}

func (m *flagMap) Set(value string) error {
	kv := strings.Split(value, ":")
	(*m)[kv[0]] = kv[1]
	return nil
}

var (
	temperatureSensors flagMap = make(flagMap)
	enableCanBus               = true
	enableOnewireBus           = true
)

func main() {
	sheetCfg := googlesheet.Config{}
	flag.StringVar(&sheetCfg.CredentialsFile, "google-api-credentials-file", "google-api-credentials.json",
		"File downloaded when following https://developers.google.com/sheets/api/quickstart/go")
	flag.StringVar(&sheetCfg.SheetId, "google-sheet-id", "",
		"ID of the Google Sheet to use")
	flag.DurationVar(&sheetCfg.MaxHaveValueAge, "max-sheet-value-age", defaultLogInterval,
		"Interval between updates of the sheet")

	parserCfg := ultrasource.Config{}
	flag.BoolVar(&parserCfg.LogDetails, "print-parser-details", false,
		"Log details of Hoval message parsing to stdout")

	agentCfg := agent.Config{}
	flag.BoolVar(&agentCfg.UpdateCurrentSettings, "update-current-settings", true,
		"Enable updating current settings in sheet from CAN answers")
	flag.BoolVar(&agentCfg.ApplyDesiredSettings, "apply-desired-settings", true,
		"Enable applying desired setting as CAN commands")
	flag.BoolVar(&agentCfg.LogCurrentSettings, "log-current-settings", true,
		"Log current settings to sheet as table")
	flag.DurationVar(&agentCfg.SheetPollingInterval, "sheet-polling-interval", time.Minute,
		"Interval between polls of the sheet")
	flag.DurationVar(&agentCfg.SettingsQueryInterval, "settings-query-interval", defaultLogInterval,
		"Interval between batches of CAN queries of current settings")
	flag.DurationVar(&agentCfg.SettingsQueryGap, "settings-query-gap", time.Second,
		"Interval between individual CAN queries")
	flag.DurationVar(&agentCfg.SettingsLogInterval, "settings-log-interval", defaultLogInterval,
		"Interval between logging current settings to sheet")
	flag.DurationVar(&agentCfg.SettingsLogDelay, "settings-log-delay", time.Minute,
		"Delay of logging loop to query loop")
	flag.Var(&temperatureSensors, "temperature-sensor",
		"Temperature sensor in the format id:name")

	flag.BoolVar(&enableCanBus, "enable-can-bus", enableCanBus,
		"Enable CAN bus")
	flag.BoolVar(&enableOnewireBus, "enable-onewire-bus", enableOnewireBus,
		"Enable 1-wire bus")

	flag.Parse()
	if len(sheetCfg.SheetId) == 0 || len(sheetCfg.CredentialsFile) == 0 {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(1)
	}
	agentCfg.TemperatureSensors = temperatureSensors

	log.Printf("CAN bus: %v", enableCanBus)
	log.Printf("1-wire bus: %v", enableOnewireBus)
	log.Printf("1-wire sensors: %v", agentCfg.TemperatureSensors)

	ctx := context.Background()
	sheet := googlesheet.NewClient(ctx, googlesheet.NewServiceClient(ctx, sheetCfg), sheetCfg)
	var parser *ultrasource.Parser
	var can ultrasource.Client
	if enableCanBus {
		parser = ultrasource.NewParser(parserCfg)
		can = ultrasource.NewClient(ctx)
	}
	var sensors temperature.Client
	if enableOnewireBus {
		sensors = temperature.NewClient()
	}
	agent.RunForever(ctx, sheet, parser, can, sensors, agentCfg)
}
