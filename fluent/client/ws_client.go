package client

import (
	"errors"
	"net/http"
	"sync"

	"github.com/IBM/fluent-forward-go/fluent/client/ws"
	"github.com/IBM/fluent-forward-go/fluent/client/ws/ext"
	"github.com/gorilla/websocket"

	"github.com/tinylib/msgp/msgp"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate

const (
	AuthorizationHeader = "Authorization"
)

type IAMAuthInfo struct {
	token string
	mutex sync.RWMutex
}

// IAMToken returns the current token value. It is thread safe.
func (ai *IAMAuthInfo) IAMToken() string {
	ai.mutex.RLock()
	defer ai.mutex.RUnlock()

	return ai.token
}

// SetIAMToken updates the token returned by IAMToken(). It is thread safe.
func (ai *IAMAuthInfo) SetIAMToken(token string) {
	ai.mutex.Lock()
	defer ai.mutex.Unlock()

	ai.token = token
}

func NewIAMAuthInfo(token string) *IAMAuthInfo {
	return &IAMAuthInfo{token: token}
}

// WSSession represents a single websocket connection.
type WSSession struct {
	ServerAddress
	Connection ws.Connection
}

//counterfeiter:generate . WSConnectionFactory
type WSConnectionFactory interface {
	New() (ext.Conn, error)
}

// DefaultWSConnectionFactory is used by the client if no other
// ConnectionFactory is provided.
type DefaultWSConnectionFactory struct {
	ServerAddress
	AuthInfo *IAMAuthInfo
}

func (wcf *DefaultWSConnectionFactory) New() (ext.Conn, error) {
	var (
		dialer websocket.Dialer
		header http.Header
	)

	if wcf.AuthInfo != nil && len(wcf.AuthInfo.IAMToken()) > 0 {
		header.Add(AuthorizationHeader, wcf.AuthInfo.IAMToken())
	}

	conn, _, err := dialer.Dial(wcf.ServerAddress.String(), header)
	// TODO: dump response, which is second return value from Dial

	return conn, err
}

// WSClient manages the lifetime of a single websocket connection.
type WSClient struct {
	ConnectionFactory WSConnectionFactory
	ServerAddress
	AuthInfo          *IAMAuthInfo
	ConnectionOptions ws.ConnectionOptions
	Session           *WSSession
}

// Connect initializes the Session and Connection objects by opening
// a websocket connection. If AuthInfo is not nil, the token it returns
// will be passed via the "Authentication" header during the initial
// HTTP call.
func (c *WSClient) Connect() error {
	if c.ConnectionFactory == nil {
		c.ConnectionFactory = &DefaultWSConnectionFactory{
			ServerAddress: c.ServerAddress,
			AuthInfo:      c.AuthInfo,
		}
	}

	conn, err := c.ConnectionFactory.New()
	if err != nil {
		return err
	}

	connection, err := ws.NewConnection(conn, c.ConnectionOptions)
	if err != nil {
		return err
	}

	c.Session = &WSSession{
		ServerAddress: c.ServerAddress,
		Connection:    connection,
	}

	return nil
}

// Disconnect ends the current Session and terminates its websocket connection.
func (c *WSClient) Disconnect() (err error) {
	if c.Session != nil {
		err = c.Session.Connection.Close()
	}

	c.Session = nil

	return
}

// Reconnect terminates the existing Session and creates a new one.
func (c *WSClient) Reconnect() (err error) {
	if err = c.Disconnect(); err == nil {
		err = c.Connect()
	}

	return
}

// SendMessage sends a single msgp.Encodable across the wire.
func (c *WSClient) SendMessage(e msgp.Encodable) error {
	if c.Session == nil {
		return errors.New("No active session")
	}

	// msgp.Encode makes use of object pool to decrease allocations
	return msgp.Encode(c.Session.Connection, e)
}

// Listen starts a read loop on the Session's websocket connection. It blocks until the Session
// is closed.
func (c *WSClient) Listen() error {
	if c.Session == nil || c.Session.Connection == nil {
		return errors.New("No active session")
	}

	return c.Session.Connection.Listen()
}