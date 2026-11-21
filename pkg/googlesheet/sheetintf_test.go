package googlesheet

import (
	"context"
	"errors"
	"testing"
)

type stubService struct {
	readVals [][]interface{}
	readErr  error
	writeErr error
	writes   int
}

func (s *stubService) Read(ctx context.Context, sheetId, rng string) ([][]interface{}, error) {
	return s.readVals, s.readErr
}

func (s *stubService) Write(ctx context.Context, sheetId, rng string, vals [][]interface{}) error {
	s.writes++
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

func TestRefreshSkipsWriteOnReadError(t *testing.T) {
	ctx := context.Background()
	srv := &stubService{readErr: errors.New("offline")}
	c := NewClient(ctx, srv, Config{SheetId: "sheet"})

	c.RefreshFacetValue(ctx, FacetValue{Setting: HeatingProgram, Facet: Have, Value: "konstant"})
	c.RefreshFluctuatingHaveValue(ctx, FacetValue{Setting: ActualWaterTempHigher, Facet: Have, Value: "50"})

	if srv.writes != 0 {
		t.Fatalf("expected 0 writes on read error, got %d", srv.writes)
	}
	if c.LatestValues()[HeatingProgram] != "konstant" || c.LatestValues()[ActualWaterTempHigher] != "50" {
		t.Fatalf("expected latestValues to still record readings, got %v", c.LatestValues())
	}
}
