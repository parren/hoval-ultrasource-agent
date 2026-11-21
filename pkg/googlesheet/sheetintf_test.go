package googlesheet

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubService struct {
	readVals      [][]interface{}
	batchReadVals [][][]interface{}
	readErr       error
	writeErr      error
	writes        int
	batchWrites   int
	lastBatch     []RangeValues
}

func (s *stubService) Read(ctx context.Context, sheetId, rng string) ([][]interface{}, error) {
	return s.readVals, s.readErr
}

func (s *stubService) BatchRead(ctx context.Context, sheetId string, ranges []string) ([][][]interface{}, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.batchReadVals, nil
}

func (s *stubService) Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	s.writes++
	return s.writeErr
}

func (s *stubService) BatchWrite(ctx context.Context, sheetId string, updates []RangeValues) error {
	s.batchWrites++
	s.lastBatch = updates
	return s.writeErr
}

func (s *stubService) Append(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	return nil
}

func TestReadSettingValues_ErrorsAndShortRows(t *testing.T) {
	ctx := context.Background()

	t.Run("read error returns error without panic", func(t *testing.T) {
		srv := &stubService{readErr: errors.New("transient 503")}
		c := NewClient(ctx, srv, Config{SheetId: "sheet"})
		if _, err := c.ReadSettingValues(ctx, HeatingProgram); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("empty rows returns zero values without panic", func(t *testing.T) {
		srv := &stubService{readVals: nil}
		c := NewClient(ctx, srv, Config{SheetId: "sheet"})
		got, err := c.ReadSettingValues(ctx, HeatingProgram)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != (SettingValues{Setting: HeatingProgram}) {
			t.Fatalf("got %+v, want empty SettingValues", got)
		}
	})

	t.Run("short row with trailing empty cells does not panic", func(t *testing.T) {
		srv := &stubService{readVals: [][]interface{}{{"konstant"}}}
		c := NewClient(ctx, srv, Config{SheetId: "sheet"})
		got, err := c.ReadSettingValues(ctx, HeatingProgram)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := SettingValues{Setting: HeatingProgram, Want: "konstant"}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
}

func TestReadSettings_Batch(t *testing.T) {
	ctx := context.Background()
	srv := &stubService{
		batchReadVals: [][][]interface{}{
			{{"konstant", "konstant", "konstant"}},
			{{"20.5"}},
		},
	}
	c := NewClient(ctx, srv, Config{SheetId: "sheet"})
	got, err := c.ReadSettings(ctx, []Setting{HeatingProgram, DesiredHeatingTemp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[HeatingProgram] != (SettingValues{Setting: HeatingProgram, Want: "konstant", Sent: "konstant", Have: "konstant"}) {
		t.Fatalf("unexpected HeatingProgram: %+v", got[HeatingProgram])
	}
	if got[DesiredHeatingTemp] != (SettingValues{Setting: DesiredHeatingTemp, Want: "20.5"}) {
		t.Fatalf("unexpected DesiredHeatingTemp: %+v", got[DesiredHeatingTemp])
	}
}

func TestBatchFlushAndCache(t *testing.T) {
	ctx := context.Background()
	srv := &stubService{}
	c := NewClient(ctx, srv, Config{SheetId: "sheet", MaxHaveValueAge: time.Hour})

	c.RefreshFacetValue(ctx, FacetValue{Setting: HeatingProgram, Facet: Have, Value: "konstant"})
	c.RefreshFluctuatingHaveValue(ctx, FacetValue{Setting: ActualWaterTempHigher, Facet: Have, Value: "50"})

	if srv.batchWrites != 0 {
		t.Fatalf("expected no writes before Flush, got %d", srv.batchWrites)
	}
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	if srv.batchWrites != 1 || len(srv.lastBatch) != 2 {
		t.Fatalf("expected 1 batch write of 2 ranges, got %d writes of %d ranges", srv.batchWrites, len(srv.lastBatch))
	}

	// Unchanged values should not trigger another write.
	c.RefreshFacetValue(ctx, FacetValue{Setting: HeatingProgram, Facet: Have, Value: "konstant"})
	c.RefreshFluctuatingHaveValue(ctx, FacetValue{Setting: ActualWaterTempHigher, Facet: Have, Value: "50"})
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("second Flush failed: %v", err)
	}
	if srv.batchWrites != 1 {
		t.Fatalf("expected cache hit with no extra batch write, got %d", srv.batchWrites)
	}

	// Failed flush retains pending items for retry.
	srv.writeErr = errors.New("network down")
	c.RefreshFacetValue(ctx, FacetValue{Setting: HeatingProgram, Facet: Have, Value: "standby"})
	if err := c.Flush(ctx); err == nil {
		t.Fatal("expected Flush error, got nil")
	}
	srv.writeErr = nil
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("retry Flush failed: %v", err)
	}
	if srv.batchWrites != 3 {
		t.Fatalf("expected retry batch write, got %d total", srv.batchWrites)
	}
}
