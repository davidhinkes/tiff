package tiff

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestReader(t *testing.T) {
	var got = []byte{0, 0}
	want := []byte{12, 43}
	var buffer bytes.Buffer
	err := binary.Write(&buffer, binary.BigEndian, want)
	if err != nil {
		t.Error(err)
	}
	err = binary.Read(&buffer, binary.BigEndian, got)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSize(t *testing.T) {
	es := map[uint16]interface{}{
		42: []byte{43, 44},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	if got, want := len(buffer.Bytes()), 26; got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBigSize(t *testing.T) {
	es := map[uint16]interface{}{
		43: []string{"Hello"},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	bs := buffer.Bytes()
	if got, want := len(bs), 34; got != want {
		t.Errorf("got %v want %v", got, want)
	}
	_, err := Decode(bytes.NewReader(bs))
	if err != nil {
		t.Error(err)
	}
}

func TestByteOrder(t *testing.T) {
	es := map[uint16]interface{}{
		42: []byte{43},
		43: []string{"Hello"},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	got := buffer.Bytes()[0:2]
	if want := []byte("MM"); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDecode(t *testing.T) {
	es := map[uint16]interface{}{
		42: []byte{43},
		43: []string{"Hello"},
		7:  []byte{1, 2, 3, 4, 5, 6, 7, 8},
		8:  []uint16{1, 2, 3, 4, 5, 6, 7, 8},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	newTiff, err := Decode(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(tiff, newTiff) {
		t.Errorf("got not equal, wanted equal;\n%v\nvs\n%v", tiff, newTiff)
	}
}
