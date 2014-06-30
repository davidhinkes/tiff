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

// encodeIDF assumes that the writer is already at the desired location.
func encodeIDF(bldr *builder, idf IDF, bo binary.ByteOrder) (uint32, error) {
	binary.Write(bldr.buffer, bo, uint16(len(idf.Entries)))
	// The tiff spec says the tags should be encoded in increasing order.
	var tags []int // Using []int instead of []uint16 to make life easier with sort.Ints.
	for tag, _ := range idf.Entries {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	for _, tag := range tags {
		err := encodeEntry(bldr, uint16(tag), idf.Entries[uint16(tag)], bo)
		if err != nil {
			return 0, err
		}
	}
	at := bldr.buffer.Len()
	binary.Write(bldr.buffer, bo, uint32(0))
	// The tiff spec says the tags should be encoded in increasing order.
	return uint32(at), nil
}

func encodeEntry(bldr *builder, tag uint16, val interface{}, bo binary.ByteOrder) error {
	coder, ok := codersByType[reflect.TypeOf(val)]
	if !ok {
		return nil
	}
	buf := bldr.buffer
	binary.Write(buf, bo, tag)
	binary.Write(buf, bo, coder.ID)
	v, count := coder.Marshal(val, bo)
	binary.Write(buf, bo, count)
	if len(v) <= 4 {
		for len(v) < 4 {
			v = append(v, byte(0))
		}
		binary.Write(buf, bo, v)
	} else {
		entryBldr := makeBuilder()
		entryBldr.buffer.Write(v)
		bldr.builders[uint32(buf.Len())] = entryBldr
		// Will be over-written by defered.
		binary.Write(buf, bo, uint32(0))
	}
	return nil
}

func (t Tiff) Encode(w io.Writer, b binary.ByteOrder) {
	bldr := makeBuilder()
	buffer := bldr.buffer
	// Operations on monad do not need to be checked for errors.
	monad := &writerMonad{
		W: buffer,
	}
	if b == binary.BigEndian {
		monad.Write([]byte("MM"))
	} else {
		monad.Write([]byte("II"))
	}
	binary.Write(monad, b, uint16(42))
	var previousAddr = uint32(buffer.Len())
	binary.Write(monad, b, uint32(0)) // will be overwritten
	var previousBldr = bldr
	for _, dir := range t.IDFs {
		newBldr := makeBuilder()
		addr, err := encodeIDF(newBldr, dir, b)
		if err != nil {
			log.Fatal(err)
		}
		previousBldr.builders[previousAddr] = newBldr
		previousAddr = addr
		previousBldr = newBldr
	}
	var finalOutputBuffer bytes.Buffer
	bldr.WriteTo(&finalOutputBuffer, b)
	finalOutputBuffer.WriteTo(w)
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

type builder struct {
	buffer   *bytes.Buffer
	builders map[uint32]*builder
}

func (b builder) WriteTo(buf *bytes.Buffer, bo binary.ByteOrder) error {
	var err error
	offset := uint32(buf.Len())
	_, err = buf.Write(b.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("builder.Write: %v", err)
	}
	for addr, subBuilder := range b.builders {
		for buf.Len()%4 != 0 {
			_, err = buf.Write([]byte{0})
			if err != nil {
				return err
			}
		}
		err = binary.Write(overwriteBuffer{buf, addr + offset}, bo, uint32(buf.Len()))
		if err != nil {
			return err
		}
		err = subBuilder.WriteTo(buf, bo)
		if err != nil {
			return err
		}
	}
	return nil
}

func makeBuilder() *builder {
	return &builder{
		buffer:   &bytes.Buffer{},
		builders: make(map[uint32]*builder)}
}
