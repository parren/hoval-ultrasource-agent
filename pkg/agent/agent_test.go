package agent

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"
	"testing"
	"time"

	"go.einride.tech/can"
	gs "github.com/parren/hoval-ultrasource-agent/pkg/googlesheet"
	us "github.com/parren/hoval-ultrasource-agent/pkg/ultrasource"
)

const tick = time.Millisecond
const step = tick * 3
const timeout = time.Second * 4

func TestSetNewValue(t *testing.T) {
	for _, tt := range []struct {
		setting string
		valueId us.ValueId
		init    fakeRow
		update  string
		sent    interface{}
		pending fakeRow
		final   fakeRow
	}{
		{
			setting: "heating_program",
			valueId: us.HeatingProgramId,
			init:    fakeRow{"konstant", "konstant", "konstant", ""},
			update:  "standby",
			sent:    "Standby",
			pending: fakeRow{"standby", "standby>", "konstant"},
			final:   fakeRow{"standby", "standby", "standby"},
		},
		{
			setting: "water_program",
			valueId: us.WaterProgramId,
			init:    fakeRow{"konstant", "konstant", "konstant", ""},
			update:  "standby",
			sent:    "Standby",
			pending: fakeRow{"standby", "standby>", "konstant"},
			final:   fakeRow{"standby", "standby", "standby"},
		},
		{
			setting: "room_temp",
			valueId: us.DesiredConstantRoomTempId,
			init:    fakeRow{"10", "10", "10", ""},
			update:  "45",
			sent:    float32(45),
			pending: fakeRow{"45", "45>", "10"},
			final:   fakeRow{"45", "45", "45"},
		},
		{
			setting: "water_temp",
			valueId: us.DesiredConstantWaterTempId,
			init:    fakeRow{"10", "10", "10", ""},
			update:  "45",
			sent:    float32(45),
			pending: fakeRow{"45", "45>", "10"},
			final:   fakeRow{"45", "45", "45"},
		},
	} {
		t.Run(tt.setting, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			defer time.Sleep(tick)

			parser, can := initCan()
			sheetClient, sheet := initSheet(ctx)
			sheet.rows[tt.setting] = tt.init

			agentCfg := Config{
				UpdateCurrentSettings: true,
				ApplyDesiredSettings:  true,
				CanPollingInterval:    tick,
				SheetPollingInterval:  tick,
			}
			go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)

			time.Sleep(step)
			if err := sheet.checkRowStart(tt.setting, tt.init); err != nil {
				t.Fatal(err)
			}
			sheet.simulateUser(tt.setting, tt.update)

			time.Sleep(step)
			if err := sheet.checkRowStart(tt.setting, tt.pending); err != nil {
				t.Fatal(err)
			}
			if err := can.checkXmit(us.IsSet, tt.valueId, tt.sent); err != nil {
				t.Fatal(err)
			}
			can.clearXmit()
			can.simulateFrame(mustBuildFrame(t, us.IsAnswer, tt.valueId, tt.sent))

			time.Sleep(step)
			if err := sheet.checkRowStart(tt.setting, tt.final); err != nil {
				t.Fatal(err)
			}
			if err := can.checkNotXmit(us.IsSet, tt.valueId, tt.sent); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQuerySettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["actual_water_temp"] = fakeRow{"", "", "", ""}

	const queryInterval = step * 5

	agentCfg := Config{
		UpdateCurrentSettings: true,
		SettingsQueryInterval: queryInterval,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)
	start := time.Now()

	time.Sleep(step)
	if err := can.checkXmit(us.IsQuery, us.ActualWaterTempHigherId, nil); err != nil {
		t.Fatal(err)
	}
	can.clearXmit()

	time.Sleep(queryInterval - time.Since(start) - step)
	if err := can.checkNotXmit(us.IsQuery, us.ActualWaterTempHigherId, nil); err != nil {
		t.Fatal(err)
	}

	time.Sleep(queryInterval - time.Since(start) + step)
	if err := can.checkXmit(us.IsQuery, us.ActualWaterTempHigherId, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAndLogValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["actual_water_temp"] = fakeRow{"", "", "", ""}
	sheet.rows["actual_water_temp_lower"] = fakeRow{"", "", "", ""}

	const logDelay = step * 10
	const logInterval = step * 15

	agentCfg := Config{
		UpdateCurrentSettings:      true,
		LogCurrentSettingsToSheet:  true,
		CanPollingInterval:         tick,
		SettingsLogToSheetInterval: logInterval,
		SettingsLogDelay:           logDelay,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)
	start := time.Now()

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", ""); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", ""); err != nil {
		t.Fatal(err)
	}
	if sheet.logCount() > 0 {
		t.Fatalf("expected no logs, but got: %v", sheet.logCount())
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(12.34)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "12.3"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", ""); err != nil {
		t.Fatal(err)
	}
	if sheet.logCount() > 0 {
		t.Fatalf("expected no logs yet, but got: %v", sheet.logCount())
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(34.56)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "12.3"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "34.6"); err != nil {
		t.Fatal(err)
	}
	if sheet.logCount() > 0 {
		t.Fatalf("expected no logs yet, but got: %v", sheet.logCount())
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(23.45)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "23.5"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "34.6"); err != nil {
		t.Fatal(err)
	}
	if sheet.logCount() > 0 {
		t.Fatalf("expected no logs yet, but got: %v", sheet.logCount())
	}

	time.Sleep(logDelay - time.Since(start) - step)
	if sheet.logCount() > 0 {
		t.Fatalf("expected no logs yet, but got: %v", sheet.logCount())
	}

	time.Sleep(logDelay - time.Since(start) + step)
	if sheet.logCount() != 1 {
		t.Fatalf("expected 1 log, but got: %v", sheet.logCount())
	}
	if err := sheet.checkLastLog(actualWaterTempHigherIdx, "23.5"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkLastLog(actualWaterTempLowerIdx, "34.6"); err != nil {
		t.Fatal(err)
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(40)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "40"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "34.6"); err != nil {
		t.Fatal(err)
	}
	if sheet.logCount() != 1 {
		t.Fatalf("expected 1 log, but got: %v", sheet.logCount())
	}

	time.Sleep(logDelay + logInterval - time.Since(start) - step)
	if sheet.logCount() != 1 {
		t.Fatalf("expected 1 logs, but got: %v", sheet.logCount())
	}

	time.Sleep(logDelay + logInterval - time.Since(start) + step)
	if sheet.logCount() != 2 {
		t.Fatalf("expected 2 logs, but got: %v", sheet.logCount())
	}
	if err := sheet.checkLastLog(actualWaterTempHigherIdx, "40"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkLastLog(actualWaterTempLowerIdx, "34.6"); err != nil {
		t.Fatal(err)
	}
}

func TestAutoResetLegionellaTemp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["water_program"] = fakeRow{"konstant", "konstant", "konstant", ""}
	sheet.rows["water_temp"] = fakeRow{"60", "60", "60", ""}

	agentCfg := Config{
		UpdateCurrentSettings:  true,
		ApplyDesiredSettings:   true,
		ApplyAutomaticSettings: true,
		CanPollingInterval:     tick,
		SheetPollingInterval:   tick,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)

	time.Sleep(step)
	if err := sheet.checkRowStart("water_temp", fakeRow{"60", "60", "60"}); err != nil {
		t.Fatal(err)
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(59.9)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(59.8)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "59.9"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "59.8"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"60", "60", "60"}); err != nil {
		t.Fatal(err)
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(60.0)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(59.9)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "59.9"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"60", "60", "60"}); err != nil {
		t.Fatal(err)
	}
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(60.0)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(60.0)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"50", "50>", "60"}); err != nil {
		t.Fatal(err)
	}
	if err := can.checkXmit(us.IsSet, us.DesiredConstantWaterTempId, float32(50)); err != nil {
		t.Fatal(err)
	}
}

func TestAutoResetLegionellaTemp_ifOnlyOneHigher(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["water_program"] = fakeRow{"konstant", "konstant", "konstant", ""}
	sheet.rows["water_temp"] = fakeRow{"60", "60", "60", ""}

	agentCfg := Config{
		UpdateCurrentSettings:  true,
		ApplyDesiredSettings:   true,
		ApplyAutomaticSettings: true,
		CanPollingInterval:     tick,
		SheetPollingInterval:   tick,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)

	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(59.0)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(62.0)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "59"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "62"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"50", "50>", "60"}); err != nil {
		t.Fatal(err)
	}
	if err := can.checkXmit(us.IsSet, us.DesiredConstantWaterTempId, float32(50)); err != nil {
		t.Fatal(err)
	}
}

func TestAutoResetLegionellaTemp_ifWrongProgram(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["water_program"] = fakeRow{"standby", "standby", "standby", ""}
	sheet.rows["water_temp"] = fakeRow{"60", "60", "60", ""}

	agentCfg := Config{
		UpdateCurrentSettings:  true,
		ApplyDesiredSettings:   true,
		ApplyAutomaticSettings: true,
		CanPollingInterval:     tick,
		SheetPollingInterval:   tick,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)

	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(60.0)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(60.0)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"60", "60", "60"}); err != nil {
		t.Fatal(err)
	}
	if err := can.checkNotXmit(us.IsSet, us.DesiredConstantWaterTempId, float32(50)); err != nil {
		t.Fatal(err)
	}
}

func TestAutoResetLegionellaTemp_ifAlreadyLower(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer time.Sleep(tick)

	parser, can := initCan()
	sheetClient, sheet := initSheet(ctx)
	sheet.rows["water_program"] = fakeRow{"konstant", "konstant", "konstant", ""}
	sheet.rows["water_temp"] = fakeRow{"10", "10>", "60", ""}

	agentCfg := Config{
		UpdateCurrentSettings:  true,
		ApplyDesiredSettings:   true,
		ApplyAutomaticSettings: true,
		CanPollingInterval:     tick,
		SheetPollingInterval:   tick,
	}
	go RunForever(ctx, sheetClient, parser, can, nil, agentCfg)

	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempHigherId, float32(60.0)))
	can.simulateFrame(mustBuildFrame(t, us.IsAnswer, us.ActualWaterTempLowerId, float32(60.0)))

	time.Sleep(step)
	if err := sheet.checkHave("actual_water_temp", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("actual_water_temp_lower", "60"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkRowStart("water_temp", fakeRow{"10", "10>", "60"}); err != nil {
		t.Fatal(err)
	}
	if err := can.checkNotXmit(us.IsSet, us.DesiredConstantWaterTempId, float32(50)); err != nil {
		t.Fatal(err)
	}
}

type (
	fakeCell int
	logCell  int

	fakeRow []interface{}
	fakeLog struct {
		rng string
		row fakeRow
	}

	fakeSheet struct {
		lock sync.Mutex
		rows map[string]fakeRow
		logs []fakeLog
	}

	fakeTemp struct {
		temps map[string]float32
	}

	fakeCan struct {
		lock sync.Mutex
		recv []can.Frame
		xmit []can.Frame
	}
)

const (
	cellUser fakeCell = 0
	cellSent fakeCell = 1
	cellHave fakeCell = 2
	cellDate fakeCell = 3

	actualWaterTempHigherIdx logCell = 9
	actualWaterTempLowerIdx  logCell = 10
)

var (
	dated_re = regexp.MustCompile("(.*)_dated")
	user_re  = regexp.MustCompile("(.*)_want")
	have_re  = regexp.MustCompile("(.*)_have")
	sent_re  = regexp.MustCompile("(.*)_sent")
)

func initSheet(ctx context.Context) (gs.Client, *fakeSheet) {
	sheetCfg := gs.Config{}
	sheet := &fakeSheet{
		rows: map[string]fakeRow{},
	}
	sheetClient := gs.NewClient(ctx, sheet, sheetCfg)
	return sheetClient, sheet
}

func (s *fakeSheet) resolve(rng string) []interface{} {
	if m := dated_re.FindStringSubmatch(rng); m != nil {
		r := s.row(m[1])
		return r[cellHave:]
	}
	if m := user_re.FindStringSubmatch(rng); m != nil {
		r := s.row(m[1])
		return r[cellUser : cellUser+1]
	}
	if m := have_re.FindStringSubmatch(rng); m != nil {
		r := s.row(m[1])
		return r[cellHave : cellHave+1]
	}
	if m := sent_re.FindStringSubmatch(rng); m != nil {
		r := s.row(m[1])
		return r[cellSent : cellSent+1]
	}
	return s.row(rng)
}

func (s *fakeSheet) row(name string) fakeRow {
	row, ok := s.rows[name]
	if !ok {
		row = fakeRow{"", "", "", ""}
		s.rows[name] = row
	}
	return row
}

func (f fakeTemp) ReadTemp(id string) (float32, error) {
	return f.temps[id], nil
}

func TestOnReadingsAndSyncSensors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sheetClient, sheet := initSheet(ctx)
	gotCh := make(chan struct {
		header []interface{}
		row    []interface{}
	}, 4)

	cfg := Config{
		UpdateCurrentSettings:      true,
		LogCurrentSettingsToFiles:  true,
		SettingsQueryInterval:      step * 20,
		SettingsLogToFilesInterval: step * 20,
		SettingsLogDelay:           step,
		TemperatureSensors:         map[string]string{"id-5m": "temp5m", "id-1m": "temp1m"},
		OnReadings: func(now time.Time, header, row []interface{}) error {
			gotCh <- struct {
				header []interface{}
				row    []interface{}
			}{header: header, row: row}
			return nil
		},
	}
	sensors := fakeTemp{temps: map[string]float32{"id-1m": 19.5, "id-5m": 21.25}}

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunForever(ctx, sheetClient, nil, nil, sensors, cfg)
	}()

	select {
	case got := <-gotCh:
		n := len(got.header)
		if n != len(ReportedSettings)+2 {
			t.Fatalf("header len = %d, want %d", n, len(ReportedSettings)+2)
		}
		if got.header[n-2] != "temp1m" || got.header[n-1] != "temp5m" {
			t.Fatalf("unexpected sorted sensor headers: %v, %v", got.header[n-2], got.header[n-1])
		}
		if got.row[n-2] != "19.5" || got.row[n-1] != "21.25" {
			t.Fatalf("unexpected sensor values: %v, %v", got.row[n-2], got.row[n-1])
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for OnReadings callback")
	}

	if err := sheet.checkHave("temp1m", "19.5"); err != nil {
		t.Fatal(err)
	}
	if err := sheet.checkHave("temp5m", "21.25"); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
}

func (s *fakeSheet) Read(ctx context.Context, sheetId, rng string) ([][]interface{}, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	src := s.resolve(rng)
	cp := make([]interface{}, len(src))
	copy(cp, src)
	return [][]interface{}{cp}, nil
}

func (s *fakeSheet) BatchRead(ctx context.Context, sheetId string, ranges []string) ([][][]interface{}, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	res := make([][][]interface{}, len(ranges))
	for i, rng := range ranges {
		src := s.resolve(rng)
		cp := make([]interface{}, len(src))
		copy(cp, src)
		res[i] = [][]interface{}{cp}
	}
	return res, nil
}

func (s *fakeSheet) Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	r := s.resolve(rng)
	copy(r, vals[0])
	log.Printf("    %v\n", s.rows)
	return nil
}

func (s *fakeSheet) BatchWrite(ctx context.Context, sheetId string, updates []gs.RangeValues) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, u := range updates {
		r := s.resolve(u.Range)
		copy(r, u.Values[0])
	}
	log.Printf("    %v\n", s.rows)
	return nil
}

func (s *fakeSheet) Append(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	cp := make(fakeRow, len(vals[0]))
	copy(cp, vals[0])
	s.logs = append(s.logs, fakeLog{rng: rng, row: cp})
	return nil
}

func (s *fakeSheet) simulateUser(n string, v string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.rows[n][cellUser] = v
}

func (s *fakeSheet) logCount() int {
	s.lock.Lock()
	defer s.lock.Unlock()
	return len(s.logs)
}

func (s *fakeSheet) checkHave(n string, want string) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	r := s.rows[n]
	if len(r) <= int(cellHave) {
		return fmt.Errorf("%v[%v] should be %v, is %v", n, cellHave, want, r)
	}
	if have := r[cellHave]; have != want {
		return fmt.Errorf("%v[%v] should be %v, is %v", n, cellHave, want, have)
	}
	return nil
}

func (s *fakeSheet) checkRowStart(n string, wantRow fakeRow) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	r := s.rows[n]
	if len(r) < len(wantRow) {
		return fmt.Errorf("%v should start with %v, is %v", n, wantRow, r)
	}
	for i, want := range wantRow {
		if have := r[i]; have != want {
			return fmt.Errorf("%v should start with %v, is %v", n, wantRow, r)
		}
	}
	return nil
}

func (s *fakeSheet) checkLastLog(i logCell, want string) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	log := s.logs[len(s.logs)-1]
	if have := log.row[i]; have != want {
		return fmt.Errorf("expected %v at %v, but got %v in %v", want, i, have, log.row)
	}
	return nil
}

func initCan() (*us.Parser, *fakeCan) {
	parserCfg := us.Config{LogDetails: false}
	parser := us.NewParser(parserCfg)
	can := &fakeCan{
		lock: sync.Mutex{},
		recv: make([]can.Frame, 0),
		xmit: make([]can.Frame, 0),
	}
	return parser, can
}

func (c *fakeCan) TransmitFrame(ctx context.Context, f can.Frame) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	log.Printf("        Received %v\n", f)
	c.xmit = append(c.xmit, f)
	return nil
}

func (c *fakeCan) Receive() bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	return len(c.recv) > 0
}

func (c *fakeCan) Frame() can.Frame {
	c.lock.Lock()
	defer c.lock.Unlock()
	f := c.recv[0]
	c.recv = c.recv[1:]
	log.Printf("        Returning %v\n", f)
	return f
}

func (c *fakeCan) Reconnect(ctx context.Context) error { return nil }
func (c *fakeCan) Close() error                        { return nil }

func (c *fakeCan) simulateFrame(f can.Frame) {
	log.Printf("        Simulating %v\n", f)
	c.lock.Lock()
	defer c.lock.Unlock()
	c.recv = append(c.recv, f)
}

func (c *fakeCan) checkXmit(t us.MessageType, vid us.ValueId, v interface{}) error {
	f, ok, err := c.didXmit(t, vid, v)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("expected to transmit %v: (%v, %v, %v); transmitted %v", f, t, vid, v, c.xmit)
	}
	return nil
}

func (c *fakeCan) checkNotXmit(t us.MessageType, vid us.ValueId, v interface{}) error {
	f, ok, err := c.didXmit(t, vid, v)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("did not expect to transmit %v: (%v, %v, %v), transmitted %v", f, t, vid, v, c.xmit)
	}
	return nil
}

func (c *fakeCan) didXmit(t us.MessageType, vid us.ValueId, v interface{}) (*can.Frame, bool, error) {
	want, err := us.BuildFrame(t, vid, v)
	if err != nil {
		return nil, false, err
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	for _, have := range c.xmit {
		if have == want {
			return &want, true, nil
		}
	}
	return &want, false, nil
}

func (c *fakeCan) clearXmit() {
	log.Printf("        Clearing transmitted\n")
	c.lock.Lock()
	defer c.lock.Unlock()
	c.xmit = []can.Frame{}
}

func mustBuildFrame(tst *testing.T, t us.MessageType, vid us.ValueId, v interface{}) (f can.Frame) {
	f, err := us.BuildFrame(t, vid, v)
	if err != nil {
		tst.Fatal(err)
	}
	return
}
