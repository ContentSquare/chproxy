package chdecompressor

import (
	"fmt"
	"io"

	"github.com/DataDog/zstd"
	"github.com/pierrec/lz4"
)

// Reader reads clickhouse compressed stream.
// See https://github.com/yandex/ClickHouse/blob/ae8783aee3ef982b6eb7e1721dac4cb3ce73f0fe/dbms/src/IO/CompressedStream.h
type Reader struct {
	src     io.Reader
	data    []byte
	scratch []byte
}

// NewReader returns new clickhouse compressed stream reader reading from src.
func NewReader(src io.Reader) *Reader {
	return &Reader{
		src:     src,
		scratch: make([]byte, 16),
	}
}

// Read reads up to len(buf) bytes from clickhouse compressed stream.
func (r *Reader) Read(buf []byte) (int, error) {
	// exhaust remaining data from previous Read()
	if len(r.data) == 0 {
		if err := r.readNextBlock(); err != nil {
			return 0, err
		}
	}

	n := copy(buf, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *Reader) readNextBlock() error {
	// Skip checksum
	if _, err := io.ReadFull(r.src, r.scratch[:16]); err != nil {
		if err == io.EOF {
			return io.EOF
		}
		return fmt.Errorf("cannot read checksum: %s", err)
	}

	// Read compression type
	if _, err := io.ReadFull(r.src, r.scratch[:1]); err != nil {
		return fmt.Errorf("cannot read compression type: %s", err)
	}
	compressionType := r.scratch[0]

	// Read compressed size
	compressedSize, err := r.readUint32()
	if err != nil {
		return fmt.Errorf("cannot read compressed size: %s", err)
	}
	compressedSize -= 9 // minus header length

	// Read decompressed size
	decompressedSize, err := r.readUint32()
	if err != nil {
		return fmt.Errorf("cannot read decompressed size: %s", err)
	}

	// Read compressed block
	block := make([]byte, compressedSize)
	if _, err = io.ReadFull(r.src, block); err != nil {
		return fmt.Errorf("cannot read compressed block: %s", err)
	}

	// Decompress block
	r.data = make([]byte, decompressedSize)
	switch compressionType {
	case noneType:
		r.data = block
	case lz4Type:
		if _, err := lz4.UncompressBlock(block, r.data); err != nil {
			return fmt.Errorf("cannot decompress lz4 block: %s", err)
		}
	case zstdType:
		r.data, err = zstd.Decompress(r.data, block)
		if err != nil {
			return fmt.Errorf("cannot decompress zstd block: %s", err)
		}
	default:
		return fmt.Errorf("unknown compressionType: %X", compressionType)
	}
	return nil
}

const (
	noneType = 0x02
	lz4Type  = 0x82
	zstdType = 0x90
)

func (r *Reader) readUint32() (uint32, error) {
	b := r.scratch[:4]
	_, err := io.ReadFull(r.src, b)
	if err != nil {
		return 0, err
	}
	n := uint32(b[0]) | (uint32(b[1]) << 8) | (uint32(b[2]) << 16) | (uint32(b[3]) << 24)
	return n, nil
}
