package tiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"reflect"
	"sort"
)

type Tiff struct {
	IDFs []IDF
}

type IDF struct {
	// Entries is a map of tags to concrete values.
	Entries map[uint16]interface{}
}

type coder struct {
	Unmarshal   func(b []byte, count uint32, o binary.ByteOrder) (interface{}, error)
	PayloadSize func(count uint32) int
	// Marshal retursn the bytes to write and the count.
	Marshal func(val interface{}, o binary.ByteOrder) ([]byte, uint32)
	ID      uint16
	Zero    interface{}
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

type decodeContext struct {
	R io.ReadSeeker
	O binary.ByteOrder
}

// decodeEntry will append to the map.
func (ctx decodeContext) decodeEntry(m map[uint16]interface{}) error {
	var tag uint16
	var val interface{}
	var err error

	err = binary.Read(ctx.R, ctx.O, &tag)
	if err != nil {
		return err
	}
	var typeID uint16
	err = binary.Read(ctx.R, ctx.O, &typeID)
	if err != nil {
		return err
	}
	var count uint32
	err = binary.Read(ctx.R, ctx.O, &count)
	if err != nil {
		return err
	}
	coder, ok := codersByTypeID[typeID]
	if !ok {
		// typeID not known, this is OK and should not result in an error
		return nil
	}
	valueSize := coder.PayloadSize(count)
	value := make([]byte, valueSize)
	if valueSize <= 4 {
		bytes := make([]byte, 4)
		_, err := ctx.R.Read(bytes)
		if err != nil {
			return err
		}
		copy(value, bytes)
	} else {
		var valueAddr uint32
		err = binary.Read(ctx.R, ctx.O, &valueAddr)
		if err != nil {
			return err
		}
		// jump keeps track of where we are
		jump, err := ctx.R.Seek(0, 1)
		if err != nil {
			return err
		}
		_, err = ctx.R.Seek(int64(valueAddr), 0)
		if err != nil {
			return err
		}
		_, err = ctx.R.Read(value)
		if err != nil {
			return err
		}
		_, err = ctx.R.Seek(jump, 0)
		if err != nil {
			return err
		}
	}
	val, err = coder.Unmarshal(value, count, ctx.O)
	if err != nil {
		return err
	}
	m[tag] = val
	return nil
}

// decodeIDF assumes the next bytes to read are an IDF.
func (ctx decodeContext) decodeIDF() (IDF, error) {
	var err error
	var idf = IDF{map[uint16]interface{}{}}
	r := ctx.R
	ordering := ctx.O
	var numEntries uint16
	err = binary.Read(r, ordering, &numEntries)
	if err != nil {
		return idf, err
	}
	for z := uint16(0); z < numEntries; z++ {
		if err := ctx.decodeEntry(idf.Entries); err != nil {
			return idf, err
		}
	}
	return idf, nil
}

func Decode(r io.ReadSeeker) (Tiff, error) {
	var err error
	var tiff = Tiff{}
	hdr := make([]byte, 2)
	_, err = r.Read(hdr)
	if err != nil {
		return Tiff{}, err
	}
	hdrAsString := string(hdr)
	var ordering binary.ByteOrder
	if hdrAsString == "II" {
		ordering = binary.LittleEndian
	} else if hdrAsString == "MM" {
		ordering = binary.BigEndian
	} else {
		return Tiff{}, fmt.Errorf("expecting II or MM as first two bytes; got %v", hdrAsString)
	}
	var magicNumber uint16
	err = binary.Read(r, ordering, &magicNumber)
	if err != nil {
		return Tiff{}, err
	}
	if magicNumber != 42 {
		return Tiff{}, fmt.Errorf("expecting magic number equal to 42; got %v", magicNumber)
	}
	ctx := decodeContext{
		R: r,
		O: ordering,
	}
	for {
		var idfAddr uint32
		err = binary.Read(r, ordering, &idfAddr)
		if err != nil {
			return tiff, err
		}
		if idfAddr == 0 {
			return tiff, nil
		}
		_, err = r.Seek(int64(idfAddr), 0)
		if err != nil {
			return tiff, err
		}
		idf, err := ctx.decodeIDF()
		if err != nil {
			return tiff, err
		}
		tiff.IDFs = append(tiff.IDFs, idf)
	}
}

type encodingContext struct {
	W *writerMonad
	D *deferedWriter
	B *bytes.Buffer
	O binary.ByteOrder
}

// encodeIDF assumes that the writer is already at the desired location.
func (ctx encodingContext) encodeIDF(idf IDF) {
	binary.Write(ctx.W, ctx.O, uint16(len(idf.Entries)))
	// The tiff spec says the tags should be encoded in increasing order.
	var tags []int // Using []int instead of []uint16 to make life easier with sort.Ints.
	for tag, _ := range idf.Entries {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	for _, tag := range tags {
		ctx.encodeEntry(uint16(tag), idf.Entries[uint16(tag)])
	}
}

func (ctx encodingContext) encodeEntry(tag uint16, val interface{}) {
	coder, ok := codersByType[reflect.TypeOf(val)]
	if !ok {
		return
	}
	bo := ctx.O
	binary.Write(ctx.W, bo, tag)
	binary.Write(ctx.W, bo, coder.ID)
	v, count := coder.Marshal(val, bo)
	binary.Write(ctx.W, bo, count)
	if len(v) <= 4 {
		for len(v) < 4 {
			v = append(v, byte(0))
		}
		binary.Write(ctx.W, bo, v)
	} else {
		ctx.D.add(item{uint32(ctx.B.Len()), v})
		// Will be over-written by defered.
		binary.Write(ctx.W, bo, uint32(0))
	}
}

func (t Tiff) Encode(w io.Writer, b binary.ByteOrder) {
	buffer := new(bytes.Buffer)
	// Operations on monad do not need to be checked for errors.
	monad := &writerMonad{
		W: buffer,
	}
	def := new(deferedWriter)
	ctx := encodingContext{
		W: monad,
		D: def,
		B: buffer,
		O: b,
	}
	if b == binary.BigEndian {
		monad.Write([]byte("MM"))
	} else {
		monad.Write([]byte("II"))
	}
	binary.Write(monad, b, uint16(42))
	for _, dir := range t.IDFs {
		binary.Write(ctx.W, ctx.O, uint32(ctx.B.Len()+4))
		ctx.encodeIDF(dir)
	}
	binary.Write(monad, b, uint32(0))
	def.Write(buffer, b)
	buffer.WriteTo(w)
	if monad.Err != nil {
		log.Fatal(monad.Err)
	}
}

type deferedWriter struct {
	items []item
}

func (d *deferedWriter) add(e item) {
	d.items = append(d.items, e)
}

type item struct {
	index uint32
	data  []byte
}

func (d deferedWriter) Write(buffer *bytes.Buffer, bo binary.ByteOrder) {
	monad := &writerMonad{W: buffer}
	for _, i := range d.items {
		padding := make([]byte, 4-buffer.Len()%4)
		monad.Write(padding)
		addr := buffer.Len()
		monad.Write(i.data)
		err := binary.Write(overwriteBuffer{buffer, i.index}, bo, uint32(addr))
		if err != nil {
			log.Fatal(err)
		}
	}
	if monad.Err != nil {
		log.Fatal(monad.Err)
	}
}

type overwriteBuffer struct {
	*bytes.Buffer
	offset uint32
}

func (o overwriteBuffer) Write(b []byte) (int, error) {
	return copy(o.Bytes()[o.offset:], b), nil
}
