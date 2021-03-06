package transporter

import (
	"errors"
	"io"
	"net"
	"syscall"

	"go.uber.org/atomic"

	"github.com/Ehco1996/ehco/internal/constant"
	"github.com/Ehco1996/ehco/internal/web"
)

// 全局pool
var BufferPool *BytePool

func init() {
	BufferPool = NewBytePool(constant.BUFFER_POOL_SIZE, constant.BUFFER_SIZE)
}

// BytePool implements a leaky pool of []byte in the form of a bounded channel
type BytePool struct {
	c    chan []byte
	size int
}

// NewBytePool creates a new BytePool bounded to the given maxSize, with new
// byte arrays sized based on width.
func NewBytePool(maxSize int, size int) (bp *BytePool) {
	return &BytePool{
		c:    make(chan []byte, maxSize),
		size: size,
	}
}

// Get gets a []byte from the BytePool, or creates a new one if none are
// available in the pool.
func (bp *BytePool) Get() (b []byte) {
	select {
	case b = <-bp.c:
	// reuse existing buffer
	default:
		// create new buffer
		b = make([]byte, bp.size)
	}
	return
}

// Put returns the given Buffer to the BytePool.
func (bp *BytePool) Put(b []byte) {
	select {
	case bp.c <- b:
		// buffer went back into pool
	default:
		// buffer didn't go back into pool, just discard
	}
}

func transport(rw1, rw2 io.ReadWriter, remote string) error {
	errc := make(chan error, 2)

	go func() {
		buf := BufferPool.Get()
		defer BufferPool.Put(buf)
		wt, err := io.CopyBuffer(rw1, rw2, buf)
		web.NetWorkTransmitBytes.WithLabelValues(remote).Add(float64(wt * 2))
		errc <- err
	}()

	go func() {
		buf := BufferPool.Get()
		defer BufferPool.Put(buf)
		wt, err := io.CopyBuffer(rw2, rw1, buf)
		web.NetWorkTransmitBytes.WithLabelValues(remote).Add(float64(wt * 2))
		errc <- err
	}()

	err := <-errc
	// NOTE 我们不关心operror 比如 eof/reset/broken pipe
	if err != nil {
		if err == io.EOF || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
			err = nil
		}
		if _, ok := err.(*net.OpError); ok {
			err = nil
		}
	}
	return err
}

type BufferCh struct {
	Ch      chan []byte
	Handled atomic.Bool
	UDPAddr *net.UDPAddr
}

func newudpBufferCh(clientUDPAddr *net.UDPAddr) *BufferCh {
	return &BufferCh{
		Ch:      make(chan []byte, 100),
		Handled: atomic.Bool{},
		UDPAddr: clientUDPAddr,
	}
}
