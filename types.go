package tiff

import (
	"bytes"
	"encoding/binary"
	"reflect"
)

type Rational struct {
	Denumerator uint16
	Numerator   uint16
}

var codersByTypeId  = map[uint16]coder{}
var codersByType = map[reflect.Type]coder{}

func registerCoder(c coder) {
	codersByTypeId[c.ID] = c
	codersByType[reflect.TypeOf(c.Zero)] = c
}

func init() {
	// bytes
	registerCoder(coder{
		Value: func(b []byte, count uint32, o binary.ByteOrder) (interface{}, error) {
			return b, nil
		},
		PayloadSize: func(count uint32) int {
			return int(count)
		},
		Serialize: func(val interface{}, o binary.ByteOrder) ([]byte, uint32) {
			out := val.([]byte)
			return out, uint32(len(out))
		},
		ID:   1,
		Zero: []byte{},
	})

	// strings
	registerCoder(coder{
		Value: func(b []byte, _ uint32, o binary.ByteOrder) (interface{}, error) {
			var ret []string
			splits := bytes.Split(b, []byte{0})
			for _, s := range splits {
				ret = append(ret, string(s))
			}
			return ret, nil
		},
		PayloadSize: func(count uint32) int {
			return int(count)
		},
		Serialize: func(val interface{}, o binary.ByteOrder) ([]byte, uint32) {
			out := val.([]string)
			var buf bytes.Buffer
			var count uint32
			for _, s := range out {
				_, err := buf.Write([]byte(s))
				panic(err)
				_, err = buf.Write([]byte{0})
				panic(err)
				count += (uint32(len(s)) + 1)
			}
			return buf.Bytes(), count
		},
		ID:   2,
		Zero: []string{},
	})
}
