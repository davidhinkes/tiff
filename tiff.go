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

const (
	tiffHeaderSize = 8
)

type Tiff struct {
	IDFs []IDF
}

type IDF struct {
	// Entries is a map of tags to concrete values.
	Entries map[uint16]interface{}
	SubIDFs []IDF
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
func (ctx decodeContext) decodeIDF() (IDF, uint32, error) {
	var err error
	var idf = IDF{map[uint16]interface{}{}, nil}
	r := ctx.R
	ordering := ctx.O
	var numEntries uint16
	err = binary.Read(r, ordering, &numEntries)
	if err != nil {
		return idf, 0, err
	}
	for z := uint16(0); z < numEntries; z++ {
		if err := ctx.decodeEntry(idf.Entries); err != nil {
			return idf, 0, err
		}
	}
	var nextIDFAddr uint32
	err = binary.Read(r, ordering, &nextIDFAddr)
	if err != nil {
		return idf, 0, err
	}
	err = ctx.fillSubIDF(&idf)
	if err != nil {
		return idf, nextIDFAddr, err
	}
	return idf, nextIDFAddr, nil
}

func (ctx decodeContext) fillSubIDF(idf *IDF) error {
	i, ok := idf.Entries[330]
	if !ok {
		return nil
	}
	addrs, ok := i.([]uint32)
	if !ok {
		return nil
	}
	for _, addr := range addrs {
		_, err := ctx.R.Seek(int64(addr), 0)
		if err != nil {
			return err
		}
		subIDF, _, err := ctx.decodeIDF()
		if err != nil {
			return err
		}
		idf.SubIDFs = append(idf.SubIDFs, subIDF)
	}
	return nil
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
	var idfAddr uint32
	err = binary.Read(r, ordering, &idfAddr)
	if err != nil {
		return tiff, err
	}
	for {
		if idfAddr == 0 {
			return tiff, nil
		}
		_, err = r.Seek(int64(idfAddr), 0)
		if err != nil {
			return tiff, err
		}
		idf, next, err := ctx.decodeIDF()
		if err != nil {
			return tiff, err
		}
		idfAddr = next
		tiff.IDFs = append(tiff.IDFs, idf)
	}
}

type encodeContext struct {
	B *builder
	O binary.ByteOrder
}

// encodeIDF assumes that the writer is already at the desired location.
func (ctx encodeContext) encodeIDF(idf IDF, nextIDFAddr uint32) (uint32, error) {
	buf := new(bytes.Buffer)
	binary.Write(buf, ctx.O, uint16(len(idf.Entries)))
	// The tiff spec says the tags should be encoded in increasing order.
	var tags []int // Using []int instead of []uint16 to make life easier with sort.Ints.
	for tag, _ := range idf.Entries {
		tags = append(tags, int(tag))
	}
	sort.Ints(tags)
	for _, tag := range tags {
		err := ctx.encodeEntry(buf, uint16(tag), idf.Entries[uint16(tag)])
		if err != nil {
			return 0, err
		}
	}
	binary.Write(buf, ctx.O, nextIDFAddr)
	at := ctx.B.add(buf)
	// The tiff spec says the tags should be encoded in increasing order.
	return at, nil
}

func (ctx encodeContext) encodeEntry(buf *bytes.Buffer, tag uint16, val interface{}) error {
	coder, ok := codersByType[reflect.TypeOf(val)]
	if !ok {
		return nil
	}
	bo := ctx.O
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
		at := ctx.B.add(bytes.NewBuffer(v))
		binary.Write(buf, bo, at)
	}
	return nil
}

func (t Tiff) Encode(w io.Writer, b binary.ByteOrder) {
	bldr := newBuilder(tiffHeaderSize)
	ctx := encodeContext{bldr, b}
	nextIDFAddr := uint32(0)
	for i := len(t.IDFs) - 1; i >= 0; i-- {
		dir := t.IDFs[i]
		var err error
		nextIDFAddr, err = ctx.encodeIDF(dir, nextIDFAddr)
		if err != nil {
			log.Fatal(err)
		}
	}
	headerBuffer := new(bytes.Buffer)

	// Operations on monad do not need to be checked for errors.
	monad := &writerMonad{
		W: headerBuffer,
	}
	if b == binary.BigEndian {
		monad.Write([]byte("MM"))
	} else {
		monad.Write([]byte("II"))
	}
	binary.Write(monad, b, uint16(42))
	binary.Write(monad, b, nextIDFAddr)
	if length := headerBuffer.Len(); length != tiffHeaderSize {
		log.Fatalf("header buffer lengh must be 12; got %v", length)
	}
	bldr.addAt(0, headerBuffer)
	if monad.Err != nil {
		log.Fatal(monad.Err)
	}
	bldr.writeTo(w)
}

type builder struct {
	next    uint32
	buffers map[uint32]*bytes.Buffer
}

func newBuilder(offset uint32) *builder {
	return &builder{
		next:    offset,
		buffers: make(map[uint32]*bytes.Buffer),
	}
}

func (b *builder) add(buf *bytes.Buffer) uint32 {
	if m := b.next % 4; m != 0 {
		b.next += 4 - m
	}
	ret := b.next
	b.buffers[b.next] = buf
	b.next += uint32(buf.Len())
	return ret
}

func (b *builder) addAt(at uint32, buf *bytes.Buffer) {
	b.buffers[at] = buf
}

// writeTo will log.Fatal if error are encountered.
func (b *builder) writeTo(w io.Writer) {
	var offsets []int // TODO: make this []uint32
	for k, _ := range b.buffers {
		offsets = append(offsets, int(k))
	}
	sort.Ints(offsets)
	written := 0
	for _, offset := range offsets {
		if written > offset {
			log.Fatalf("offset (%v) > written (%v)", offset, written)
		}
		for written < offset {
			w.Write([]byte{0})
			written += 1
		}
		b, err := w.Write(b.buffers[uint32(offset)].Bytes())
		written += b
		if err != nil {
			log.Fatal(err)
		}
	}
}
