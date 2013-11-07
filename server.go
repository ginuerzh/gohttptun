// server.go
package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

var (
	conns = make(map[string]*Connection)
)

type Connection struct {
	Host    string
	Conn    net.Conn
	Output  <-chan []byte
	Timeout time.Duration
}

func NewConnection(conn net.Conn, host string) *Connection {
	c := new(Connection)
	c.Host = host
	c.Conn = conn
	c.Timeout = TimeoutMin
	ch := make(chan []byte)
	c.Output = ch
	go func(ch chan<- []byte) {
		defer close(ch)
		defer c.Conn.Close()
		for {
			buf, err := read(c.Conn)
			if len(buf) > 0 {
				ch <- buf
			}

			if err != nil {
				//log.Println(err)
				break
			}
		}
	}(ch)

	return c
}

func goServer() {
	log.Println("server listen on", listenAddr, "proxy", proxyUrl, "buffer", bufferSize)

	http.HandleFunc(connectURI, func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		req, err := http.ReadRequest(bufio.NewReader(r.Body))
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.Method == "CONNECT" {
			//log.Println("https:", req.URL.Host)
			s, err := net.Dial("tcp", req.URL.Host)
			if err != nil {
				//log.Println(err)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			rn := rand.New(rand.NewSource(time.Now().UnixNano()))
			token := strconv.FormatInt(rn.Int63(), 10)

			conns[token] = NewConnection(s, req.URL.Host)

			cookie := &http.Cookie{Name: "token", Value: token}
			http.SetCookie(w, cookie)
			cookie = &http.Cookie{Name: "prot", Value: "https"}
			http.SetCookie(w, cookie)
			w.Write([]byte("HTTP/1.0 200 Connection established\r\nProxy-agent: go-http-tunnel\r\n\r\n"))

			return
		}

		resp, err := doRequest(req)
		if resp != nil {
			//log.Println(err)
			defer resp.Body.Close()
		}
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}

		resp.Write(w)
	})

	http.HandleFunc(httpsURI, func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		token := r.FormValue("token")
		s, ok := conns[token]
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		buf, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println(err)
		}
		if len(buf) > 0 {
			if n, err := s.Conn.Write(buf); err != nil {
				log.Println(n, err)
				w.WriteHeader(http.StatusRequestTimeout)
				delete(conns, token)
				s.Conn.Close()
				return
			}
		}

		timeout := time.After(s.Timeout)
		select {
		case b, ok := <-s.Output:
			if !ok {
				//log.Println(token, len(b), "connection closed")
				w.WriteHeader(http.StatusServiceUnavailable)
				break
			}
			n, err := w.Write(b)
			if err != nil {
				log.Println(n, err)
			}
			s.Timeout = TimeoutMin // reset timeout
			//log.Println("send data", n)
			break
		case <-timeout:
			s.Timeout *= 2 //Extend timeout
			//log.Println(token, "timeout, no data to send")
			w.WriteHeader(http.StatusOK)
		}
	})

	log.Fatal(http.ListenAndServe(listenAddr, nil))
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
