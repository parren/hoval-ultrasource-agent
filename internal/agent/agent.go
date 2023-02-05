package agent

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	gs "parren.ch/ultrasource/pkg/googlesheet"
	temp "parren.ch/ultrasource/pkg/temperature"
	us "parren.ch/ultrasource/pkg/ultrasource"
)

type Config struct {
	UpdateCurrentSettings  bool
	ApplyDesiredSettings   bool
	ApplyAutomaticSettings bool
	LogCurrentSettings     bool
	CanPollingInterval     time.Duration
	SheetPollingInterval   time.Duration
	SettingsQueryInterval  time.Duration
	SettingsQueryGap       time.Duration
	SettingsLogInterval    time.Duration
	SettingsLogDelay       time.Duration
	TemperatureSensors     map[string]string
}

func RunForever(ctx context.Context, sheet gs.Client, parser *us.Parser, can us.Client, sensors temp.Client, cfg Config) {
	answerMsgs := make(chan settingAnswerMessage, 100)
	defer close(answerMsgs)
	if cfg.UpdateCurrentSettings {
		log.Println("Updating current settings in sheet from messages")
		if cfg.SettingsQueryInterval > 0 {
			go queryCurrentSettingsForever(ctx, can, sensors, cfg)
		}
		if cfg.CanPollingInterval > 0 {
			go receiveAnswerMessagesForever(ctx, can, parser, answerMsgs, cfg)
		}
		go updateCurrentSettingsForever(ctx, answerMsgs, sheet, cfg)
		if sensors != nil {
			go updateSensorReadingsForever(ctx, sensors.TemperatureReadings(), sheet, cfg)
		}
		if cfg.LogCurrentSettings {
			go logCurrentSettingsForever(ctx, sheet, cfg)
		}
	}
	if cfg.ApplyDesiredSettings {
		log.Println("Applying changed desired settings from sheet as messages")
		go updateDesiredSettingsForever(ctx, sheet, can, cfg)
	}
	<-ctx.Done()
}

type settingAnswerMessage struct {
	msg us.Message
	set Setting
}

func queryCurrentSettingsForever(ctx context.Context, xmit us.Transmitter, sensors temp.Client, cfg Config) {
	runThenTick(ctx, cfg.SettingsQueryInterval, func() {
		if xmit != nil {
			log.Println("Querying current settings")
			for _, s := range ReportedSettings {
				f, err := us.BuildFrame(us.IsQuery, s.valueId, nil)
				if err != nil {
					log.Fatalf("Failed to create query frame for %v: %v\n", s.valueId, err)
					return
				}
				log.Printf("Sending CAN frame %v\n", f)
				err = xmit.TransmitFrame(ctx, f)
				if err != nil {
					log.Printf("Failed to send frame: %v: %v\n", f, err)
				}
				time.Sleep(cfg.SettingsQueryGap)
			}
		}
		if sensors != nil {
			log.Println("Querying current sensor readings")
			for id, name := range cfg.TemperatureSensors {
				log.Printf("Reading sensor %v: %v\n", name, id)
				sensors.RequestTemp(id)
			}
		}
	})
}

func receiveAnswerMessagesForever(ctx context.Context, recv us.Receiver, parser *us.Parser,
	out chan<- settingAnswerMessage, cfg Config,
) {
	runThenTick(ctx, cfg.CanPollingInterval, func() {
		log.Println("Polling CAN frames")
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
			if m.Id.Unknown() || m.Type.Unknown() {
				continue
			}
			for _, s := range ReportedSettings {
				if m.Id == s.valueId {
					out <- settingAnswerMessage{msg: *m, set: s}
				}
			}
		}
	})
}

func updateCurrentSettingsForever(ctx context.Context, msgs <-chan settingAnswerMessage, sheet gs.Client, cfg Config) {
	for m := range msgs {
		if v, ok := m.set.ParseMessage(m.msg); ok {
			s := m.set.SheetSetting
			fv := gs.FacetValue{Setting: s, Facet: gs.Have, Value: v}
			if m.set.isStable {
				sheet.RefreshFacetValue(ctx, fv)
			} else {
				sheet.RefreshFluctuatingHaveValue(ctx, fv)
			}
			if m.set.isDesired {
				vs := sheet.ReadSettingValues(ctx, s)
				if vs.Want == vs.Have && vs.Sent != vs.Have {
					sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s, Facet: gs.Sent, Value: vs.Have})
				}
			}
		}
	}
}

func updateSensorReadingsForever(ctx context.Context, readings <-chan temp.TemperatureReading, sheet gs.Client, cfg Config) {
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-readings:
			if r.Error != nil {
				log.Printf("Failed to read sensor %v: %v\n", r.Id, r.Error)
				continue
			}
			name := cfg.TemperatureSensors[r.Id]
			fv := gs.FacetValue{Setting: gs.Setting(name), Facet: gs.Have, Value: fmt.Sprintf("%v", r.Temperature)}
			sheet.RefreshFluctuatingHaveValue(ctx, fv)
		}
	}
}

func updateDesiredSettingsForever(ctx context.Context, sheet gs.Client, xmit us.Transmitter, cfg Config) {
	runThenTick(ctx, cfg.SheetPollingInterval, func() {
		if cfg.ApplyAutomaticSettings {
			applyAutomaticWaterTemperatureSetting(ctx, sheet, xmit, cfg)
		}
		applyDesiredSettings(ctx, sheet, xmit, cfg)
	})
}

func applyDesiredSettings(ctx context.Context, sheet gs.Client, xmit us.Transmitter, cfg Config) {
	log.Println("Polling for changed desired settings")
	for _, s := range PushedSettings {
		vs := sheet.ReadSettingValues(ctx, s.SheetSetting)
		if vs.Want != vs.Have {
			if vs.Sent != vs.Want && vs.Sent != vs.Want+">" {
				sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Want})
				if xmit != nil {
					applyDesiredSetting(ctx, s, vs, xmit, sheet)
				} else {
					log.Printf("CAN bus disabled. Ignoring changed desired setting %v\n", vs)
				}
			}
		} else if vs.Sent != vs.Have {
			sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Have})
		}
	}
}

func applyDesiredSetting(ctx context.Context, s Setting, vs gs.SettingValues, xmit us.Transmitter, sheet gs.Client) {
	log.Printf("Applying desired setting %v\n", vs)
	f, err := s.MakeUpdateFrame(vs)
	if err != nil {
		log.Printf("Failed to create update frame for %v: %v\n", vs, err)
		return
	}
	sheet.InvalidateSettingValue(s.SheetSetting)

	log.Printf("Sending CAN frame %v\n", f)
	err = xmit.TransmitFrame(ctx, f)
	if err != nil {
		log.Printf("Failed to send frame: %v: %v\n", f, err)
	}
	sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Want + ">"})
}

const (
	triggerWaterTempCelsius = 60
	reducedWaterTempCelsius = "50"
)

func applyAutomaticWaterTemperatureSetting(ctx context.Context, sheet gs.Client, xmit us.Transmitter, cfg Config) {
	log.Println("Polling for automatic water temperature adjustments")

	if !isWaterTempAtLeast(triggerWaterTempCelsius, sheet) {
		return
	}
	log.Printf("Water temp has reached limit of %v\n", triggerWaterTempCelsius)

	vs := sheet.ReadSettingValues(ctx, gs.WaterProgram)
	if vs.Want != WaterProgramConstant {
		log.Printf("Water program is not set to constant, but %v\n", vs.Want)
		return
	}

	vs = sheet.ReadSettingValues(ctx, gs.DesiredWaterTemp)
	celsius, err := strconv.ParseFloat(vs.Want, 32)
	if err != nil {
		log.Printf("Failed to parse desired water temp: %v\n", vs.Want)
	} else if celsius < triggerWaterTempCelsius {
		log.Printf("Water temp is already set to %v\n", vs.Want)
		return
	}

	log.Printf("Reducing water temp from %v to %v\n", vs.Want, reducedWaterTempCelsius)
	sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: gs.DesiredWaterTemp, Facet: gs.Want, Value: reducedWaterTempCelsius})
}

func isWaterTempAtLeast(desiredCelsius float64, sheet gs.Client) bool {
	for _, s := range []gs.Setting{gs.ActualWaterTempHigher, gs.ActualWaterTempLower} {
		v, ok := sheet.LatestValues()[s]
		if !ok {
			return false
		}
		celsius, err := strconv.ParseFloat(v, 32)
		if err != nil {
			log.Printf("Failed to parse water temp: %v: %v\n", s, v)
			return false
		}
		if celsius < desiredCelsius {
			return false
		}
	}
	return true
}

func logCurrentSettingsForever(ctx context.Context, sheet gs.Client, cfg Config) {
	time.Sleep(cfg.SettingsLogDelay)
	runThenTick(ctx, cfg.SettingsLogInterval, func() {
		log.Println("Logging current settings")

		header := []interface{}{"Timestamp"}
		row := []interface{}{gs.FormatTimestamp(time.Now())}
		for _, s := range ReportedSettings {
			header = append(header, s.SheetSetting)
			v := sheet.LatestValues()[s.SheetSetting]
			row = append(row, v)
		}
		for _, name := range cfg.TemperatureSensors {
			s := gs.Setting(name)
			header = append(header, s)
			v := sheet.LatestValues()[s]
			row = append(row, v)
		}
		sheet.Write(ctx, "Log!A1", [][]interface{}{header})
		sheet.AppendOverwritingRows(ctx, "Log!A1", [][]interface{}{row})
	})
}

func runThenTick(ctx context.Context, interval time.Duration, body func()) {
	runner := make(chan struct{}, 1)
	runner <- struct{}{}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-runner:
			body()
		case <-ticker.C:
			body()
		}
	}
}
