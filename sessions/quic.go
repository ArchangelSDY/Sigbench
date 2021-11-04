package sessions

import (
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type quicConn struct {
	sess   quic.Session
	stream quic.Stream
}

func (c *quicConn) Read(b []byte) (int, error) {
	return c.stream.Read(b)
}
func (c *quicConn) Write(b []byte) (int, error) {
	return c.stream.Write(b)
}
func (c *quicConn) Close() error {
	return c.stream.Close()
}
func (c *quicConn) LocalAddr() net.Addr {
	return c.sess.LocalAddr()
}
func (c *quicConn) RemoteAddr() net.Addr {
	return c.sess.RemoteAddr()
}
func (c *quicConn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}
func (c *quicConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}
func (c *quicConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

type quicSessionPool struct {
	n      uint64
	sesses []quic.Session
	cnt    uint64
}

func (p *quicSessionPool) Dial(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) error {
	p.sesses = make([]quic.Session, p.n)
	for i := uint64(0); i < p.n; i++ {
		quicSess, err := quic.DialAddr(addr, tlsConfig, quicConfig)
		if err != nil {
			return err
		}
		p.sesses[i] = quicSess
	}
	return nil
}

func (p *quicSessionPool) Get() quic.Session {
	idx := atomic.AddUint64(&p.cnt, 1)
	return p.sesses[idx%p.n]
}
