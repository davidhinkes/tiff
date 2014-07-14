package tiff

import (
	"bytes"
	"encoding/binary"
	"reflect"
)

type coder struct {
	Unmarshal   func(b []byte, count uint32, o binary.ByteOrder) (interface{}, error)
	PayloadSize func(count uint32) int
	// Marshal returns the bytes to write and the count.
	Marshal func(val interface{}, o binary.ByteOrder) ([]byte, uint32)
	ID      uint16
	Zero    interface{}
}

type Rational struct {
	Denumerator uint32
	Numerator   uint32
}

type SRational struct {
	Denumerator int32
	Numerator   int32
}

var codersByTypeID = map[uint16]coder{}
var codersByType = map[reflect.Type]coder{}

func registerCoder(c coder) {
	codersByTypeID[c.ID] = c
	codersByType[reflect.TypeOf(c.Zero)] = c
}

func makeSimpleCoder(id uint16, zero interface{}, bytesPerElem int) coder {
	ty := reflect.TypeOf(zero)
	sliceOfTy := reflect.SliceOf(ty)
	return coder{
		Unmarshal: func(b []byte, count uint32, o binary.ByteOrder) (interface{}, error) {
			elems := len(b) / bytesPerElem
			v := reflect.MakeSlice(sliceOfTy, elems, elems)
			err := binary.Read(bytes.NewReader(b), o, v.Interface())
			if err != nil {
				panic(err)
			}
			return v.Interface(), nil
		},
		PayloadSize: func(count uint32) int {
			return int(count) * bytesPerElem
		},
		Marshal: func(val interface{}, o binary.ByteOrder) ([]byte, uint32) {
			buffer := new(bytes.Buffer)
			err := binary.Write(buffer, o, val)
			if err != nil {
				panic(err)
			}
			return buffer.Bytes(), uint32(reflect.ValueOf(val).Len())
		},
		ID:   id,
		Zero: reflect.Zero(sliceOfTy).Interface(),
	}
}

func init() {
	// Basic types of slices can use the simple coder.
	registerCoder(makeSimpleCoder(1, uint8(0), 1))
	registerCoder(makeSimpleCoder(3, uint16(0), 2))
	registerCoder(makeSimpleCoder(4, uint32(0), 4))
	registerCoder(makeSimpleCoder(5, Rational{}, 8))
	registerCoder(makeSimpleCoder(6, int8(0), 1))
	registerCoder(makeSimpleCoder(7, byte(0), 1))
	registerCoder(makeSimpleCoder(8, int16(0), 2))
	registerCoder(makeSimpleCoder(9, int32(0), 4))
	registerCoder(makeSimpleCoder(10, SRational{}, 8))
	registerCoder(makeSimpleCoder(11, float64(0), 8))
	registerCoder(makeSimpleCoder(12, float32(0), 4))

	// strings
	registerCoder(coder{
		Unmarshal: func(b []byte, _ uint32, o binary.ByteOrder) (interface{}, error) {
			var ret []string
			splits := bytes.Split(b, []byte{0})
			for _, s := range splits {
				if len(s) > 0 {
					ret = append(ret, string(s))
				}
			}
			return ret, nil
		},
		PayloadSize: func(count uint32) int {
			return int(count)
		},
		Marshal: func(val interface{}, o binary.ByteOrder) ([]byte, uint32) {
			out := val.([]string)
			var buf bytes.Buffer
			var count uint32
			for _, s := range out {
				_, err := buf.Write([]byte(s))
				if err != nil {
					panic(err)
				}
				_, err = buf.Write([]byte{0})
				if err != nil {
					panic(err)
				}
				count += (uint32(len(s)) + 1)
			}
			return buf.Bytes(), count
		},
		ID:   2,
		Zero: []string{},
	})
}
