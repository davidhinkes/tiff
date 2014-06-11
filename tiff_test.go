package tiff

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestNothing(t *testing.T) {
	var _ Entry = ByteEntry{T: 0}
}

func TestSize(t *testing.T) {
	es := []Entry{
		ShortEntry{42, []uint16{43}},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	if got,want := len(buffer.Bytes()),26; got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestBigSize(t *testing.T) {
	es := []Entry{
		ASCIIEntry{43, []string{"Hello"}},
	}
	tiff := Tiff{
		IDFs: []IDF{
			{Entries: es},
		},
	}
	buffer := new(bytes.Buffer)
	tiff.Encode(buffer, binary.BigEndian)
	if got,want := len(buffer.Bytes()),34; got != want {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestByteOrder(t *testing.T) {
	es := []Entry{
		ShortEntry{42, []uint16{43}},
		ASCIIEntry{43, []string{"Hello"}},
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
