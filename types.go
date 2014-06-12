package tiff

import (
	"bytes"
	"encoding/binary"
)

type ByteEntry struct {
	T uint16
	V []uint8
}

func (e ByteEntry) Info() entryInfo {
	return entryInfo{e.T, 1, uint32(len(e.V))}
}
func (e ByteEntry) Val(_ binary.ByteOrder) []byte {
	b := new(bytes.Buffer)
	b.Write(e.V)
	return b.Bytes()
}

type ASCIIEntry struct {
	T uint16
	V []string
}

func (e ASCIIEntry) Info() entryInfo {
	sum := uint32(0)
	for _, a := range e.V {
		sum += (uint32(len(a)) + 1)
	}
	return entryInfo{e.T, 2, sum}
}
func (e ASCIIEntry) Val(_ binary.ByteOrder) []byte {
	buffer := new(bytes.Buffer)
	for _, s := range e.V {
		buffer.Write([]byte(s))
		buffer.Write([]byte{0})
	}
	return buffer.Bytes()
}

type ShortEntry struct {
	T uint16
	V []uint16
}

func (e ShortEntry) Info() entryInfo {
	return entryInfo{e.T, 3, uint32(len(e.V))}
}
func (e ShortEntry) Val(o binary.ByteOrder) []byte {
	b := new(bytes.Buffer)
	binary.Write(b, o, e.V)
	return b.Bytes()
}

type LongEntry struct {
	T uint16
	V []uint32
}

func (e LongEntry) Info() entryInfo {
	return entryInfo{e.T, 1, uint32(len(e.V))}
}
func (e LongEntry) Val(o binary.ByteOrder) []byte {
	b := new(bytes.Buffer)
	binary.Write(b, o, e.V)
	return b.Bytes()
}

type Rational struct {
	Numerator   uint32
	Denominator uint32
}

type RationalEntry struct {
	T uint16
	V []Rational
}

func (e RationalEntry) Info() entryInfo {
	return entryInfo{e.T, 1, uint32(len(e.V))}
}
func (e RationalEntry) Val(o binary.ByteOrder) []byte {
	buffer := new(bytes.Buffer)
	for _, v := range e.V {
		binary.Write(buffer, o, v.Numerator)
		binary.Write(buffer, o, v.Denominator)
	}
	return buffer.Bytes()
}
