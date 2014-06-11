package tiff

import (
	"bytes"
	"encoding/binary"
	"io"
)

type Tiff struct {
	IDFs []IDF
}

type IDF struct {
	Entries []Entry
}

type entryInfo struct {
	Tag   uint16
	Type  uint16
	Count uint32
}

type Entry interface {
	Info() entryInfo
	Val(b binary.ByteOrder) []byte
}

func (t Tiff) Encode(w io.Writer, b binary.ByteOrder) {
	buffer := new(bytes.Buffer)
	var def defered
	if b == binary.BigEndian {
		buffer.Write([]byte("MM"))
	} else {
		buffer.Write([]byte("II"))
	}
	binary.Write(buffer, b, uint16(42))
	for _, dir := range t.IDFs {
		binary.Write(buffer, b, uint32(buffer.Len()+4))
		binary.Write(buffer, b, uint16(len(dir.Entries)))
		for _, e := range dir.Entries {
			i := e.Info()
			binary.Write(buffer, b, i.Tag)
			binary.Write(buffer, b, i.Type)
			binary.Write(buffer, b, i.Count)
			v := e.Val(b)
			if len(v) <= 4 {
				for len(v) < 4 {
					v = append(v, byte(0))
				}
				binary.Write(buffer, b, v)
			} else {
				def.add(item{uint32(buffer.Len()), v})
				// Will be over-written by defered.
				binary.Write(buffer, b, uint32(0))
			}
		}
	}
	binary.Write(buffer, b, uint32(0))
	def.Write(buffer, b)
	buffer.WriteTo(w)
}

type defered struct {
	items []item
}
func (d *defered) add(e item) {
	d.items = append(d.items, e)
}
type item struct {
	index uint32
	data  []byte
}

func (d defered) Write(buffer *bytes.Buffer, bo binary.ByteOrder) {
	for _, i := range d.items {
		padding := make([]byte, 4-buffer.Len()%4)
		buffer.Write(padding)
		addr := buffer.Len()
		buffer.Write(i.data)
		binary.Write(overwriteBuffer{buffer, i.index}, bo, uint32(addr))
	}
}

type overwriteBuffer struct {
	*bytes.Buffer
	offset uint32
}

func (o overwriteBuffer) Write(b []byte) (int, error) {
	return copy(o.Bytes()[o.offset:], b), nil
}
