package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"go.einride.tech/can/pkg/socketcan"
	us "parren.ch/ultrasource/pkg/ultrasource"
)

const (
	indent = "\t\t\t\t"
)

var (
	queryInterval time.Duration
	sendGap       time.Duration

	valueIds = []us.ValueId{
		// us.HeatingProgramId,
		// us.WaterProgramId,
		// us.DesiredWaterTempId1,
		// us.DesiredWaterTempId2,
		// us.DesiredHeatingTempId,
		// us.ActualWaterTempBelowId,
		// us.ActualWaterTempAboveId,
		// us.ActualHeatingTempId,
		// us.ActualOutsideTempId,
		// us.ActualOutsideMinTempId,
		// us.ActualOutsideMaxTempId,
		// us.ActualOutsideAvgTempId,
		// us.DesiredRoomTempId,
		// us.DesiredHeatingTempId,
		// us.ActualHeatingTempId,
		// us.ActualHeaterTempId,

		us.DesiredHeaterTempId,
		us.ActualHeaterTempId,
		us.ActualHeaterReturnTempId,
		us.ActualModulationId,
		us.ActualHeaterHoursId,
		us.ActualHeaterEnergyId,
		us.ActualGridEnergyId,
	}
)

func main() {
	parserCfg := us.Config{}
	flag.BoolVar(&parserCfg.LogDetails, "print-parser-details", false,
		"Log details of Hoval message parsing to stdout")

	flag.DurationVar(&queryInterval, "query-interval", 10*time.Second, "Interval between CAN queries")
	flag.DurationVar(&sendGap, "can-send-gap", 500*time.Millisecond, "Interval between CAN queries")

	flag.Parse()

	ctx := context.Background()
	parser := us.NewParser(parserCfg)
	runForever(ctx, parser)
}

func runForever(ctx context.Context, parser *us.Parser) {
	conn, err := socketcan.DialContext(ctx, "can", "can0")
	if err != nil {
		log.Fatalf("Unable to dial can0: %v", err)
	}
	xmit := socketcan.NewTransmitter(conn)

	recv := socketcan.NewReceiver(conn)
	go queryCurrentSettingsForever(ctx, xmit)
	go receiveAnswerMessagesForever(recv, parser)
	blockForever()
}

func queryCurrentSettingsForever(ctx context.Context, xmit *socketcan.Transmitter) {
	for {
		fmt.Println(indent + "Querying current settings")
		for _, vid := range valueIds {
			f, err := us.BuildFrame(us.IsQuery, vid, nil)
			if err != nil {
				log.Fatalf("Failed to create query frame for %v: %v\n", vid, err)
				return
			}
			fmt.Printf("%sSending CAN frame %v\n", indent, f)
			err = xmit.TransmitFrame(ctx, f)
			if err != nil {
				fmt.Printf("Failed to send frame: %v: %v\n", f, err)
			}
			time.Sleep(sendGap)
		}
		time.Sleep(queryInterval)
	}
}

func receiveAnswerMessagesForever(recv *socketcan.Receiver, parser *us.Parser) {
	for recv.Receive() {
		f := recv.Frame()
		m, err := parser.ParseFrame(f)
		if err != nil {
			log.Printf("Parse error: %v for %v\n", err, f)
			continue
		}
		if m == nil {
			continue
		}
		if m.Type != us.IsAnswer {
			continue
		}
		// if m.Id.Unknown() || m.Type.Unknown() {
		// 	continue
		// }
		fmt.Printf("%v (%v)\n", *m, f)
	}
}

func blockForever() {
	<-(make(chan bool))
}
