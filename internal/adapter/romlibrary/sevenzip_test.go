package romlibrary

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
	"unicode/utf16"
)

// The property ids of a 7z header.
const (
	sevenZipEnd         = 0x00
	sevenZipHeader      = 0x01
	sevenZipMainStreams = 0x04
	sevenZipFilesInfo   = 0x05
	sevenZipPackInfo    = 0x06
	sevenZipUnpackInfo  = 0x07
	sevenZipSubStreams  = 0x08
	sevenZipSize        = 0x09
	sevenZipFolder      = 0x0b
	sevenZipCodersSize  = 0x0c
	sevenZipNumUnpack   = 0x0d
	sevenZipName        = 0x11
	sevenZipHeaderSize  = 32
)

// sevenZipArchive builds a 7z archive that stores every entry uncompressed,
// one folder per entry.
func sevenZipArchive(t *testing.T, entries []zipEntry) []byte {
	t.Helper()

	var packed bytes.Buffer
	for _, e := range entries {
		_, _ = packed.WriteString(e.body)
	}

	var header bytes.Buffer
	header.WriteByte(sevenZipHeader)
	header.WriteByte(sevenZipMainStreams)

	header.WriteByte(sevenZipPackInfo)
	writeSevenZipNumber(&header, 0)
	writeSevenZipNumber(&header, uint64(len(entries)))
	header.WriteByte(sevenZipSize)
	for _, e := range entries {
		writeSevenZipNumber(&header, uint64(len(e.body)))
	}
	header.WriteByte(sevenZipEnd)

	header.WriteByte(sevenZipUnpackInfo)
	header.WriteByte(sevenZipFolder)
	writeSevenZipNumber(&header, uint64(len(entries)))
	header.WriteByte(0)
	for range entries {
		header.WriteByte(0x01)
		header.WriteByte(0x01)
		header.WriteByte(0x00)
	}
	header.WriteByte(sevenZipCodersSize)
	for _, e := range entries {
		writeSevenZipNumber(&header, uint64(len(e.body)))
	}
	header.WriteByte(sevenZipEnd)

	header.WriteByte(sevenZipSubStreams)
	header.WriteByte(sevenZipNumUnpack)
	for range entries {
		writeSevenZipNumber(&header, 1)
	}
	header.WriteByte(sevenZipEnd)

	header.WriteByte(sevenZipEnd)

	header.WriteByte(sevenZipFilesInfo)
	writeSevenZipNumber(&header, uint64(len(entries)))
	names := sevenZipNames(entries)
	header.WriteByte(sevenZipName)
	writeSevenZipNumber(&header, uint64(1+len(names)))
	header.WriteByte(0)
	_, _ = header.Write(names)
	header.WriteByte(sevenZipEnd)

	header.WriteByte(sevenZipEnd)

	out := make([]byte, 0, sevenZipHeaderSize+packed.Len()+header.Len())
	out = append(out, '7', 'z', 0xbc, 0xaf, 0x27, 0x1c, 0x00, 0x04)
	out = append(out, 0x00, 0x00, 0x00, 0x00)
	start := make([]byte, 20)
	binary.LittleEndian.PutUint64(start[0:8], uint64(packed.Len()))
	binary.LittleEndian.PutUint64(start[8:16], uint64(header.Len()))
	binary.LittleEndian.PutUint32(start[16:20], crc32.ChecksumIEEE(header.Bytes()))
	out = append(out, start...)
	binary.LittleEndian.PutUint32(out[8:12], crc32.ChecksumIEEE(start))
	out = append(out, packed.Bytes()...)
	out = append(out, header.Bytes()...)
	return out
}

// sevenZipNames encodes the names of the entries the way a 7z header holds them.
func sevenZipNames(entries []zipEntry) []byte {
	var names bytes.Buffer
	for _, e := range entries {
		for _, unit := range utf16.Encode([]rune(e.name)) {
			_, _ = names.Write([]byte{byte(unit), byte(unit >> 8)})
		}
		_, _ = names.Write([]byte{0x00, 0x00})
	}
	return names.Bytes()
}

// writeSevenZipNumber writes a number the way a 7z header holds them.
func writeSevenZipNumber(w *bytes.Buffer, value uint64) {
	first := byte(0)
	mask := byte(0x80)
	i := 0
	for ; i < 8; i++ {
		if value < uint64(1)<<(7*(i+1)) {
			first |= byte(value >> (8 * i))
			break
		}
		first |= mask
		mask >>= 1
	}
	_ = w.WriteByte(first)
	for ; i > 0; i-- {
		_ = w.WriteByte(byte(value))
		value >>= 8
	}
}
