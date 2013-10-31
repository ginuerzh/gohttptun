// server.go
package main

import (
	"bufio"
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Connection struct {
	Host string
	Conn net.Conn
}

const (
	BufferSize = 1460
)

var (
	proxyUrl   string
	listenAddr string
	conns      = make(map[string]*Connection)
)

func init() {
	flag.StringVar(&proxyUrl, "P", "", "http proxy for forward")
	flag.StringVar(&listenAddr, "L", ":8000", "listen address")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("proxy:", proxyUrl)
}

func main() {
	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		proxy, err := connectProxy(proxyUrl)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		buf, _ := ioutil.ReadAll(r.Body)

		for i, _ := range buf {
			buf[i] -= 1
		}

		req, err := http.ReadRequest(bufio.NewReader(bytes.NewBuffer(buf)))
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if req.Method == "CONNECT" {
			log.Println("https:", req.URL.Host)
			s, err := net.Dial("tcp", req.URL.Host)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			rn := rand.New(rand.NewSource(time.Now().UnixNano()))
			token := strconv.FormatInt(rn.Int63(), 10)

			conns[token] = &Connection{Host: req.URL.Host, Conn: s}

			cookie := &http.Cookie{Name: "token", Value: token}
			http.SetCookie(w, cookie)
			cookie = &http.Cookie{Name: "prot", Value: "abc"}
			http.SetCookie(w, cookie)
			/*
				b := []byte("HTTP/1.0 200 Connection established\r\nProxy-agent: go-http-tunnel\r\n\r\n")
				for i, _ := range b {
					b[i] += 1
				}
			*/
			w.WriteHeader(http.StatusOK)

			return
		}

		resp, err := request(req, proxy)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()
		io.Copy(w, resp.Body)
	})

	http.HandleFunc("/https", func(w http.ResponseWriter, r *http.Request) {
		//log.Println(r.RequestURI, r.URL, r.FormValue("token"))
		defer r.Body.Close()

		token := r.FormValue("token")
		s, ok := conns[token]
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		n, err := io.Copy(s.Conn, r.Body)
		if err != nil {
			log.Println(n, err)
			w.WriteHeader(http.StatusServiceUnavailable)
			delete(conns, token)
			s.Conn.Close()
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/poll", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		token := r.FormValue("token")
		s, ok := conns[token]
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		buf, err := read(s.Conn)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusServiceUnavailable)
			delete(conns, token)
			s.Conn.Close()
			return
		}

		for i, _ := range buf {
			buf[i] += 1
		}

		w.Write(buf)
	})

	log.Println("listen on", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func connectProxy(proxy string) (net.Conn, error) {
	if len(proxy) > 0 {
		return net.Dial("tcp", proxy)
	}

	return nil, nil
}

func read(r io.Reader) ([]byte, error) {
	buf := make([]byte, BufferSize)
	n, err := r.Read(buf)
	return buf[:n], err
}

func request(req *http.Request, proxy net.Conn) (*http.Response, error) {
	if proxy != nil {
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

	client := new(http.Client)
	return client.Do(req)
}
