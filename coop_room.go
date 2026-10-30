package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Rooms live on the relay (a Durable Object per invite code). The relay only
// forwards messages: the host's game runs the case, and each guest gets a
// virtual connection into coopServer.serve.
//
// Host <-> relay frames: {"c": guestID, "open": true} | {"c", "d": msg} | {"c", "close": true}
// The relay also sends {"room": "ABC123"} to the host once the room exists.

type roomFrame struct {
	Room  string          `json:"room,omitempty"`
	C     string          `json:"c,omitempty"`
	D     json.RawMessage `json:"d,omitempty"`
	Open  bool            `json:"open,omitempty"`
	Close bool            `json:"close,omitempty"`
}

var errBadAccessCode = errors.New("접속 코드가 올바르지 않습니다")

func roomURL(path string) (string, error) {
	u, err := url.Parse(relayEndpoint())
	if err != nil || u.Host == "" {
		return "", errors.New("중계 서버 주소가 없습니다")
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}

func dialRoom(path string, header http.Header) (*websocket.Conn, error) {
	u, err := roomURL(path)
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case 401:
				return nil, errBadAccessCode
			case 404:
				return nil, errors.New("그런 방이 없습니다. 초대 코드를 확인하세요")
			case 429:
				return nil, errors.New("방이 꽉 찼습니다")
			}
		}
		return nil, fmt.Errorf("중계 서버에 연결할 수 없습니다: %w", err)
	}
	c.SetReadLimit(1 << 20)
	return c, nil
}

// hostRoom opens a room on the relay and bridges guests into srv.
// It returns the invite code and a stop function.
func hostRoom(srv *coopServer) (string, func(), error) {
	ws, err := dialRoom("/rooms/new", http.Header{"x-teamtalk-token": {cfg.AccessCode}})
	if err != nil {
		return "", nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, data, err := ws.Read(rctx)
	var first roomFrame
	if err != nil || json.Unmarshal(data, &first) != nil || first.Room == "" {
		ws.CloseNow()
		return "", nil, errors.New("방을 만들지 못했습니다")
	}

	type guest struct {
		conn net.Conn
		in   chan []byte // guest -> server, in order
	}
	var mu sync.Mutex
	guests := map[string]*guest{}
	send := func(f roomFrame) {
		b, _ := json.Marshal(f)
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		ws.Write(wctx, websocket.MessageText, b)
	}
	go func() {
		defer func() {
			mu.Lock()
			for _, g := range guests {
				g.conn.Close()
			}
			mu.Unlock()
		}()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var f roomFrame
			if json.Unmarshal(data, &f) != nil || f.C == "" {
				continue
			}
			mu.Lock()
			g := guests[f.C]
			switch {
			case f.Open && g == nil:
				ours, theirs := net.Pipe()
				g = &guest{conn: ours, in: make(chan []byte, 64)}
				guests[f.C] = g
				go srv.serve(theirs)
				go func(g *guest) {
					for b := range g.in {
						if _, err := g.conn.Write(b); err != nil {
							return
						}
					}
				}(g)
				go func(id string, c net.Conn) { // server -> guest
					sc := bufio.NewScanner(c)
					sc.Buffer(make([]byte, 64*1024), 1<<20)
					for sc.Scan() {
						send(roomFrame{C: id, D: append(json.RawMessage(nil), sc.Bytes()...)})
					}
					send(roomFrame{C: id, Close: true})
					mu.Lock()
					if g := guests[id]; g != nil {
						close(g.in)
						delete(guests, id)
					}
					mu.Unlock()
				}(f.C, ours)
			case f.Close && g != nil:
				g.conn.Close()
			case len(f.D) > 0 && g != nil:
				select {
				case g.in <- append(f.D, '\n'):
				default: // guest is flooding; drop the message
				}
			}
			mu.Unlock()
		}
	}()
	return first.Room, func() { ws.Close(websocket.StatusNormalClosure, "host left") }, nil
}

// joinRoom connects a guest to a room. Each relay message is one JSON line.
func joinRoom(code string) (net.Conn, error) {
	ws, err := dialRoom("/rooms/"+url.PathEscape(code), nil)
	if err != nil {
		return nil, err
	}
	return websocket.NetConn(ctx, ws, websocket.MessageText), nil
}
