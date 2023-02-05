package agent

import (
	"fmt"
	"strconv"

	"github.com/vishalkuo/bimap"
	"go.einride.tech/can"
	gs "parren.ch/ultrasource/pkg/googlesheet"
	us "parren.ch/ultrasource/pkg/ultrasource"
)

type Setting struct {
	SheetSetting gs.Setting
	valueId      us.ValueId
	converter    *converter
	isStable     bool
	isDesired    bool
}

type converter struct {
	ParseMessage    func(us.Message) (v string, ok bool)
	MakeUpdateFrame func(string, us.ValueId) (can.Frame, error)
}

const (
	WaterProgramConstant = "konstant"
)

var (
	HeatingProgram = Setting{
		SheetSetting: gs.HeatingProgram,
		valueId:      us.HeatingProgramId,
		isStable:     true,
		isDesired:    true,
		converter: strMap(map[string]string{
			"Woche 1":  "anwesend",
			"Woche 2":  "abwesend",
			"Konstant": "konstant",
			"Standby":  "standby",
		})}
	SettableDesiredHeatingTemp = Setting{
		SheetSetting: gs.DesiredHeatingTemp,
		valueId:      us.DesiredConstantRoomTempId,
		isStable:     true,
		isDesired:    true,
		converter:    &celsius}
	WaterProgram = Setting{
		SheetSetting: gs.WaterProgram,
		valueId:      us.WaterProgramId,
		isStable:     true,
		isDesired:    true,
		converter: strMap(map[string]string{
			"Konstant": WaterProgramConstant,
			"Standby":  "standby",
		})}
	SettableDesiredWaterTemp = Setting{
		SheetSetting: gs.DesiredWaterTemp,
		valueId:      us.DesiredConstantWaterTempId,
		isStable:     true,
		isDesired:    true,
		converter:    &celsius}
)

var PushedSettings = []Setting{
	HeatingProgram,
	SettableDesiredHeatingTemp,
	WaterProgram,
	SettableDesiredWaterTemp,
}

var ReportedSettings = []Setting{
	HeatingProgram,
	SettableDesiredHeatingTemp,
	WaterProgram,
	SettableDesiredWaterTemp,

	{SheetSetting: "resulting_room_temp", valueId: us.DesiredRoomTempId},
	{SheetSetting: "heating_temp", valueId: us.DesiredHeatingTempId},
	{SheetSetting: "actual_heating_temp", valueId: us.ActualHeaterTempId},

	{SheetSetting: "resulting_water_temp", valueId: us.DesiredWaterTempId},
	{SheetSetting: gs.ActualWaterTempHigher, valueId: us.ActualWaterTempHigherId},
	{SheetSetting: gs.ActualWaterTempLower, valueId: us.ActualWaterTempLowerId},

	{SheetSetting: "desired_heater_temp", valueId: us.DesiredHeaterTempId},
	{SheetSetting: "actual_heater_temp", valueId: us.ActualHeaterTempId},
	{SheetSetting: "actual_heater_return_temp", valueId: us.ActualHeaterReturnTempId},

	{SheetSetting: "actual_outside_temp", valueId: us.ActualOutsideTempId},
	{SheetSetting: "actual_outside_min_temp", valueId: us.ActualOutsideMinTempId},
	{SheetSetting: "actual_outside_max_temp", valueId: us.ActualOutsideMaxTempId},
	{SheetSetting: "actual_outside_avg_temp", valueId: us.ActualOutsideAvgTempId},

	{SheetSetting: "modulation", valueId: us.ActualModulationId},
	{SheetSetting: "hours", valueId: us.ActualHeaterHoursId},
	{SheetSetting: "heat_energy", valueId: us.ActualHeaterEnergyId},
	{SheetSetting: "grid_energy", valueId: us.ActualGridEnergyId},
	{SheetSetting: "heater_mode", valueId: us.HeaterModeId},
}

func (s Setting) ParseMessage(m us.Message) (v string, ok bool) {
	if s.converter == nil {
		return fmt.Sprintf("%v", m.Value), true
	}
	return s.converter.ParseMessage(m)
}

func (s Setting) MakeUpdateFrame(vs gs.SettingValues) (f can.Frame, err error) {
	return s.converter.MakeUpdateFrame(vs.Want, s.valueId)
}

var celsius = converter{
	ParseMessage: func(m us.Message) (v string, ok bool) {
		v = fmt.Sprintf("%v", m.Value)
		ok = true
		return
	},
	MakeUpdateFrame: func(v string, vid us.ValueId) (f can.Frame, err error) {
		celsius, err := strconv.ParseFloat(v, 32)
		if err != nil {
			err = fmt.Errorf("failed to parse number from: %v: %v", v, err)
			return
		}
		if celsius > 65 || celsius < 0 {
			err = fmt.Errorf("outside range 0-65 °C: %v", celsius)
			return
		}
		f, err = us.BuildFrame(us.IsSet, vid, float32(celsius))
		return
	}}

func strMap(valueByValue map[string]string) *converter {
	bm := bimap.NewBiMapFromMap(valueByValue)
	return &converter{
		ParseMessage: func(m us.Message) (string, bool) {
			v := fmt.Sprintf("%v", m.Value)
			if s, ok := bm.Get(v); ok {
				v = s
			}
			return v, true
		},
		MakeUpdateFrame: func(v string, vid us.ValueId) (f can.Frame, err error) {
			if s, ok := bm.GetInverse(v); ok {
				v = s
			}
			f, err = us.BuildFrame(us.IsSet, vid, v)
			return
		}}
}
