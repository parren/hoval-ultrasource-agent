package ultrasource

var (
	Controller = Device{Type: 15, Id: 255}
	Display    = Device{Type: 8, Id: 1}

	deviceNames = map[Device]string{
		Display:    "display",
		Controller: "main",
	}

	ActualOutsideTempId        = ValueId{Group: 0, Number: 0, Id: 0}
	ActualOutsideMinTempId     = ValueId{Group: 0, Number: 0, Id: 21103}
	ActualOutsideMaxTempId     = ValueId{Group: 0, Number: 0, Id: 21104}
	ActualOutsideAvgTempId     = ValueId{Group: 1, Number: 0, Id: 2020}
	HeatingProgramId           = ValueId{Group: 1, Number: 0, Id: 3050}
	WaterProgramId             = ValueId{Group: 2, Number: 0, Id: 5050}
	DesiredWaterTempId         = ValueId{Group: 2, Number: 0, Id: 1004}
	DesiredConstantWaterTempId = ValueId{Group: 2, Number: 0, Id: 5051}
	ActualWaterTempHigherId    = ValueId{Group: 2, Number: 0, Id: 4}
	ActualWaterTempLowerId     = ValueId{Group: 2, Number: 0, Id: 6}
	DesiredRoomTempId          = ValueId{Group: 1, Number: 0, Id: 1001}
	DesiredConstantRoomTempId  = ValueId{Group: 1, Number: 0, Id: 3051}
	DesiredHeatingTempId       = ValueId{Group: 1, Number: 0, Id: 1002}
	ActualHeatingTempId        = ValueId{Group: 1, Number: 0, Id: 2}
	DesiredHeaterTempId        = ValueId{Group: 10, Number: 1, Id: 1007}
	ActualHeaterTempId         = ValueId{Group: 10, Number: 1, Id: 7}
	ActualHeaterReturnTempId   = ValueId{Group: 10, Number: 1, Id: 8}
	ActualHeaterHoursId        = ValueId{Group: 10, Number: 1, Id: 2081}
	ActualModulationId         = ValueId{Group: 10, Number: 1, Id: 20052}
	ActualHeaterEnergyId       = ValueId{Group: 10, Number: 1, Id: 23003}
	ActualGridEnergyId         = ValueId{Group: 10, Number: 1, Id: 23002}
	HeaterModeId               = ValueId{Group: 1, Number: 0, Id: 2051}

	ActiveError1 = ValueId{Group: 0, Number: 0, Id: 29042}
	ActiveError2 = ValueId{Group: 0, Number: 0, Id: 29043}
	ActiveError3 = ValueId{Group: 0, Number: 0, Id: 29044}
	ActiveError4 = ValueId{Group: 0, Number: 0, Id: 29045}
	ActiveError5 = ValueId{Group: 0, Number: 0, Id: 29046}

	ValueDescs = map[ValueId]ValueDesc{
		// from Hoval docs: https://docs.google.com/spreadsheets/d/1UIvXYuhgNktCHV6-2tIfGE_4feMdR_PtWXL5rk_cBLc/edit#gid=1974512636
		ActualOutsideTempId:       {Name: "Aussentemp. °C", Conv: vTenthsDegreesCelsius},
		ActualOutsideMinTempId:    {Name: "Aussentemp. Tagesmin °C", Conv: vTenthsDegreesCelsius},
		ActualOutsideMaxTempId:    {Name: "Aussentemp. Tagesmax °C", Conv: vTenthsDegreesCelsius},
		ActualOutsideAvgTempId:    {Name: "Aussentemp. Mittelwert °C", Conv: vTenthsDegreesCelsius},
		DesiredRoomTempId:         {Name: "Raum-Soll °C", Conv: vTenthsDegreesCelsius},
		DesiredConstantRoomTempId: {Name: "Normal-Raumtemperatur Heizbetrieb °C", Conv: vTenthsDegreesCelsius},
		DesiredHeatingTempId:      {Name: "Vorlauf-Soll °C", Conv: vTenthsDegreesCelsius},
		ActualHeatingTempId:       {Name: "Vorlauf-Ist °C", Conv: vTenthsDegreesCelsius},
		HeatingProgramId: {Name: "Betriebswahl Heizung", Conv: listConverter(1, []string{
			"Standby", "Woche 1", "Woche 2", "", "Konstant", "Sparbetrieb", "", "Handbetrieb Heizen", "Handbetrieb Kühlen"})},
		ActualWaterTempHigherId: {Name: "Warmwasser-Ist SF (unten) °C", Conv: vTenthsDegreesCelsius},
		ActualWaterTempLowerId:  {Name: "Warmwasser-Ist SF2 (oben) °C", Conv: vTenthsDegreesCelsius},
		DesiredWaterTempId:      {Name: "Warmwasser-Soll (v1) °C", Conv: vTenthsDegreesCelsius},
		WaterProgramId: {Name: "Betriebswahl Warmwasser", Conv: listConverter(1, []string{
			"Standby", "Woche 1", "Woche 2", "", "Konstant", "", "Sparbetrieb"})},
		DesiredConstantWaterTempId: {Name: "Warmwasser-Soll (v2) °C", Conv: vTenthsDegreesCelsius},
		DesiredHeaterTempId:        {Name: "Wärmeerzeuger-Soll °C", Conv: vTenthsDegreesCelsius},
		ActualHeaterTempId:         {Name: "Wärmeerzeuger-Ist °C", Conv: vTenthsDegreesCelsius},
		ActualHeaterReturnTempId:   {Name: "Wärmeerzeuger-Ruecklauf °C", Conv: vTenthsDegreesCelsius},
		ActualHeaterHoursId:        {Name: "Betriebsstunden h", Conv: vHours},
		ActualModulationId:         {Name: "Modulation %", Conv: vPercent},
		ActualHeaterEnergyId:       {Name: "Heizleistung kW", Conv: vKiloWatts},
		ActualGridEnergyId:         {Name: "Elektroleistung kW", Conv: vKiloWatts},

		HeaterModeId: {Name: "Status Heizkreisregelung", Conv: valueConverter{toValue: listParser([]string{
			"Abgeschaltet",
			"Normal Heizbetrieb",
			"Komfort Heizbetrieb",
			"Spar Heizbetrieb",
			"Frostbetrieb",
			"Zwangsabnahme (bei Zwang > +50%)",
			"Zwangsdrosselung (bei Zwang < -50%)",
			"Ferienbetrieb",
			"Partybetrieb",
			"Normal Kuehlbetrieb",
			"Komfort Kuehlbetrieb",
			"Spar Kuehlbetrieb",
			"Stoerung",
			"Handbetrieb",
			"Schutz Kuehlbetrieb",
			"Partybetrieb Kuehlen",
			"Austrocknung Aufheizphase",
			"Austrocknung Stationärphase",
			"Austrocknung Abkuehlphase",
			"Austrocknung Endphase",
			"",
			"",
			"Kuehlbetrieb Extern/Konstantanforderung",
			"Heizbetrieb Extern/Konstantanforderung",
			"",
			"",
			"Vorzugsbetrieb SmartGrid"})}},

		ActiveError1: {Name: "Aktiver Fehler 1", Conv: vActiveError},
		ActiveError2: {Name: "Aktiver Fehler 2", Conv: vActiveError},
		ActiveError3: {Name: "Aktiver Fehler 3", Conv: vActiveError},
		ActiveError4: {Name: "Aktiver Fehler 4", Conv: vActiveError},
		ActiveError5: {Name: "Aktiver Fehler 5", Conv: vActiveError},

		{Group: 10, Number: 1, Id: 2053}: {Name: "Status Wärmeerzeugerregelung", Conv: vU8},
		{Group: 10, Number: 1, Id: 9075}: {Name: "Betriebswahl Wärmeerzeuger", Conv: valueConverter{toValue: listParser([]string{
			"Deakt.", "Automatik", "", "", "Heizen", "Kühlen"})}},
		{Group: 10, Number: 1, Id: 20053}: {Name: "Betriebsmeldung Kompressor FA", Conv: vU8},
		{Group: 10, Number: 1, Id: 23085}: {Name: "Emissionstest aktivieren", Conv: vU8},
		// from system settings: https://docs.google.com/spreadsheets/d/1An_R-BGNlrP__Yml479R4d3zDWAG7q4Iifh6VwGwAsk/edit#gid=0
		{Group: 1, Number: 0, Id: 4005}: {Name: "Funktionsbezeichnung Heizkreis 1", Conv: vText},
		// unknown and checked against Hoval docs
		{Group: 1, Number: 0, Id: 1}:     {},
		{Group: 1, Number: 0, Id: 502}:   {Name: "Bezeichnung Tagesprogramm Heizung ganzer Tag", Conv: vText},
		{Group: 1, Number: 0, Id: 503}:   {},
		{Group: 1, Number: 0, Id: 504}:   {},
		{Group: 1, Number: 0, Id: 505}:   {Name: "Bezeichnung Wochenprogramm Heizung Woche 1", Conv: vText},
		{Group: 1, Number: 0, Id: 3058}:  {},
		{Group: 1, Number: 0, Id: 7014}:  {},
		{Group: 1, Number: 0, Id: 20125}: {},
		{Group: 2, Number: 0, Id: 503}:   {},
		{Group: 2, Number: 0, Id: 505}:   {Name: "Bezeichnung Warmwasserprogramm Woche 1", Conv: vText},
		{Group: 2, Number: 0, Id: 4005}:  {Name: "Funktionsbezeichnung Warmwasser 1", Conv: vText},
		{Group: 2, Number: 0, Id: 20125}: {},
		{Group: 10, Number: 1, Id: 1100}: {},
	}
)

type (
	ActiveErrorMsg struct {
		ShowDate       uint16
		HideTime       uint16
		HideDate       uint16
		Source         uint16
		FunctionGroup  uint8
		FunctionNumber uint8
		ErrorType      uint8
		ErrorCode      uint16
	}

	ActiveError struct {
		Msg           ActiveErrorMsg
		ErrorTypeText string
		ErrorCodeText string
		SourceText    string
	}
)

const (
	NoShowDate = 0xff00
	NoHideTime = 0xffff
	NoHideDate = 0xffff
	NoSource   = 0xffff
	NoError    = 0x0000
)

var (
	ErrorTypes = map[uint8]string{
		0x00: "OK",
		0x31: "Blockierung (temporaer)",
		0x32: "Warnung",
	}

	ErrorCodes = map[uint16]string{
		0x0000: "OK",
		0xb100: "Durchfluss Kondensatorkreis (B:11)",
	}

	Sources = map[uint16]string{
		0x3c07: "TTE-WEZ / WFA200",
	}
)

// answer Aktiver Fehler 1 as ultrasource.activeErrorMsg{
// ShowDate:0x300, HideTime:0xb00, HideDate:0x100,
// Source:0x3c07, FunctionGroup:0x1c, FunctionNumber:0x1, ErrorType:0x31, ErrorCode:0xb100}
// Fehlercode: B:11
// Beschreibung: Durchfluss Kondensatorkreis
// Quelle: TTE-WEZ 1 / WFA200 (WFA200)
