// server.go
package main

import (
	"bufio"
	"bytes"
	"github.com/codegangsta/martini"
	"github.com/dchest/uniuri"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Connection struct {
	Host      string
	Conn      net.Conn
	chanClose chan interface{}
	Output    <-chan []byte
	Timeout   time.Duration
}

type ConnPool struct {
	sync.RWMutex
	m map[string]*Connection
}

var (
	connPool *ConnPool = NewConnPool(poolSize)
)

func NewConnPool(poolSize int) *ConnPool {
	return &ConnPool{m: make(map[string]*Connection, poolSize)}
}

func (pool *ConnPool) Get(id string) *Connection {
	pool.RLock()
	defer pool.RUnlock()

	return pool.m[id]
}

func (pool *ConnPool) Add(id string, conn *Connection) {
	pool.Lock()
	defer pool.Unlock()

	pool.m[id] = conn
}

func (pool *ConnPool) Remove(id string) *Connection {
	pool.Lock()
	defer pool.Unlock()

	conn := pool.m[id]
	if conn != nil {
		conn.chanClose <- 1
	}
	delete(pool.m, id)

	return conn
}

func NewConnection(conn net.Conn, host string) *Connection {
	c := new(Connection)
	c.Host = host
	c.Conn = conn
	c.Timeout = TimeoutMin
	c.chanClose = make(chan interface{}, 1)
	ch := make(chan []byte, 32)
	c.Output = ch
	go func(ch chan<- []byte) {
		defer close(ch)
		defer c.Conn.Close()

		ticker := time.NewTicker(1 * time.Millisecond)

		for {
			select {
			case <-c.chanClose:
				return
			case <-ticker.C:
				buf, err := read(c.Conn)
				if len(buf) > 0 {
					ch <- buf
				}

				if err != nil {
					//log.Println(err)
					return
				}
			}
		}
	}(ch)

	return c
}

func connectHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	req, err := http.ReadRequest(bufio.NewReader(r.Body))
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.Method == "CONNECT" {
		s, err := net.Dial("tcp", req.URL.Host)
		if err != nil {
			//log.Println(err)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		id := uniuri.New()
		connPool.Add(id, NewConnection(s, req.URL.Host))

		cookie := &http.Cookie{Name: "cid", Value: id}
		http.SetCookie(w, cookie)
		w.Write([]byte("HTTP/1.0 200 Connection established\r\nProxy-agent: go-http-tunnel\r\n\r\n"))

		return
	}

	resp, err := doRequest(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	resp.Write(w)
}

func disconnectHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	id := r.FormValue("id")

	connPool.Remove(id)

	w.WriteHeader(http.StatusOK)
}

func pollHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	id := r.FormValue("id")
	conn := connPool.Get(id)
	if conn == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Println(err)
	}
	if len(buf) > 0 {
		if n, err := conn.Conn.Write(buf); err != nil {
			log.Println(n, err)
			w.WriteHeader(http.StatusNotAcceptable)
			connPool.Remove(id)

			return
		}
	}

	timeout := time.After(conn.Timeout)
	select {
	case b, ok := <-conn.Output:
		if !ok {
			//log.Println(token, len(b), "connection closed")
			w.WriteHeader(http.StatusGone)
			break
		}
		n, err := w.Write(b)
		if err != nil {
			log.Println(n, err)
		}
		conn.Timeout = TimeoutMin // reset timeout
		//log.Println("send data", n)
		break
	case <-timeout:
		conn.Timeout *= 2 //Extend timeout
		//log.Println(token, "timeout, no data to send")
		w.WriteHeader(http.StatusOK)
	}
}

func goServer() {
	log.Println("server listen on", listenAddr, "proxy", proxyUrl, "buffer", bufferSize)

	m := martini.Classic()

	m.Post("/connect", connectHandler)
	m.Post("/poll", pollHandler)
	m.Post("/disconnect", disconnectHandler)

	http.ListenAndServe(listenAddr, m)
}

func doRequest(req *http.Request) (*http.Response, error) {
	proxy, err := connectProxy(proxyUrl)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	if proxy != nil {
		defer proxy.Close()

		if err := req.WriteProxy(proxy); err != nil {
			log.Println(err)
			return nil, err
		}

		r, err := ioutil.ReadAll(proxy)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		return http.ReadResponse(bufio.NewReader(bytes.NewBuffer(r)), req)
	}

	req.Header.Del("Proxy-Connection")
	req.RequestURI = ""
	return http.DefaultClient.Do(req)
}
