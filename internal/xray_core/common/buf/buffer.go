package buf

import (
	"io"
	"liuproxy_nexus/internal/xray_core/common/bytespool"
	"liuproxy_nexus/internal/xray_core/common/errors"
)

const (
	// Size of a regular buffer.
	Size = 8192
)

var pool = bytespool.GetPool(Size)

// Buffer is a recyclable allocation of a byte array. Buffer.Release() recycles
// the buffer into an internal buffer pool, in order to recreate a buffer more
// quickly.
type Buffer struct {
	v         []byte
	start     int32
	end       int32
	unmanaged bool
}

// New creates a Buffer with 0 length and 8K capacity.
func New() *Buffer {
	buf := pool.Get().([]byte)
	if cap(buf) >= Size {
		buf = buf[:Size]
	} else {
		buf = make([]byte, Size)
	}

	return &Buffer{
		v: buf,
	}
}

func NewExisted(b []byte) *Buffer {
	if cap(b) < Size {
		panic("Invalid buffer")
	}

	oLen := len(b)
	if oLen < Size {
		b = b[:Size]
	}

	return &Buffer{
		v:   b,
		end: int32(oLen),
	}
}

// Release recycles the buffer into an internal buffer pool.
func (b *Buffer) Release() {
	if b == nil || b.v == nil || b.unmanaged {
		return
	}

	p := b.v
	b.v = nil
	b.Clear()

	if cap(p) == Size {
		pool.Put(p)
	}
}

// Clear clears the content of the buffer, results an empty buffer with
// Len() = 0.
func (b *Buffer) Clear() {
	b.start = 0
	b.end = 0
}

// Bytes returns the content bytes of this Buffer.
func (b *Buffer) Bytes() []byte {
	return b.v[b.start:b.end]
}

// Extend increases the buffer size by n bytes, and returns the extended part.
// It panics if result size is larger than buf.Size.
func (b *Buffer) Extend(n int32) []byte {
	end := b.end + n
	if end > int32(len(b.v)) {
		panic("extending out of bound")
	}
	ext := b.v[b.end:end]
	b.end = end
	return ext
}

// BytesTo returns a slice of this Buffer from start to the given position.
func (b *Buffer) BytesTo(to int32) []byte {
	if to < 0 {
		to += b.Len()
	}
	if to < 0 {
		to = 0
	}
	return b.v[b.start : b.start+to]
}

// Check makes sure that 0 <= b.start <= b.end.
func (b *Buffer) Check() {
	if b.start < 0 {
		b.start = 0
	}
	if b.end < 0 {
		b.end = 0
	}
	if b.start > b.end {
		b.start = b.end
	}
}

// Advance cuts the buffer at the given position.
func (b *Buffer) Advance(from int32) {
	if from < 0 {
		from += b.Len()
	}
	b.start += from
	b.Check()
}

// Len returns the length of the buffer content.
func (b *Buffer) Len() int32 {
	if b == nil {
		return 0
	}
	return b.end - b.start
}

// IsEmpty returns true if the buffer is empty.
func (b *Buffer) IsEmpty() bool {
	return b.Len() == 0
}

// IsFull returns true if the buffer has no more room to grow.
func (b *Buffer) IsFull() bool {
	return b != nil && b.end == int32(len(b.v))
}

// Write implements Write method in io.Writer.
func (b *Buffer) Write(data []byte) (int, error) {
	nBytes := copy(b.v[b.end:], data)
	b.end += int32(nBytes)
	return nBytes, nil
}

// Read implements io.Reader.Read().
func (b *Buffer) Read(data []byte) (int, error) {
	if b.Len() == 0 {
		return 0, io.EOF
	}
	nBytes := copy(data, b.v[b.start:b.end])
	if int32(nBytes) == b.Len() {
		b.Clear()
	} else {
		b.start += int32(nBytes)
	}
	return nBytes, nil
}

// ReadFrom implements io.ReaderFrom.
func (b *Buffer) ReadFrom(reader io.Reader) (int64, error) {
	n, err := reader.Read(b.v[b.end:])
	b.end += int32(n)
	return int64(n), err
}

// ReadFullFrom reads exact size of bytes from given reader, or until error occurs.
func (b *Buffer) ReadFullFrom(reader io.Reader, size int32) (int64, error) {
	end := b.end + size
	if end > int32(len(b.v)) {
		v := end
		return 0, errors.NewError("out of bound: ", v)
	}
	n, err := io.ReadFull(reader, b.v[b.end:end])
	b.end += int32(n)
	return int64(n), err
}

// String returns the string form of this Buffer.
func (b *Buffer) String() string {
	return string(b.Bytes())
}
