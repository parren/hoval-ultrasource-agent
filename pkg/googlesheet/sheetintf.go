package googlesheet

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

type (
	Client interface {
		ReadSettingValues(ctx context.Context, setting Setting) SettingValues
		ReadFacetValue(ctx context.Context, setting Setting, facet Facet) string
		WriteFacetValue(ctx context.Context, v FacetValue)
		RefreshFacetValue(ctx context.Context, v FacetValue)
		InvalidateSettingValue(s Setting)
		RefreshFluctuatingHaveValue(ctx context.Context, v FacetValue)
		Write(ctx context.Context, rng string, values [][]interface{})
		AppendOverwritingRows(ctx context.Context, rng string, values [][]interface{})
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

func NewServiceClient(ctx context.Context, cfg Config) ServiceClient {
	// https://stackoverflow.com/questions/39691100/golang-google-sheets-api-v4-write-update-example
	srv, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.CredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope))
	if err != nil {
		log.Fatalf("Unable to retrieve Google Sheets client: %v", err)
	}
	return &serviceImpl{srv: srv}
}

func NewClient(ctx context.Context, srv ServiceClient, cfg Config) Client {
	return &clientImpl{cfg: cfg, srv: srv,
		datedValues:  make(map[Setting]datedValue),
		latestValues: make(map[Setting]string)}
}

type (
	ServiceClient interface {
		Read(ctx context.Context, sheetId, rng string) [][]interface{}
		Write(ctx context.Context, sheetId, rng string, vals [][]interface{})
		Append(ctx context.Context, sheetId, rng string, vals [][]interface{})
	}

	serviceImpl struct {
		srv *sheets.Service
	}

	clientImpl struct {
		cfg          Config
		srv          ServiceClient
		datedValues  map[Setting]datedValue
		latestValues map[Setting]string
	}

	datedValue struct {
		Value        string
		LastUpdateAt time.Time
	}
)

func (c *clientImpl) ReadSettingValues(ctx context.Context, setting Setting) SettingValues {
	rng := string(setting)
	rows := c.read(ctx, rng)
	log.Printf("Read %v values: %v", rng, rows)
	row := rows[0]
	result := SettingValues{Setting: setting}
	result.Want = fmt.Sprintf("%v", row[0])
	result.Sent = fmt.Sprintf("%v", row[1])
	result.Have = fmt.Sprintf("%v", row[2])
	return result
}

func (c *clientImpl) ReadFacetValue(ctx context.Context, setting Setting, facet Facet) string {
	rng := facetRange(setting, facet)
	rows := c.read(ctx, rng)
	log.Printf("Read %v values: %v", rng, rows)
	if len(rows) < 1 || len(rows[0]) < 1 {
		return ""
	}
	return fmt.Sprintf("%v", rows[0][0])
}

func (c *clientImpl) WriteFacetValue(ctx context.Context, v FacetValue) {
	c.Write(ctx, facetRange(v.Setting, v.Facet), [][]interface{}{{v.Value}})
}

func (c *clientImpl) RefreshFacetValue(ctx context.Context, v FacetValue) {
	c.latestValues[v.Setting] = v.Value
	curr := c.ReadFacetValue(ctx, v.Setting, v.Facet)
	if curr != v.Value {
		log.Printf("Updating current setting %v\n", v)
		c.WriteFacetValue(ctx, v)
	}
}

func (c *clientImpl) InvalidateSettingValue(s Setting) {
	delete(c.datedValues, s)
}

func (c *clientImpl) RefreshFluctuatingHaveValue(ctx context.Context, v FacetValue) {
	if v.Facet != Have {
		log.Fatalf("must be a Have value: %v", v)
	}
	c.latestValues[v.Setting] = v.Value
	if dv, ok := c.datedValues[v.Setting]; ok {
		if dv.LastUpdateAt.Add(c.cfg.MaxHaveValueAge).After(time.Now()) {
			return
		}
	}
	curr := c.ReadFacetValue(ctx, v.Setting, HaveWithDate)
	if curr == v.Value {
		return
	}
	log.Printf("Updating current setting %v\n", v)
	timeStamp := time.Now()
	rangeName := fmt.Sprintf("%v_%v", v.Setting, HaveWithDate)
	c.Write(ctx, rangeName, [][]interface{}{{v.Value, FormatTimestamp(timeStamp)}})
	c.datedValues[v.Setting] = datedValue{Value: v.Value, LastUpdateAt: timeStamp}
}

func facetRange(setting Setting, facet Facet) string {
	return fmt.Sprintf("%v_%v", setting, facet)
}

func (c *clientImpl) read(ctx context.Context, rng string) [][]interface{} {
	return c.srv.Read(ctx, c.cfg.SheetId, rng)
}

func (c *clientImpl) Write(ctx context.Context, rng string, values [][]interface{}) {
	c.srv.Write(ctx, c.cfg.SheetId, rng, values)
}

func (c *clientImpl) AppendOverwritingRows(ctx context.Context, rng string, values [][]interface{}) {
	c.srv.Append(ctx, c.cfg.SheetId, rng, values)
}

func (c *clientImpl) LatestValues() map[Setting]string {
	return c.latestValues
}

func (s *serviceImpl) Read(ctx context.Context, sheetId, rng string) [][]interface{} {
	rsp, err := s.srv.Spreadsheets.Values.
		Get(sheetId, rng).
		ValueRenderOption("UNFORMATTED_VALUE").
		Do()
	if err != nil {
		log.Printf("Unable to retrieve range %v from sheet: %v", rng, err)
		return [][]interface{}{}
	}
	return rsp.Values
}

func (s *serviceImpl) Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) {
	rb := &sheets.ValueRange{Values: vals}
	_, err := s.srv.Spreadsheets.Values.
		Update(sheetId, rng, rb).
		ValueInputOption("USER_ENTERED").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to append range %v to sheet: %v", rng, err)
	}
}

func (s *serviceImpl) Append(ctx context.Context, sheetId, rng string, vals [][]interface{}) {
	rb := &sheets.ValueRange{Values: vals}
	_, err := s.srv.Spreadsheets.Values.
		Append(sheetId, rng, rb).
		ValueInputOption("USER_ENTERED").
		Context(ctx).Do()
	if err != nil {
		log.Printf("Unable to append range %v to sheet: %v", rng, err)
	}
}

func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}
