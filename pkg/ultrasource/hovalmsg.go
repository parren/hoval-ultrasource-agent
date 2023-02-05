package ultrasource

import (
	"encoding/binary"
	"fmt"
	"time"

	"go.einride.tech/can"
)

type (
	Config struct {
		LogDetails bool
	}

	MessageType    byte
	DeviceType     byte
	DeviceId       byte
	FunctionGroup  byte
	FunctionNumber byte
	DataPointId    uint16

	Device struct {
		Type DeviceType
		Id   DeviceId
	}

	ValueId struct {
		Group  FunctionGroup
		Number FunctionNumber
		Id     DataPointId
	}

	Message struct {
		Timestamp time.Time
		Type      MessageType
		Device    Device
		Id        ValueId
		Value     interface{}
	}

	sequenceKey struct {
		device Device
		id     byte
	}

	Parser struct {
		cfg     Config
		pending map[sequenceKey]*unfinished
	}

	valueConverter struct {
		toValue     func([]byte) (interface{}, error)
		appendValue func([]byte, interface{}) ([]byte, error)
	}

	ValueDesc struct {
		Name string
		Conv valueConverter
	}

	messageTypeData struct {
		name      string
		noValueId bool
		isText    bool
	}
)

const (
	StartOfMessage byte = 0x1f

	IsQuery  MessageType = 0x40
	IsAnswer MessageType = 0x42
	IsSet    MessageType = 0x46
)

var (
	messageTypeDatas = map[MessageType]messageTypeData{
		IsQuery:  {name: "query"},
		IsAnswer: {name: "answer"},
		IsSet:    {name: "set"},
		0x8:      {noValueId: true},
		0x44:     {},
		0x4c:     {noValueId: true},
		0x50:     {noValueId: true},
		0x52:     {noValueId: true},
		0x56:     {},
		0x62:     {noValueId: true, isText: true}, // display?
		0x61:     {noValueId: true},
		0x70:     {noValueId: true},
		0x74:     {noValueId: true},
	}

	vText = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return toUtf8(b), nil
		}}

	vU8 = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return b[0], nil
		},
		appendValue: func(bytes []byte, v interface{}) ([]byte, error) {
			return append(bytes, v.(byte)), nil
		}}

	vTenthsDegreesCelsius = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return float32(int16(binary.BigEndian.Uint16(b))) / 10, nil
		},
		appendValue: func(bytes []byte, v interface{}) ([]byte, error) {
			f := v.(float32)
			i := int16(f * 10)
			u := uint16(i)
			return binary.BigEndian.AppendUint16(bytes, u), nil
		}}

	vPercent = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return float32(b[0]) / 100, nil
		}}

	vHours = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return int(binary.BigEndian.Uint32(b)), nil
		}}

	vKiloWatts = valueConverter{
		toValue: func(b []byte) (interface{}, error) {
			return float32(binary.BigEndian.Uint16(b)) / 100, nil
		}}
)

func listParser(options []string) func(b []byte) (interface{}, error) {
	return func(b []byte) (interface{}, error) {
		var i int
		switch len(b) {
		case 1:
			i = int(b[0])
		case 2:
			i = int(binary.LittleEndian.Uint16(b))
		case 4:
			i = int(binary.LittleEndian.Uint32(b))
		}
		if i >= len(options) {
			return fmt.Sprintf("?UNKNOWN(%v)", i), nil
		}
		return options[i], nil
	}
}

func listConverter(len int, options []string) valueConverter {
	return valueConverter{
		toValue: listParser(options),
		appendValue: func(b []byte, v interface{}) ([]byte, error) {
			s := v.(string)
			for i, o := range options {
				if s == o {
					switch len {
					case 1:
						return append(b, byte(i)), nil
					case 2:
						return binary.LittleEndian.AppendUint16(b, uint16(i)), nil
					case 4:
						return binary.LittleEndian.AppendUint32(b, uint32(i)), nil
					}
				}
			}
			return nil, fmt.Errorf("no version for %v in %v", v, options)
		}}
}

func BuildFrame(t MessageType, vid ValueId, v interface{}) (f can.Frame, err error) {
	bytes, err := buildMessage(t, vid, v)
	if err != nil {
		return
	}
	f.IsExtended = true
	// Observed in the logs: 1fe00801
	f.ID = binary.BigEndian.Uint32([]byte{
		StartOfMessage,
		0xe0,
		byte(Display.Type),
		byte(Display.Id),
	})
	f.Length = byte(len(bytes))
	copy(f.Data[:], bytes)
	return
}

func buildMessage(t MessageType, vid ValueId, v interface{}) (bytes []byte, err error) {
	frameCount := 1
	bytes = []byte{byte(frameCount), byte(t)}
	bytes = append(bytes, byte(vid.Group))
	bytes = append(bytes, byte(vid.Number))
	bytes = binary.BigEndian.AppendUint16(bytes, uint16(vid.Id))
	if v != nil {
		vd, ok := ValueDescs[vid]
		if !ok {
			err = fmt.Errorf("no conversion for %v", vid)
			return
		}
		if vd.Conv.appendValue == nil {
			err = fmt.Errorf("no appendValue for %v", vid)
			return
		}
		bytes, err = vd.Conv.appendValue(bytes, v)
	}
	return
}

func NewParser(cfg Config) *Parser {
	return &Parser{
		cfg:     cfg,
		pending: map[sequenceKey]*unfinished{},
	}
}

type unfinished struct {
	data            []byte
	remainingFrames byte
}

func (p *Parser) ParseFrame(f can.Frame) (m *Message, err error) {
	idBytes := [4]byte{}
	binary.BigEndian.PutUint32(idBytes[:], f.ID)
	frameType := idBytes[0]
	d := Device{Type: DeviceType(idBytes[2]), Id: DeviceId(idBytes[3])}
	if p.cfg.LogDetails {
		fmt.Printf("\t\t\tframet=%v, ?=%v, devt=%v, devid=%v\n",
			frameType, idBytes[1], d.Type, d.Id)
	}
	switch frameType {
	case 0x0:
		// Unknown message
		break
	case StartOfMessage:
		// Start of message
		if f.Length < 2 {
			err = fmt.Errorf("length<2 in %v", f)
			return
		}
		totalLen := f.Data[0]
		remainingFrames := totalLen >> 3
		if remainingFrames == 0 {
			m, err = p.parseMessage(d, f.Data[1:f.Length])
			return
		}
		key := sequenceKey{device: d, id: f.Data[1]}
		data := f.Data[2:f.Length]
		if p.cfg.LogDetails {
			fmt.Printf("%v start: %v frames=%v total=%v len=%v\n", f, key, remainingFrames, totalLen, len(data))
		}
		p.pending[key] = &unfinished{
			data:            data,
			remainingFrames: remainingFrames - 1,
		}
	default:
		// Continuation of message
		key := sequenceKey{device: d, id: f.Data[0]}
		unf, ok := p.pending[key]
		if !ok {
			err = fmt.Errorf("key %v not pending for type=0x%x, dev=%v in %v", key, frameType, d, f)
			return
		}
		data := f.Data[1:f.Length]
		unf.data = append(unf.data, data...)
		unf.remainingFrames = unf.remainingFrames - 1
		if p.cfg.LogDetails {
			fmt.Printf("%v added: %v, frames=%v len=%v\n", f, key, unf.remainingFrames, len(data))
		}
		if unf.remainingFrames == 0 {
			delete(p.pending, key)
			data := unf.data
			crc := binary.BigEndian.Uint16(data[len(data)-2:])
			data = data[:len(data)-2]
			if p.cfg.LogDetails {
				fmt.Printf(" -> complete %v: crc=%v, len=%v %v\n", key, crc, len(data), data)
			}
			m, err = p.parseMessage(d, data)
		}
	}
	return
}

func (p Parser) parseMessage(dev Device, raw []byte) (m *Message, err error) {
	t := MessageType(raw[0])
	td := messageTypeDatas[t]
	data := raw[1:]
	var vid ValueId
	if !td.noValueId {
		vid.Group = FunctionGroup(data[0])
		vid.Number = FunctionNumber(data[1])
		vid.Id = DataPointId(binary.BigEndian.Uint16(data[2:4]))
		data = data[4:]
		if p.cfg.LogDetails {
			fmt.Printf(" -> id %v\n", vid)
		}
	}
	var value interface{} = data
	switch t {
	case IsQuery:
		m = &Message{Timestamp: time.Now(), Type: t, Device: dev, Id: vid}
	case IsAnswer, IsSet:
		vd, ok := ValueDescs[vid]
		if ok && vd.Conv.toValue != nil {
			value, err = vd.Conv.toValue(data)
		} else {
			value = []interface{}{data, toUtf8(data)}
		}
		m = &Message{Timestamp: time.Now(), Type: t, Device: dev, Id: vid, Value: value}
	default:
		if td.isText {
			value = toUtf8(data)
		} else {
			value = []interface{}{data, toUtf8(data)}
		}
		m = &Message{Timestamp: time.Now(), Type: t, Device: dev, Id: vid, Value: value}
	}
	return
}

func (id ValueId) Unknown() bool {
	d, ok := ValueDescs[id]
	return !ok || len(d.Name) == 0
}

func (id ValueId) String() string {
	d, ok := ValueDescs[id]
	if !ok {
		return fmt.Sprintf("?UNKNOWN{%v,%v,%v}", id.Group, id.Number, id.Id)
	}
	if len(d.Name) == 0 {
		return fmt.Sprintf("?(%v,%v,%v)", id.Group, id.Number, id.Id)
	}
	return d.Name
}

func (t MessageType) Unknown() bool {
	s, ok := messageTypeDatas[t]
	return !ok || len(s.name) == 0
}

func (t MessageType) String() string {
	s, ok := messageTypeDatas[t]
	if !ok {
		return fmt.Sprintf("?UNKNOWN(0x%x)", int(t))
	}
	if len(s.name) == 0 {
		return fmt.Sprintf("?(0x%x)", int(t))
	}
	return s.name
}

func (d Device) String() string {
	n, ok := deviceNames[d]
	if !ok {
		return fmt.Sprintf("?dev{type=%v, id=%v}}", d.Type, d.Id)
	}
	return n
}

func (m Message) String() string {
	if messageTypeDatas[m.Type].noValueId {
		return fmt.Sprintf("%v %v from %v", m.Type, m.Value, m.Device)
	}
	return fmt.Sprintf("%v %v as %#v from %v", m.Type, m.Id, m.Value, m.Device)
}

func toUtf8(iso8859_1_buf []byte) string {
	buf := make([]rune, len(iso8859_1_buf))
	for i, b := range iso8859_1_buf {
		buf[i] = rune(b)
	}
	return string(buf)
}
