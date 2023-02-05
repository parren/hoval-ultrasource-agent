package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"

	"go.einride.tech/can"
	"parren.ch/ultrasource/pkg/ultrasource"
)

var (
	logFile         string
	showKnownFrames bool = false
	showUnknown     bool = false
	cfg                  = ultrasource.Config{LogDetails: false}
)

func main() {
	flag.StringVar(&logFile, "log", "", "Candump log file")
	flag.BoolVar(&showKnownFrames, "known-frames", false, "show known frames")
	flag.BoolVar(&showUnknown, "unknown", false, "show unknown things")
	flag.BoolVar(&cfg.LogDetails, "details", false, "show details")
	flag.Parse()
	if len(logFile) == 0 {
		fmt.Println("Usage:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// (1670677101.571858) can0 1F400FFF#19BB70A100015208
	framePat := regexp.MustCompile(`^\(.*\) can0 (.*)$`)

	p := ultrasource.NewParser(cfg)

	f, err := os.Open(logFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	idsSeen := make(map[ultrasource.ValueId]int)
	typesSeen := make(map[ultrasource.MessageType]int)

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		match := framePat.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		frameStr := match[1]
		if cfg.LogDetails {
			fmt.Printf("\t\t\t%v\n", frameStr)
		}
		frame := can.Frame{}
		err = frame.UnmarshalString(frameStr)
		if err != nil {
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
		if !showUnknown {
			if msg.Type.Unknown() || msg.Id.Unknown() {
				continue
			}
		}
		frameSuffix := ""
		if showKnownFrames {
			frameSuffix = fmt.Sprintf(" (CAN: %v)", frame)
		}
		if msg.Type == ultrasource.IsQuery {
			fmt.Printf("\t\t%v%v\n", *msg, frameSuffix)
		} else if msg.Type == ultrasource.IsAnswer {
			fmt.Printf("\t%v%v\n", *msg, frameSuffix)
		} else {
			fmt.Printf("%v%v\n", *msg, frameSuffix)
		}
		typesSeen[msg.Type] = typesSeen[msg.Type] + 1
		idsSeen[msg.Id] = idsSeen[msg.Id] + 1
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
