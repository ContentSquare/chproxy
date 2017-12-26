// @see https://github.com/yandex/ClickHouse/blob/ae8783aee3ef982b6eb7e1721dac4cb3ce73f0fe/dbms/src/IO/CompressedStream.h

package chdecompressor

import (
	"bytes"
	"fmt"
	"io"

	"github.com/DataDog/zstd"
	"github.com/pierrec/lz4"
)

type Reader struct {
	src             io.Reader
	data            []byte
	compressionType string
}

func NewReader(src io.Reader) *Reader {
	return &Reader{src: src}
}

func (r *Reader) Read(buf []byte) (n int, err error) {
	// exhaust remaining data from previous Read()
	if len(r.data) > 0 {
		n = copy(buf, r.data)
		r.data = r.data[n:]
		return
	}

	// checksum
	b := make([]byte, 16)
	_, err = io.ReadFull(r.src, b)
	if err != nil {
		return 0, err
	}

	if err := r.detectCompressionType(); err != nil {
		return 0, err
	}

	// 4 bytes, including 9 bytes of header length
	compressedSize, err := r.readUint32()
	if err != nil {
		return 0, fmt.Errorf("unable to read compressed size: %s", err)
	}
	compressedSize -= 9

	// 4 bytes of decompressed size
	decompressedSize, err := r.readUint32()
	if err != nil {
		return 0, fmt.Errorf("unable to read decompressed size: %s", err)
	}

	block := make([]byte, compressedSize)
	_, err = io.ReadFull(r.src, block)
	if err != nil {
		return 0, err
	}

	r.data = make([]byte, decompressedSize)
	_, err = r.decompress(block, r.data)
	if err != nil {
		return 0, err
	}
	n = copy(buf, r.data)
	r.data = r.data[n:]
	return
}

var (
	lz4Type  = []byte{0x82}
	zstdType = []byte{0x90}
)

func (r *Reader) detectCompressionType() error {
	b := make([]byte, 1)
	_, err := io.ReadFull(r.src, b)
	if err != nil {
		return err
	}
	if bytes.Equal(b, lz4Type) {
		r.compressionType = "lz4"
		return nil
	}
	if bytes.Equal(b, zstdType) {
		r.compressionType = "zstd"
		return nil
	}
	return fmt.Errorf("unsupported compression type: %v", b)
}

func (r *Reader) decompress(src, buf []byte) (int, error) {
	switch r.compressionType {
	case "lz4":
		return lz4.UncompressBlock(src, buf, 0)
	case "zstd":
		buf, err := zstd.Decompress(buf, src)
		return len(buf), err
	default:
		return 0, fmt.Errorf("BUG: unkown compression type %q", r.compressionType)
	}
}

func (r *Reader) readUint32() (uint32, error) {
	b := make([]byte, 4)
	_, err := io.ReadFull(r.src, b)
	if err != nil {
		return 0, err
	}
	_ = b[3]
	return uint32(b[0]) | (uint32(b[1]) << 8) | (uint32(b[2]) << 16) | (uint32(b[3]) << 24), nil
}
