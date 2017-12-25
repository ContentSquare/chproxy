package lz4

import (
	"bytes"
	"fmt"
	"github.com/pierrec/lz4"
	"io"
)

type Reader struct {
	src  io.Reader
	data []byte
}

func NewReader(src io.Reader) *Reader {
	return &Reader{src: src}
}

var lz4Type = []byte{0x82}

func (r *Reader) Read(buf []byte) (n int, err error) {
	// exhaust remaining data from previous Read()
	if len(r.data) > 0 {
		n = copy(buf, r.data)
		r.data = r.data[n:]
		if len(r.data) == 0 {
			r.data = nil
		}
		return
	}

	// checksum
	b := make([]byte, 16)
	n, err = r.src.Read(b)
	if err != nil {
		return 0, err
	}
	if n < 16 {
		return 0, fmt.Errorf("invalid checksum read")
	}
	magic := make([]byte, 1)
	_, err = r.src.Read(magic)
	if err != nil {
		return 0, err
	}
	if !bytes.Equal(magic, lz4Type) {
		return 0, fmt.Errorf("wrong compressing alghoritm: %q", string(magic))
	}
	compressedSize, err := r.readUint32()
	if err != nil {
		return 0, fmt.Errorf("unable to read compressed size: %s", err)
	}
	compressedSize -= 9 // header

	decompressedSize, err := r.readUint32()
	if err != nil {
		return 0, fmt.Errorf("unable to read decompressed size: %s", err)
	}

	fmt.Println()
	block := make([]byte, compressedSize)
	n, err = r.src.Read(block)
	if err != nil {
		return 0, err
	}
	if n < int(compressedSize) {
		return 0, fmt.Errorf("compressed data length %d missmatches with expected %d", compressedSize, n)
	}

	free := cap(buf) - len(buf)
	if free >= int(decompressedSize) {
		return lz4.UncompressBlock(block, buf, 0)
	}

	// provided buffer is to small so write to internal
	r.data = make([]byte, decompressedSize)
	_, err = lz4.UncompressBlock(block, r.data, 0)
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func (r *Reader) readUint32() (uint32, error) {
	b := make([]byte, 4)
	n, err := r.src.Read(b)
	if err != nil {
		return 0, err
	}
	if n < 4 {
		return 0, fmt.Errorf("not enough data to read")
	}
	_ = b[3]
	return uint32(b[0]) | (uint32(b[1]) << 8) | (uint32(b[2]) << 16) | (uint32(b[3]) << 24), nil
}
