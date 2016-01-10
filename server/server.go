package server

import (
	"net"
	"crypto/tls"
	"time"
	"log"
	"os"
	"encoding/gob"
	"github.com/transhift/common/protocol"
)

const LogFlags = log.Ldate | log.Ltime | log.LUTC | log.Lshortfile

var logger = log.New(os.Stdout, "", LogFlags)

type server struct {
	host  string
	port  string
	idLen uint
}

type client struct {
	net.Conn

	server *server
	logger *log.Logger
	enc    *gob.Encoder
	dec    *gob.Decoder
}

func New(host, port string, idLen uint) *server {
	return &server{host, port, idLen}
}

func (s server) Start(cert *tls.Certificate) error {
	const KeepAlivePeriod = time.Second * 30

	tlsConfig := tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	laddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(s.host, s.port))

	if err != nil {
		return err
	}

	listener, err := net.ListenTCP("tcp", laddr)

	if err != nil {
		return err
	}

	go func() {
		for {
			tcpConn, err := listener.AcceptTCP()

			if err != nil {
				logger.Println("error:", err)
				continue
			}

			tlsConn := tls.Server(tcpConn, &tlsConfig)
			c := client{
				Conn: tlsConn,
				server: &s,
				logger: log.New(os.Stdout, tcpConn.RemoteAddr().String(), LogFlags),
			}

			if err := tcpConn.SetKeepAlive(true); err != nil {
				c.logger.Println("error:", err)
				continue
			}

			if err := tcpConn.SetKeepAlivePeriod(KeepAlivePeriod); err != nil {
				c.logger.Println("error:", err)
				continue
			}

			go c.handle()
		}
	}()

	return nil
}

func (c client) handle() {
	defer c.Conn.Close()

	c.enc = gob.NewEncoder(c.Conn)
	c.dec = gob.NewDecoder(c.Conn)

	// Expect client type.
	var clientType protocol.ClientType
	err := c.dec.Decode(&clientType)

	if err != nil {
		c.logger.Println("error:", err)
		return
	}

	switch clientType {
	case protocol.TargetClient:
		c.handleTarget()
	case protocol.SourceClient:
		c.handleSource()
	default:
		c.logger.Printf("error: unknown client type 0x%x\n", clientType)
	}
}
