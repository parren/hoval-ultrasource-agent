package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"go.einride.tech/can"

	"github.com/parren/hoval-ultrasource-agent/pkg/agent"
	"github.com/parren/hoval-ultrasource-agent/pkg/googlesheet"
	"github.com/parren/hoval-ultrasource-agent/pkg/temperature"
	us "github.com/parren/hoval-ultrasource-agent/pkg/ultrasource"
)

const defaultLogInterval = time.Hour

type flagMap map[string]string

func (m *flagMap) String() string {
	return fmt.Sprintf("%v", map[string]string(*m))
}

func (m *flagMap) Set(value string) error {
	k, v, ok := strings.Cut(value, ":")
	if !ok || k == "" || v == "" {
		return fmt.Errorf("expected id:name format, got %q", value)
	}
	(*m)[k] = v
	return nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	sub := os.Args[1]
	args := os.Args[2:]
	switch sub {
	case "agent":
		runAgent(args)
	case "monitor":
		runMonitor(args)
	case "analyze":
		runAnalyze(args)
	case "query":
		runQuery(args)
	case "set":
		runSet(args)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand %q\n\n", sub)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: ultrasource <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  agent     Run the Google Sheets + CAN + 1-wire agent")
	fmt.Println("  monitor   Query and log live CAN bus messages")
	fmt.Println("  analyze   Parse offline candump log files")
	fmt.Println("  query     Send a single CAN query for a named setting")
	fmt.Println("  set       Send a single CAN set frame for a named setting")
}

func runAgent(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)

	temperatureSensors := make(flagMap)
	enableCanBus := true
	enableOnewireBus := true
	canIface := "can0"

	sheetCfg := googlesheet.Config{}
	fs.StringVar(&sheetCfg.CredentialsFile, "google-api-credentials-file", "google-api-credentials.json",
		"File downloaded when following https://developers.google.com/sheets/api/quickstart/go")
	fs.StringVar(&sheetCfg.SheetId, "google-sheet-id", "",
		"ID of the Google Sheet to use")
	fs.DurationVar(&sheetCfg.MaxHaveValueAge, "max-sheet-value-age", defaultLogInterval,
		"Interval between updates of the sheet")

	parserCfg := us.Config{}
	fs.BoolVar(&parserCfg.LogDetails, "print-parser-details", false,
		"Log details of Hoval message parsing to stdout")

	agentCfg := agent.Config{SheetFlushDelay: 2 * time.Second}
	fs.BoolVar(&agentCfg.UpdateCurrentSettings, "update-current-settings", true,
		"Enable updating current settings in sheet from CAN answers")
	fs.DurationVar(&agentCfg.CanPollingInterval, "can-bus-polling-interval", time.Minute,
		"Interval between CAN bus reconnect attempts")
	fs.BoolVar(&agentCfg.ApplyDesiredSettings, "apply-desired-settings", false,
		"Enable applying desired setting as CAN commands")
	fs.BoolVar(&agentCfg.ApplyAutomaticSettings, "apply-automatic-settings", false,
		"Enable automatic settings as sheet updates")
	fs.DurationVar(&agentCfg.SheetPollingInterval, "sheet-polling-interval", time.Minute,
		"Interval between polls of the sheet")
	fs.DurationVar(&agentCfg.SettingsQueryInterval, "settings-query-interval", defaultLogInterval,
		"Interval between batches of CAN queries of current settings")
	fs.DurationVar(&agentCfg.SettingsQueryGap, "settings-query-gap", time.Second,
		"Interval between individual CAN queries")
	fs.BoolVar(&agentCfg.LogCurrentSettingsToSheet, "log-to-sheet", false,
		"Log current settings to sheet as table")
	fs.DurationVar(&agentCfg.SettingsLogToSheetInterval, "log-to-sheet-interval", defaultLogInterval,
		"Interval between logging current settings to sheet")
	fs.BoolVar(&agentCfg.LogCurrentSettingsToFiles, "log-to-files", false,
		"Log current settings to daily CSV files")
	fs.DurationVar(&agentCfg.SettingsLogToFilesInterval, "log-to-files-interval", defaultLogInterval,
		"Interval between logging current settings to files")
	fs.StringVar(&agentCfg.LogStore.Dir, "log-to-files-dir", "",
		"Base dir of CSV settings log files")
	fs.DurationVar(&agentCfg.SettingsLogDelay, "log-delay", time.Minute,
		"Delay of logging loop to query loop")
	fs.DurationVar(&agentCfg.SheetFlushDelay, "sheet-flush-delay", 2*time.Second,
		"Batching window for flushing sheet updates")
	fs.Var(&temperatureSensors, "temperature-sensor",
		"Temperature sensor in the format id:name")

	fs.BoolVar(&enableCanBus, "enable-can-bus", enableCanBus, "Enable CAN bus")
	fs.StringVar(&canIface, "can-interface", canIface, "SocketCAN interface name")
	fs.BoolVar(&enableOnewireBus, "enable-onewire-bus", enableOnewireBus, "Enable 1-wire bus")

	_ = fs.Parse(args)
	if len(sheetCfg.SheetId) == 0 || len(sheetCfg.CredentialsFile) == 0 {
		fs.Usage()
		os.Exit(1)
	}
	agentCfg.TemperatureSensors = temperatureSensors

	log.Printf("CAN bus: %v (%s)", enableCanBus, canIface)
	log.Printf("1-wire bus: %v", enableOnewireBus)
	log.Printf("1-wire sensors: %v", agentCfg.TemperatureSensors)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sheetSrv, err := googlesheet.NewServiceClient(ctx, sheetCfg)
	if err != nil {
		log.Fatalf("Unable to retrieve Google Sheets client: %v", err)
	}
	sheet := googlesheet.NewClient(ctx, sheetSrv, sheetCfg)

	var parser *us.Parser
	var canClient us.Client
	if enableCanBus {
		parser = us.NewParser(parserCfg)
		canClient, err = us.NewInterfaceClient(ctx, canIface)
		if err != nil {
			log.Fatalf("Unable to dial %s: %v", canIface, err)
		}
		defer canClient.Close()
	}
	var sensors temperature.Client
	if enableOnewireBus {
		sensors, err = temperature.NewClient()
		if err != nil {
			log.Fatalf("Unable to init 1-wire temperature client: %v", err)
		}
	}

	agent.RunForever(ctx, sheet, parser, canClient, sensors, agentCfg)
}

func runMonitor(args []string) {
	fs := flag.NewFlagSet("monitor", flag.ExitOnError)
	parserCfg := us.Config{}
	canIface := "can0"
	var queryInterval, sendGap time.Duration
	fs.BoolVar(&parserCfg.LogDetails, "print-parser-details", false,
		"Log details of Hoval message parsing to stdout")
	fs.StringVar(&canIface, "can-interface", canIface, "SocketCAN interface name")
	fs.DurationVar(&queryInterval, "query-interval", 10*time.Second, "Interval between CAN query batches")
	fs.DurationVar(&sendGap, "can-send-gap", 500*time.Millisecond, "Gap between individual CAN queries")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	canClient, err := us.NewInterfaceClient(ctx, canIface)
	if err != nil {
		log.Fatalf("Unable to dial %s: %v", canIface, err)
	}
	defer canClient.Close()
	go func() {
		<-ctx.Done()
		_ = canClient.Close()
	}()

	parser := us.NewParser(parserCfg)
	go func() {
		for {
			for _, s := range agent.ReportedSettings {
				if ctx.Err() != nil {
					return
				}
				f, err := us.BuildFrame(us.IsQuery, s.ValueId(), nil)
				if err != nil {
					log.Printf("Failed to build frame for %s: %v", s.SheetSetting, err)
					continue
				}
				if err := canClient.TransmitFrame(ctx, f); err != nil {
					log.Printf("Failed to send frame %v: %v", f, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(sendGap):
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(queryInterval):
			}
		}
	}()

	for canClient.Receive() {
		f := canClient.Frame()
		m, err := parser.ParseFrame(f)
		if err != nil {
			log.Printf("Parse error: %v for %v", err, f)
			continue
		}
		if m == nil || m.Type != us.IsAnswer {
			continue
		}
		fmt.Printf("%v (%v)\n", *m, f)
	}
}

func runAnalyze(args []string) {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	var logFile string
	var showKnownFrames, showUnknown bool
	cfg := us.Config{}
	fs.StringVar(&logFile, "log", "", "Candump log file")
	fs.BoolVar(&showKnownFrames, "known-frames", false, "show known frames")
	fs.BoolVar(&showUnknown, "unknown", false, "show unknown things")
	fs.BoolVar(&cfg.LogDetails, "details", false, "show details")
	_ = fs.Parse(args)
	if len(logFile) == 0 {
		fs.Usage()
		os.Exit(1)
	}

	framePat := regexp.MustCompile(`^\(.*\) can0 (.*)$`)
	p := us.NewParser(cfg)

	f, err := os.Open(logFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	idsSeen := make(map[us.ValueId]int)
	typesSeen := make(map[us.MessageType]int)

	s := bufio.NewScanner(f)
	for s.Scan() {
		match := framePat.FindStringSubmatch(s.Text())
		if match == nil {
			continue
		}
		frameStr := match[1]
		if cfg.LogDetails {
			fmt.Printf("\t\t\t%v\n", frameStr)
		}
		frame := can.Frame{}
		if err := frame.UnmarshalString(frameStr); err != nil {
			fmt.Printf("\tFailed to parse %v: %v\n", frameStr, err)
			continue
		}
		msg, err := p.ParseFrame(frame)
		if err != nil {
			fmt.Printf("\tFailed to parse %v: %v\n", frameStr, err)
			continue
		}
		if msg == nil {
			continue
		}
		if !showUnknown && (msg.Type.Unknown() || msg.Id.Unknown()) {
			continue
		}
		frameSuffix := ""
		if showKnownFrames {
			frameSuffix = fmt.Sprintf(" (CAN: %v)", frame)
		}
		switch msg.Type {
		case us.IsQuery:
			fmt.Printf("\t\t%v%v\n", *msg, frameSuffix)
		case us.IsAnswer:
			fmt.Printf("\t%v%v\n", *msg, frameSuffix)
		default:
			fmt.Printf("%v%v\n", *msg, frameSuffix)
		}
		typesSeen[msg.Type]++
		idsSeen[msg.Id]++
	}

	fmt.Println("Types seen:")
	for id, n := range typesSeen {
		fmt.Printf("  %v: %v\n", id, n)
	}
	fmt.Println("IDs seen:")
	for id, n := range idsSeen {
		fmt.Printf("  %v: %v\n", id, n)
	}
}

func runQuery(args []string) {
	fs := flag.NewFlagSet("query", flag.ExitOnError)
	canIface := "can0"
	timeout := 5 * time.Second
	fs.StringVar(&canIface, "can-interface", canIface, "SocketCAN interface name")
	fs.DurationVar(&timeout, "timeout", timeout, "Wait timeout for CAN answer")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: ultrasource query [--can-interface can0] <setting>")
		for _, s := range agent.ReportedSettings {
			fmt.Fprintf(os.Stderr, "  %s\n", s.SheetSetting)
		}
		os.Exit(1)
	}
	setting, ok := agent.SettingByName(fs.Arg(0))
	if !ok {
		log.Fatalf("Unknown setting %q", fs.Arg(0))
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	canClient, err := us.NewInterfaceClient(ctx, canIface)
	if err != nil {
		log.Fatalf("Unable to dial %s: %v", canIface, err)
	}
	defer canClient.Close()
	go func() {
		<-ctx.Done()
		_ = canClient.Close()
	}()

	f, err := us.BuildFrame(us.IsQuery, setting.ValueId(), nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := canClient.TransmitFrame(ctx, f); err != nil {
		log.Fatal(err)
	}

	parser := us.NewParser(us.Config{})
	for canClient.Receive() {
		m, err := parser.ParseFrame(canClient.Frame())
		if err != nil || m == nil || m.Type != us.IsAnswer || m.Id != setting.ValueId() {
			continue
		}
		if v, ok := setting.ParseMessage(*m); ok {
			fmt.Printf("%s = %s (%v)\n", setting.SheetSetting, v, *m)
			return
		}
	}
	log.Fatalf("Timed out waiting for answer to %s", setting.SheetSetting)
}

func runSet(args []string) {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	canIface := "can0"
	fs.StringVar(&canIface, "can-interface", canIface, "SocketCAN interface name")
	_ = fs.Parse(args)
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: ultrasource set [--can-interface can0] <setting> <value>")
		for _, s := range agent.PushedSettings {
			fmt.Fprintf(os.Stderr, "  %s\n", s.SheetSetting)
		}
		os.Exit(1)
	}
	setting, ok := agent.PushedSettingByName(fs.Arg(0))
	if !ok {
		log.Fatalf("Unknown settable setting %q", fs.Arg(0))
	}

	f, err := setting.MakeUpdateFrame(googlesheet.SettingValues{
		Setting: setting.SheetSetting,
		Want:    fs.Arg(1),
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	canClient, err := us.NewInterfaceClient(ctx, canIface)
	if err != nil {
		log.Fatalf("Unable to dial %s: %v", canIface, err)
	}
	defer canClient.Close()

	if err := canClient.TransmitFrame(ctx, f); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Sent %s = %s (%v)\n", setting.SheetSetting, fs.Arg(1), f)
}
