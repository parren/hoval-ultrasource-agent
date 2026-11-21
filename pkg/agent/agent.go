package agent

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	gs "github.com/parren/hoval-ultrasource-agent/pkg/googlesheet"
	"github.com/parren/hoval-ultrasource-agent/pkg/logfiles"
	temp "github.com/parren/hoval-ultrasource-agent/pkg/temperature"
	us "github.com/parren/hoval-ultrasource-agent/pkg/ultrasource"
)

type Config struct {
	UpdateCurrentSettings      bool
	ApplyDesiredSettings       bool
	ApplyAutomaticSettings     bool
	LogCurrentSettingsToSheet  bool
	LogCurrentSettingsToFiles  bool
	CanPollingInterval         time.Duration
	SheetPollingInterval       time.Duration
	SettingsQueryInterval      time.Duration
	SettingsQueryGap           time.Duration
	SettingsLogToSheetInterval time.Duration
	SettingsLogToFilesInterval time.Duration
	SettingsLogDelay           time.Duration
	SheetFlushDelay            time.Duration
	TemperatureSensors         map[string]string
	LogStore                   logfiles.LogFileStore
	// OnReadings is an optional sink invoked on each file-logging tick with
	// ReportedSettings + sorted TemperatureSensors columns (without a leading
	// timestamp column). When nil, LogStore is used if LogStore.Dir != "".
	OnReadings func(now time.Time, header, row []interface{}) error
}

type sensorEntry struct {
	id   string
	name string
}

type settingAnswerMessage struct {
	msg us.Message
	set Setting
}

func RunForever(ctx context.Context, sheet gs.Client, parser *us.Parser, can us.Client, sensors temp.Client, cfg Config) {
	sortedSensors := sortSensors(cfg.TemperatureSensors)
	sensorNames := make([]string, len(sortedSensors))
	for i, s := range sortedSensors {
		sensorNames[i] = s.name
	}

	var answerMsgs <-chan settingAnswerMessage
	readerDone := make(chan struct{})
	if cfg.UpdateCurrentSettings && can != nil && cfg.CanPollingInterval > 0 {
		ch := make(chan settingAnswerMessage, 100)
		answerMsgs = ch
		go func() {
			defer close(readerDone)
			defer close(ch)
			receiveAnswerMessagesForever(ctx, can, parser, ch, cfg.CanPollingInterval)
		}()
		go func() {
			<-ctx.Done()
			_ = can.Close()
		}()
	} else {
		close(readerDone)
	}

	if cfg.UpdateCurrentSettings {
		log.Println("Updating current settings in sheet from messages")
	}
	if cfg.ApplyDesiredSettings {
		log.Println("Applying changed desired settings from sheet as messages")
	}

	r := &runner{
		sheet:         sheet,
		can:           can,
		sensors:       sensors,
		cfg:           cfg,
		sortedSensors: sortedSensors,
		sensorNames:   sensorNames,
		queryIdx:      -1,
	}
	r.loop(ctx, answerMsgs)
	<-readerDone
}

type runner struct {
	sheet              gs.Client
	can                us.Client
	sensors            temp.Client
	cfg                Config
	sortedSensors      []sensorEntry
	sensorNames        []string
	queryIdx           int
	sheetHeaderWritten bool
}

func (r *runner) loop(ctx context.Context, answerMsgs <-chan settingAnswerMessage) {
	var (
		queryC     <-chan time.Time
		queryStepC <-chan time.Time
		queryStepT *time.Timer

		flushC <-chan time.Time
		flushT *time.Timer

		sheetPollC <-chan time.Time
		sheetLogC  <-chan time.Time
		filesLogC  <-chan time.Time
	)

	stopTimer := func(t **time.Timer, c *<-chan time.Time) {
		if *t != nil {
			if !(*t).Stop() {
				select {
				case <-(*t).C:
				default:
				}
			}
			*t = nil
			*c = nil
		}
	}

	armFlush := func() {
		if r.cfg.SheetFlushDelay <= 0 {
			_ = r.sheet.Flush(ctx)
			return
		}
		stopTimer(&flushT, &flushC)
		flushT = time.NewTimer(r.cfg.SheetFlushDelay)
		flushC = flushT.C
	}

	startQueryBatch := func() {
		if r.can == nil {
			r.readTemperatureSensors(ctx)
			_ = r.sheet.Flush(ctx)
			return
		}
		log.Println("Querying current settings")
		if r.cfg.SettingsQueryGap <= 0 {
			for _, s := range ReportedSettings {
				r.sendQuery(ctx, s)
			}
			r.readTemperatureSensors(ctx)
			armFlush()
			return
		}
		r.queryIdx = 0
		r.sendQuery(ctx, ReportedSettings[0])
		r.queryIdx = 1
		stopTimer(&queryStepT, &queryStepC)
		queryStepT = time.NewTimer(r.cfg.SettingsQueryGap)
		queryStepC = queryStepT.C
	}

	if r.cfg.UpdateCurrentSettings && r.cfg.SettingsQueryInterval > 0 {
		t := time.NewTicker(r.cfg.SettingsQueryInterval)
		defer t.Stop()
		queryC = t.C
		startQueryBatch()
	}

	if r.cfg.ApplyDesiredSettings && r.cfg.SheetPollingInterval > 0 {
		t := time.NewTicker(r.cfg.SheetPollingInterval)
		defer t.Stop()
		sheetPollC = t.C
		r.pollDesiredSettings(ctx)
	}

	var sheetLogDelayT, filesLogDelayT *time.Timer
	var sheetLogTicker, filesLogTicker *time.Ticker
	if r.cfg.UpdateCurrentSettings && r.cfg.LogCurrentSettingsToSheet && r.cfg.SettingsLogToSheetInterval > 0 {
		sheetLogDelayT = time.NewTimer(r.cfg.SettingsLogDelay)
		defer sheetLogDelayT.Stop()
		sheetLogC = sheetLogDelayT.C
	}
	if r.cfg.UpdateCurrentSettings && r.cfg.LogCurrentSettingsToFiles && r.cfg.SettingsLogToFilesInterval > 0 {
		filesLogDelayT = time.NewTimer(r.cfg.SettingsLogDelay)
		defer filesLogDelayT.Stop()
		filesLogC = filesLogDelayT.C
	}

	defer func() {
		stopTimer(&queryStepT, &queryStepC)
		stopTimer(&flushT, &flushC)
		if sheetLogTicker != nil {
			sheetLogTicker.Stop()
		}
		if filesLogTicker != nil {
			filesLogTicker.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case m, ok := <-answerMsgs:
			if !ok {
				answerMsgs = nil
				continue
			}
			if r.handleAnswer(ctx, m) {
				armFlush()
			}

		case <-queryC:
			startQueryBatch()

		case <-queryStepC:
			queryStepT = nil
			queryStepC = nil
			if r.queryIdx >= 0 && r.queryIdx < len(ReportedSettings) {
				r.sendQuery(ctx, ReportedSettings[r.queryIdx])
				r.queryIdx++
				if r.queryIdx < len(ReportedSettings) {
					queryStepT = time.NewTimer(r.cfg.SettingsQueryGap)
					queryStepC = queryStepT.C
				} else {
					r.queryIdx = -1
					r.readTemperatureSensors(ctx)
					armFlush()
				}
			}

		case <-flushC:
			flushT = nil
			flushC = nil
			_ = r.sheet.Flush(ctx)

		case <-sheetPollC:
			stopTimer(&flushT, &flushC)
			_ = r.sheet.Flush(ctx)
			r.pollDesiredSettings(ctx)

		case <-sheetLogC:
			if sheetLogDelayT != nil {
				sheetLogDelayT = nil
				sheetLogTicker = time.NewTicker(r.cfg.SettingsLogToSheetInterval)
				sheetLogC = sheetLogTicker.C
			}
			r.logCurrentSettingsToSheet(ctx)

		case <-filesLogC:
			if filesLogDelayT != nil {
				filesLogDelayT = nil
				filesLogTicker = time.NewTicker(r.cfg.SettingsLogToFilesInterval)
				filesLogC = filesLogTicker.C
			}
			r.logCurrentSettingsToFiles()
		}
	}
}

func (r *runner) sendQuery(ctx context.Context, s Setting) {
	f, err := us.BuildFrame(us.IsQuery, s.valueId, nil)
	if err != nil {
		log.Printf("Failed to create query frame for %v: %v\n", s.valueId, err)
		return
	}
	log.Printf("Sending CAN frame %v\n", f)
	if err := r.can.TransmitFrame(ctx, f); err != nil {
		log.Printf("Failed to send frame: %v: %v\n", f, err)
	}
}

func (r *runner) readTemperatureSensors(ctx context.Context) {
	if r.sensors == nil || len(r.sortedSensors) == 0 {
		return
	}
	log.Println("Querying current sensor readings")
	for _, s := range r.sortedSensors {
		log.Printf("Reading sensor %v: %v\n", s.name, s.id)
		t, err := r.sensors.ReadTemp(s.id)
		if err != nil {
			log.Printf("Failed to read sensor %v: %v\n", s.id, err)
			continue
		}
		fv := gs.FacetValue{Setting: gs.Setting(s.name), Facet: gs.Have, Value: fmt.Sprintf("%v", t)}
		r.sheet.RefreshFluctuatingHaveValue(ctx, fv)
	}
}

func (r *runner) handleAnswer(ctx context.Context, m settingAnswerMessage) bool {
	v, ok := m.set.ParseMessage(m.msg)
	if !ok {
		return false
	}
	s := m.set.SheetSetting
	fv := gs.FacetValue{Setting: s, Facet: gs.Have, Value: v}
	if m.set.isStable {
		r.sheet.RefreshFacetValue(ctx, fv)
	} else {
		r.sheet.RefreshFluctuatingHaveValue(ctx, fv)
	}
	return true
}

func (r *runner) pollDesiredSettings(ctx context.Context) {
	keys := make([]gs.Setting, len(PushedSettings))
	for i, s := range PushedSettings {
		keys[i] = s.SheetSetting
	}
	values, err := r.sheet.ReadSettings(ctx, keys)
	if err != nil {
		return
	}
	if r.cfg.ApplyAutomaticSettings {
		r.applyAutomaticWaterTemperatureSetting(ctx, values)
	}
	r.applyDesiredSettings(ctx, values)
}

func (r *runner) applyDesiredSettings(ctx context.Context, values map[gs.Setting]gs.SettingValues) {
	log.Println("Polling for changed desired settings")
	for _, s := range PushedSettings {
		vs := values[s.SheetSetting]
		if vs.Want != vs.Have {
			if vs.Want == vs.Sent {
				log.Printf("Picking up value changed externally: %v\n", vs)
				_ = r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Want, Value: vs.Have})
				_ = r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Have})
			} else if vs.Sent != vs.Want+">" {
				_ = r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Want})
				if r.can != nil {
					r.applyDesiredSetting(ctx, s, vs)
				} else {
					log.Printf("CAN bus disabled. Ignoring changed desired setting %v\n", vs)
				}
			}
		} else if vs.Sent != vs.Have {
			_ = r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Have})
		}
	}
}

func (r *runner) applyDesiredSetting(ctx context.Context, s Setting, vs gs.SettingValues) {
	log.Printf("Applying desired setting %v\n", vs)
	f, err := s.MakeUpdateFrame(vs)
	if err != nil {
		log.Printf("Failed to create update frame for %v: %v\n", vs, err)
		return
	}
	r.sheet.InvalidateSettingValue(s.SheetSetting)

	log.Printf("Sending CAN frame %v\n", f)
	if err := r.can.TransmitFrame(ctx, f); err != nil {
		log.Printf("Failed to send frame: %v: %v\n", f, err)
	}
	_ = r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: s.SheetSetting, Facet: gs.Sent, Value: vs.Want + ">"})
}

const (
	triggerWaterTempCelsiusBoth = 60
	triggerWaterTempCelsiusOne  = 62
	reducedWaterTempCelsius     = "50"
)

func (r *runner) applyAutomaticWaterTemperatureSetting(ctx context.Context, values map[gs.Setting]gs.SettingValues) {
	log.Println("Polling for automatic water temperature adjustments")

	if isWaterTempAtLeast(triggerWaterTempCelsiusBoth, r.sheet) >= 2 {
		log.Printf("Both water temps have reached limit of %v\n", triggerWaterTempCelsiusBoth)
	} else if isWaterTempAtLeast(triggerWaterTempCelsiusOne, r.sheet) >= 1 {
		log.Printf("One water temp has reached limit of %v\n", triggerWaterTempCelsiusOne)
	} else {
		return
	}

	wp := values[gs.WaterProgram]
	if wp.Want != WaterProgramConstant {
		log.Printf("Water program is not set to constant, but %v\n", wp.Want)
		return
	}

	wt := values[gs.DesiredWaterTemp]
	celsius, err := strconv.ParseFloat(wt.Want, 32)
	if err != nil {
		log.Printf("Failed to parse desired water temp: %v\n", wt.Want)
	} else if celsius < triggerWaterTempCelsiusBoth {
		log.Printf("Water temp is already set to %v\n", wt.Want)
		return
	}

	log.Printf("Reducing water temp from %v to %v\n", wt.Want, reducedWaterTempCelsius)
	if err := r.sheet.WriteFacetValue(ctx, gs.FacetValue{Setting: gs.DesiredWaterTemp, Facet: gs.Want, Value: reducedWaterTempCelsius}); err == nil {
		wt.Want = reducedWaterTempCelsius
		values[gs.DesiredWaterTemp] = wt
	}
}

func isWaterTempAtLeast(desiredCelsius float64, sheet gs.Client) int {
	n := 0
	for _, s := range []gs.Setting{gs.ActualWaterTempHigher, gs.ActualWaterTempLower} {
		v, ok := sheet.LatestValue(s)
		if !ok {
			log.Printf("Failed to obtain water temp: %v\n", s)
			continue
		}
		celsius, err := strconv.ParseFloat(v, 32)
		if err != nil {
			log.Printf("Failed to parse water temp: %v: %v\n", s, v)
			continue
		}
		if celsius < desiredCelsius {
			continue
		}
		n++
	}
	return n
}

func (r *runner) logCurrentSettingsToSheet(ctx context.Context) {
	log.Println("Logging current settings to sheet")
	header := []interface{}{"Timestamp"}
	row := []interface{}{gs.FormatTimestamp(time.Now())}
	appendValuesToLogRow(r.sheet, r.sensorNames, &header, &row)
	if !r.sheetHeaderWritten {
		if err := r.sheet.Write(ctx, "Log!A1", [][]interface{}{header}); err == nil {
			r.sheetHeaderWritten = true
		}
	}
	_ = r.sheet.AppendOverwritingRows(ctx, "Log!A1", [][]interface{}{row})
}

func (r *runner) logCurrentSettingsToFiles() {
	ts := time.Now()
	if r.cfg.OnReadings != nil {
		log.Println("Logging current settings to sink")
		var header, row []interface{}
		appendValuesToLogRow(r.sheet, r.sensorNames, &header, &row)
		if err := r.cfg.OnReadings(ts, header, row); err != nil {
			log.Printf("Failed to log row: %v\n", err)
		}
		return
	}
	if r.cfg.LogStore.Dir == "" {
		return
	}
	log.Println("Logging current settings to file")
	header := []interface{}{"Timestamp"}
	row := []interface{}{logfiles.FormatTimestamp(ts)}
	appendValuesToLogRow(r.sheet, r.sensorNames, &header, &row)
	if err := r.cfg.LogStore.Write(ts, header, row); err != nil {
		log.Printf("Failed to log row: %v\n", err)
	}
}

func appendValuesToLogRow(sheet gs.Client, sensorNames []string, header, row *[]interface{}) {
	latest := sheet.LatestValues()
	for _, s := range ReportedSettings {
		*header = append(*header, s.SheetSetting)
		*row = append(*row, latest[s.SheetSetting])
	}
	for _, name := range sensorNames {
		*header = append(*header, name)
		*row = append(*row, latest[gs.Setting(name)])
	}
}

func sortSensors(sensors map[string]string) []sensorEntry {
	entries := make([]sensorEntry, 0, len(sensors))
	for id, name := range sensors {
		entries = append(entries, sensorEntry{id: id, name: name})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return entries[i].id < entries[j].id
	})
	return entries
}

func receiveAnswerMessagesForever(ctx context.Context, recv us.Client, parser *us.Parser,
	out chan<- settingAnswerMessage, retryInterval time.Duration,
) {
	first := true
	for {
		if ctx.Err() != nil {
			return
		}
		if !first {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryInterval):
			}
			if err := recv.Reconnect(ctx); err != nil {
				log.Printf("Failed to reconnect CAN bus: %v\n", err)
				continue
			}
		}
		first = false
		log.Println("Polling CAN frames")
		for recv.Receive() {
			f := recv.Frame()
			m, err := parser.ParseFrame(f)
			if err != nil {
				log.Printf("Parse error: %v for %v\n", err, f)
				continue
			}
			if m == nil || m.Type != us.IsAnswer || m.Id.Unknown() || m.Type.Unknown() {
				continue
			}
			for _, s := range ReportedSettings {
				if m.Id == s.valueId {
					select {
					case out <- settingAnswerMessage{msg: *m, set: s}:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}
}
