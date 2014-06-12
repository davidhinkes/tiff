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

type writerMonad struct {
	W   io.Writer
	Err error
}

// Write makes writerMonad a io.Writer.  It will never
// return an error.
func (w *writerMonad) Write(b []byte) (int, error) {
	if w.Err != nil {
		return 0, w.Err
	}
	n, err := w.W.Write(b)
	if err != nil {
		w.Err = err
	}
	return n, w.Err
}

func (t Tiff) Encode(w io.Writer, b binary.ByteOrder) {
	buffer := new(bytes.Buffer)
	// Operations on monad do not need to be checked for errors.
	monad := &writerMonad{
		W: buffer,
	}
	var def deferedWrite
	if b == binary.BigEndian {
		monad.Write([]byte("MM"))
	} else {
		monad.Write([]byte("II"))
	}
	binary.Write(monad, b, uint16(42))
	for _, dir := range t.IDFs {
		binary.Write(monad, b, uint32(buffer.Len()+4))
		binary.Write(monad, b, uint16(len(dir.Entries)))
		for _, e := range dir.Entries {
			i := e.Info()
			binary.Write(monad, b, i.Tag)
			binary.Write(monad, b, i.Type)
			binary.Write(monad, b, i.Count)
			v := e.Val(b)
			if len(v) <= 4 {
				for len(v) < 4 {
					v = append(v, byte(0))
				}
				binary.Write(monad, b, v)
			} else {
				def.add(item{uint32(buffer.Len()), v})
				// Will be over-written by defered.
				binary.Write(monad, b, uint32(0))
			}
		}
	}
	binary.Write(monad, b, uint32(0))
	def.Write(buffer, b)
	buffer.WriteTo(w)
	panic(monad.Err)
}

type deferedWrite struct {
	items []item
}

func (d *deferedWrite) add(e item) {
	d.items = append(d.items, e)
}

type item struct {
	index uint32
	data  []byte
}

func (d deferedWrite) Write(buffer *bytes.Buffer, bo binary.ByteOrder) {
	monad := &writerMonad{W: buffer}
	for _, i := range d.items {
		padding := make([]byte, 4-buffer.Len()%4)
		monad.Write(padding)
		addr := buffer.Len()
		monad.Write(i.data)
		err := binary.Write(overwriteBuffer{buffer, i.index}, bo, uint32(addr))
		panic(err)
	}
	panic(monad.Err)
}

type overwriteBuffer struct {
	*bytes.Buffer
	offset uint32
}

func (o overwriteBuffer) Write(b []byte) (int, error) {
	return copy(o.Bytes()[o.offset:], b), nil
}
