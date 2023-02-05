package ultrasource

import (
	"fmt"
	"testing"

	"go.einride.tech/can"
)

type test struct {
	frame   string
	msgType MessageType
	valueId ValueId
	value   interface{}
}

var buildAndParseFrameTests = []test{
	{"1FE00801#014601000BEA01", IsSet, HeatingProgramId, "Woche 1"},
	{"1FE00801#014601000BEA02", IsSet, HeatingProgramId, "Woche 2"},

	{"1FE00801#0146020013BA00", IsSet, WaterProgramId, "Standby"},
	{"1FE00801#0146020013BA04", IsSet, WaterProgramId, "Konstant"},

	{"1FE00801#014001000BEB", IsQuery, DesiredConstantRoomTempId, nil},
	{"1FE00801#014601000BEB00C8", IsSet, DesiredConstantRoomTempId, float32(20)},
	{"1FE00801#014601000BEB00CD", IsSet, DesiredConstantRoomTempId, float32(20.5)},

	{"1FE00801#0140020013BB", IsQuery, DesiredConstantWaterTempId, nil},
	{"1FE00801#0146020013BB01C2", IsSet, DesiredConstantWaterTempId, float32(45)},
	{"1FE00801#0146020013BB01F4", IsSet, DesiredConstantWaterTempId, float32(50)},

	{"1FE00801#014000000000", IsQuery, ActualOutsideTempId, nil},
}

var parseOnlyFrameTests = []test{
	{"1FC00FFF#014201000BEA01", IsAnswer, HeatingProgramId, "Woche 1"},
	{"1FC00FFF#014201000BEA02", IsAnswer, HeatingProgramId, "Woche 2"},

	{"1FC00FFF#0142020013BA00", IsAnswer, WaterProgramId, "Standby"},
	{"1FC00FFF#0142020013BA04", IsAnswer, WaterProgramId, "Konstant"},

	{"1FC00FFF#014201000BEB00C8", IsAnswer, DesiredConstantRoomTempId, float32(20)},
	{"1FC00FFF#014201000BEB00CD", IsAnswer, DesiredConstantRoomTempId, float32(20.5)},

	{"1FC00FFF#0142020013BB01C2", IsAnswer, DesiredConstantWaterTempId, float32(45)},
	{"1FC00FFF#0142020013BB01F4", IsAnswer, DesiredConstantWaterTempId, float32(50)},

	{"1FC00FFF#0142000000000019", IsAnswer, ActualOutsideTempId, float32(2.5)},
	{"1FC00FFF#014200000000FFE7", IsAnswer, ActualOutsideTempId, float32(-2.5)},
}

func TestBuildFrame(t *testing.T) {
	for _, tt := range buildAndParseFrameTests {
		t.Run(fmt.Sprintf("%v", tt), func(t *testing.T) {
			have, err := BuildFrame(tt.msgType, tt.valueId, tt.value)
			if tt.frame != have.String() || err != nil {
				t.Fatalf(`Have %v, %v; want %v, nil`, have, err, tt.frame)
			}
		})
	}
}

func TestParseFrame(t *testing.T) {
	p := NewParser(Config{})
	for _, tts := range [][]test{
		buildAndParseFrameTests,
		parseOnlyFrameTests,
	} {
		for _, tt := range tts {
			t.Run(fmt.Sprintf("%v", tt), func(t *testing.T) {
				f := can.Frame{}
				err := f.UnmarshalString(tt.frame)
				if err != nil {
					t.Fatalf(`Failed to unmarshal %v`, tt.frame)
				}
				m, err := p.ParseFrame(f)
				if tt.msgType != m.Type || tt.valueId != m.Id || tt.value != m.Value || err != nil {
					payload, _ := BuildFrame(tt.msgType, tt.valueId, tt.value)
					payload.ID = 0
					t.Fatalf(`Have %v (%v), %v; want %v, nil`, m, payload, err, tt)
				}
			})
		}
	}
}
