package googlesheet

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type (
	RangeValues struct {
		Range  string
		Values [][]interface{}
	}

	Client interface {
		ReadSettingValues(ctx context.Context, setting Setting) (SettingValues, error)
		ReadSettings(ctx context.Context, settings []Setting) (map[Setting]SettingValues, error)
		ReadFacetValue(ctx context.Context, setting Setting, facet Facet) (string, error)
		WriteFacetValue(ctx context.Context, v FacetValue) error
		RefreshFacetValue(ctx context.Context, v FacetValue)
		InvalidateSettingValue(s Setting)
		RefreshFluctuatingHaveValue(ctx context.Context, v FacetValue)
		Flush(ctx context.Context) error
		Write(ctx context.Context, rng string, values [][]interface{}) error
		AppendOverwritingRows(ctx context.Context, rng string, values [][]interface{}) error
		LatestValue(s Setting) (string, bool)
		LatestValues() map[Setting]string
	}

	Setting       string
	SettingValues struct {
		Setting Setting
		Want    string
		Sent    string
		Have    string
	}

	Facet      string
	FacetValue struct {
		Setting Setting
		Facet   Facet
		Value   string
	}
)

const (
	HeatingProgram        Setting = "heating_program"
	DesiredHeatingTemp    Setting = "room_temp"
	WaterProgram          Setting = "water_program"
	DesiredWaterTemp      Setting = "water_temp"
	ActualWaterTempHigher Setting = "actual_water_temp"
	ActualWaterTempLower  Setting = "actual_water_temp_lower"

	Want         Facet = "want"
	Sent         Facet = "sent"
	Have         Facet = "have"
	HaveWithDate Facet = "dated"
)

type Config struct {
	CredentialsFile string
	SheetId         string
	MaxHaveValueAge time.Duration
}

func NewServiceClient(ctx context.Context, cfg Config) (ServiceClient, error) {
	// https://stackoverflow.com/questions/39691100/golang-google-sheets-api-v4-write-update-example
	srv, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.CredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope))
	if err != nil {
		return nil, fmt.Errorf("retrieve Google Sheets client: %w", err)
	}
	return &serviceImpl{srv: srv}, nil
}

func NewClient(ctx context.Context, srv ServiceClient, cfg Config) Client {
	return &clientImpl{
		cfg:           cfg,
		srv:           srv,
		datedValues:   make(map[Setting]datedValue),
		writtenStable: make(map[Setting]string),
		latestValues:  make(map[Setting]string),
		pending:       make(map[Setting]pendingUpdate),
	}
}

type (
	ServiceClient interface {
		Read(ctx context.Context, sheetId, rng string) ([][]interface{}, error)
		BatchRead(ctx context.Context, sheetId string, ranges []string) ([][][]interface{}, error)
		Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) error
		BatchWrite(ctx context.Context, sheetId string, updates []RangeValues) error
		Append(ctx context.Context, sheetId, rng string, vals [][]interface{}) error
	}

	serviceImpl struct {
		srv *sheets.Service
	}

	pendingUpdate struct {
		rng    string
		values [][]interface{}
		val    string
		ts     time.Time
		stable bool
	}

	clientImpl struct {
		cfg           Config
		srv           ServiceClient
		mu            sync.Mutex
		datedValues   map[Setting]datedValue
		writtenStable map[Setting]string
		latestValues  map[Setting]string
		pending       map[Setting]pendingUpdate
	}

	datedValue struct {
		Value        string
		LastUpdateAt time.Time
	}
)

func parseSettingRow(setting Setting, rows [][]interface{}) SettingValues {
	result := SettingValues{Setting: setting}
	if len(rows) == 0 {
		return result
	}
	row := rows[0]
	if len(row) > 0 {
		result.Want = fmt.Sprintf("%v", row[0])
	}
	if len(row) > 1 {
		result.Sent = fmt.Sprintf("%v", row[1])
	}
	if len(row) > 2 {
		result.Have = fmt.Sprintf("%v", row[2])
	}
	return result
}

func (c *clientImpl) ReadSettingValues(ctx context.Context, setting Setting) (SettingValues, error) {
	rng := string(setting)
	rows, err := c.read(ctx, rng)
	if err != nil {
		return SettingValues{}, err
	}
	log.Printf("Read %v values: %v", rng, rows)
	return parseSettingRow(setting, rows), nil
}

func (c *clientImpl) ReadSettings(ctx context.Context, settings []Setting) (map[Setting]SettingValues, error) {
	if len(settings) == 0 {
		return map[Setting]SettingValues{}, nil
	}
	ranges := make([]string, len(settings))
	for i, s := range settings {
		ranges[i] = string(s)
	}
	allRows, err := c.srv.BatchRead(ctx, c.cfg.SheetId, ranges)
	if err != nil {
		return nil, err
	}
	res := make(map[Setting]SettingValues, len(settings))
	for i, s := range settings {
		var rows [][]interface{}
		if i < len(allRows) {
			rows = allRows[i]
		}
		res[s] = parseSettingRow(s, rows)
	}
	return res, nil
}

func (c *clientImpl) ReadFacetValue(ctx context.Context, setting Setting, facet Facet) (string, error) {
	rng := facetRange(setting, facet)
	rows, err := c.read(ctx, rng)
	if err != nil {
		return "", err
	}
	log.Printf("Read %v values: %v", rng, rows)
	if len(rows) < 1 || len(rows[0]) < 1 {
		return "", nil
	}
	return fmt.Sprintf("%v", rows[0][0]), nil
}

func (c *clientImpl) WriteFacetValue(ctx context.Context, v FacetValue) error {
	return c.Write(ctx, facetRange(v.Setting, v.Facet), [][]interface{}{{v.Value}})
}

func (c *clientImpl) RefreshFacetValue(ctx context.Context, v FacetValue) {
	c.mu.Lock()
	c.latestValues[v.Setting] = v.Value
	if prev, ok := c.writtenStable[v.Setting]; ok && prev == v.Value {
		c.mu.Unlock()
		return
	}
	c.pending[v.Setting] = pendingUpdate{
		rng:    facetRange(v.Setting, v.Facet),
		values: [][]interface{}{{v.Value}},
		val:    v.Value,
		stable: true,
	}
	c.mu.Unlock()
}

func (c *clientImpl) InvalidateSettingValue(s Setting) {
	c.mu.Lock()
	delete(c.datedValues, s)
	delete(c.writtenStable, s)
	c.mu.Unlock()
}

func (c *clientImpl) RefreshFluctuatingHaveValue(ctx context.Context, v FacetValue) {
	if v.Facet != Have {
		log.Fatalf("must be a Have value: %v", v)
	}
	now := time.Now()
	c.mu.Lock()
	c.latestValues[v.Setting] = v.Value
	if dv, ok := c.datedValues[v.Setting]; ok {
		if dv.Value == v.Value {
			c.mu.Unlock()
			return
		}
		if dv.LastUpdateAt.Add(c.cfg.MaxHaveValueAge).After(now) {
			c.mu.Unlock()
			return
		}
	}
	c.pending[v.Setting] = pendingUpdate{
		rng:    fmt.Sprintf("%v_%v", v.Setting, HaveWithDate),
		values: [][]interface{}{{v.Value, FormatTimestamp(now)}},
		val:    v.Value,
		ts:     now,
		stable: false,
	}
	c.mu.Unlock()
}

func (c *clientImpl) Flush(ctx context.Context) error {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return nil
	}
	batch := make(map[Setting]pendingUpdate, len(c.pending))
	updates := make([]RangeValues, 0, len(c.pending))
	for s, u := range c.pending {
		batch[s] = u
		updates = append(updates, RangeValues{Range: u.rng, Values: u.values})
	}
	c.mu.Unlock()

	log.Printf("Flushing %d setting update(s) to sheet", len(updates))
	if err := c.srv.BatchWrite(ctx, c.cfg.SheetId, updates); err != nil {
		return err
	}

	c.mu.Lock()
	for s, u := range batch {
		if u.stable {
			c.writtenStable[s] = u.val
		} else {
			c.datedValues[s] = datedValue{Value: u.val, LastUpdateAt: u.ts}
		}
		// Only remove from pending if no newer value arrived while flushing.
		if cur, ok := c.pending[s]; ok && cur.val == u.val && cur.rng == u.rng {
			delete(c.pending, s)
		}
	}
	c.mu.Unlock()
	return nil
}

func facetRange(setting Setting, facet Facet) string {
	return fmt.Sprintf("%v_%v", setting, facet)
}

func (c *clientImpl) read(ctx context.Context, rng string) ([][]interface{}, error) {
	return c.srv.Read(ctx, c.cfg.SheetId, rng)
}

func (c *clientImpl) Write(ctx context.Context, rng string, values [][]interface{}) error {
	return c.srv.Write(ctx, c.cfg.SheetId, rng, values)
}

func (c *clientImpl) AppendOverwritingRows(ctx context.Context, rng string, values [][]interface{}) error {
	return c.srv.Append(ctx, c.cfg.SheetId, rng, values)
}

func (c *clientImpl) LatestValue(s Setting) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.latestValues[s]
	return v, ok
}

func (c *clientImpl) LatestValues() map[Setting]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(map[Setting]string, len(c.latestValues))
	for k, v := range c.latestValues {
		cp[k] = v
	}
	return cp
}

func (s *serviceImpl) Read(ctx context.Context, sheetId, rng string) ([][]interface{}, error) {
	rsp, err := s.srv.Spreadsheets.Values.
		Get(sheetId, rng).
		ValueRenderOption("UNFORMATTED_VALUE").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to retrieve range %v from sheet: %v", rng, err)
		return nil, err
	}
	return rsp.Values, nil
}

func (s *serviceImpl) BatchRead(ctx context.Context, sheetId string, ranges []string) ([][][]interface{}, error) {
	rsp, err := s.srv.Spreadsheets.Values.
		BatchGet(sheetId).
		Ranges(ranges...).
		ValueRenderOption("UNFORMATTED_VALUE").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to batch-retrieve ranges %v from sheet: %v", ranges, err)
		return nil, err
	}
	res := make([][][]interface{}, len(rsp.ValueRanges))
	for i, vr := range rsp.ValueRanges {
		if vr != nil {
			res[i] = vr.Values
		}
	}
	return res, nil
}

func (s *serviceImpl) Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	rb := &sheets.ValueRange{Values: vals}
	_, err := s.srv.Spreadsheets.Values.
		Update(sheetId, rng, rb).
		ValueInputOption("USER_ENTERED").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to update range %v in sheet: %v", rng, err)
	}
	return err
}

func (s *serviceImpl) BatchWrite(ctx context.Context, sheetId string, updates []RangeValues) error {
	data := make([]*sheets.ValueRange, len(updates))
	for i, u := range updates {
		data[i] = &sheets.ValueRange{
			Range:  u.Range,
			Values: u.Values,
		}
	}
	req := &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "USER_ENTERED",
		Data:             data,
	}
	_, err := s.srv.Spreadsheets.Values.
		BatchUpdate(sheetId, req).
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to batch-update %d range(s) in sheet: %v", len(updates), err)
	}
	return err
}

func (s *serviceImpl) Append(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	rb := &sheets.ValueRange{Values: vals}
	_, err := s.srv.Spreadsheets.Values.
		Append(sheetId, rng, rb).
		ValueInputOption("USER_ENTERED").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to append range %v to sheet: %v", rng, err)
	}
	return err
}

func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
