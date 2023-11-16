package chdecompressor

import (
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
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
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		return fmt.Errorf("cannot read checksum: %w", err)
	}

	// Read compression type
	if _, err := io.ReadFull(r.src, r.scratch[:1]); err != nil {
		return fmt.Errorf("cannot read compression type: %w", err)
	}
	compressionType := r.scratch[0]

	// Read compressed size
	compressedSize, err := r.readUint32()
	if err != nil {
		return fmt.Errorf("cannot read compressed size: %w", err)
	}
	compressedSize -= 9 // minus header length

	// Read decompressed size
	decompressedSize, err := r.readUint32()
	if err != nil {
		return fmt.Errorf("cannot read decompressed size: %w", err)
	}

	// Read compressed block
	block := make([]byte, compressedSize)
	if _, err = io.ReadFull(r.src, block); err != nil {
		return fmt.Errorf("cannot read compressed block: %w", err)
	}

	// Decompress block
	if err := r.decompressBlock(block, compressionType, decompressedSize); err != nil {
		return err
	}

	return nil
}

func (r *Reader) decompressBlock(block []byte, compressionType byte, decompressedSize uint32) error {
	var err error

	r.data = make([]byte, decompressedSize)
	var decoder, _ = zstd.NewReader(nil)

	switch compressionType {
	case noneType:
		r.data = block
	case lz4Type:
		if _, err := lz4.UncompressBlock(block, r.data); err != nil {
			return fmt.Errorf("cannot decompress lz4 block: %w", err)
		}
	case zstdType:
		r.data = r.data[:0] // Wipe the slice but keep allocated memory
		r.data, err = decoder.DecodeAll(block, r.data)
		if err != nil {
			return fmt.Errorf("cannot decompress zstd block: %w", err)
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
