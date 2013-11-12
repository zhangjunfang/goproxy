package msocks

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sutils"
)

type MsocksService struct {
	userpass map[string]string
	dialer   sutils.Dialer
	sess     *Session
}

func LoadPassfile(filename string) (userpass map[string]string, err error) {
	logger.Infof("load passfile from file %s.", filename)

	file, err := os.Open(filename)
	if err != nil {
		logger.Err(err)
		return
	}
	defer file.Close()
	userpass = make(map[string]string, 0)

	reader := bufio.NewReader(file)
QUIT:
	for {
		line, err := reader.ReadString('\n')
		switch err {
		case io.EOF:
			if len(line) == 0 {
				break QUIT
			}
		case nil:
		default:
			return nil, err
		}
		f := strings.SplitN(line, ":", 2)
		if len(f) < 2 {
			err = fmt.Errorf("format wrong: %s", line)
			logger.Err(err)
			return nil, err
		}
		userpass[strings.Trim(f[0], "\r\n ")] = strings.Trim(f[1], "\r\n ")
	}

	logger.Infof("userinfo loaded %d record(s).", len(userpass))
	return
}

func NewService(passfile string, dialer sutils.Dialer) (ms *MsocksService, err error) {
	if dialer == nil {
		err = errors.New("empty dialer")
		logger.Err(err)
		return
	}
	ms = &MsocksService{dialer: dialer}

	if passfile == "" {
		return ms, nil
	}
	ms.userpass, err = LoadPassfile(passfile)
	return
}

func (ms *MsocksService) on_conn(network, address string, streamid uint16) (s Stream, err error) {
	conn, err := ms.dialer.Dial("tcp", address)
	if err != nil {
		logger.Err(err)
		return
	}

	ss := &ServiceStream{
		streamid: streamid,
		sess:     ms.sess,
		closed:   false,
	}
	go func() {
		sutils.CopyLink(conn, ss)
	}()
	return ss, nil
}

func (ms *MsocksService) on_auth(stream io.ReadWriteCloser) bool {
	f, err := ReadFrame(stream)
	if err != nil {
		logger.Err(err)
		return false
	}

	switch ft := f.(type) {
	default:
		logger.Err("unexpected package type")
		return false
	case *FrameAuth:
		logger.Debugf("auth with username: %s, password: %s.", ft.username, ft.password)
		if ms.userpass != nil {
			password1, ok := ms.userpass[ft.username]
			if !ok || (ft.password != password1) {
				SendFAILEDFrame(stream, ft.streamid, ERR_AUTH)
				logger.Err("failed with auth")
				return false
			}
		}
		err = SendOKFrame(stream, ft.streamid)
		if err != nil {
			logger.Err(err)
			return false
		}

		logger.Infof("auth passed with username: %s, password: %s.",
			ft.username, ft.password)
	}

	return true
}

func (ms *MsocksService) Handler(conn net.Conn) {
	logger.Debugf("connection come from: %s => %s",
		conn.RemoteAddr(), conn.LocalAddr())

	if !ms.on_auth(conn) {
		conn.Close()
		return
	}

	ms.sess = NewSession()
	ms.sess.conn = conn
	ms.sess.on_conn = ms.on_conn
	ms.sess.Run()
}

func (ms *MsocksService) ServeTCP(listener net.Listener) (err error) {
	var conn net.Conn

	for {
		conn, err = listener.Accept()
		if err != nil {
			logger.Err(err)
			return
		}
		go func() {
			defer conn.Close()
			ms.Handler(conn)
		}()
	}
	return
}