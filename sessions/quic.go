package sessions

import (
	"context"
	"crypto/tls"
	"net"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

type quicConn struct {
	conn   quic.Connection
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
	return c.conn.LocalAddr()
}
func (c *quicConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
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

type quicConnPool struct {
	n     uint64
	conns []quic.Connection
	cnt   uint64
}

func (p *quicConnPool) Dial(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) error {
	p.conns = make([]quic.Connection, p.n)
	for i := uint64(0); i < p.n; i++ {
		conn, err := quic.DialAddr(context.Background(), addr, tlsConfig, quicConfig)
		if err != nil {
			return err
		}
		p.conns[i] = conn
	}
	return nil
}

func (p *quicConnPool) Get() quic.Connection {
	idx := atomic.AddUint64(&p.cnt, 1)
	return p.conns[idx%p.n]
}
