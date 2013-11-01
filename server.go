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
	Host   string
	Conn   net.Conn
	Output <-chan []byte
}

func NewConnection(conn net.Conn, host string) *Connection {
	c := new(Connection)
	c.Host = host
	c.Conn = conn
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
			if len(buf) == 0 {
				log.Println("read 0 data")
			}
			if err != nil {
				log.Println(err)
				break
			}
		}
	}(ch)

	return c
}

var (
	proxyUrl   string
	listenAddr string
	conns      = make(map[string]*Connection)
	bufferSize int
)

func init() {
	flag.StringVar(&proxyUrl, "P", "", "http proxy for forward")
	flag.StringVar(&listenAddr, "L", ":8000", "listen address")
	flag.IntVar(&bufferSize, "b", 65535, "buffer size")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println(proxyUrl, listenAddr, bufferSize)
}

func main() {
	http.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		/*
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			log.Println(string(body))
		*/
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
				log.Println(err)
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

		resp, err := request(req)
		if resp != nil {
			defer resp.Body.Close()
		}
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}

		resp.Write(w)
	})

	http.HandleFunc("/https", func(w http.ResponseWriter, r *http.Request) {
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

		timeout := time.After(time.Millisecond * 100)
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
			log.Println("send data", n)
			break
		case <-timeout:
			//log.Println(token, "timeout, no data to send")
			w.WriteHeader(http.StatusOK)
		}
	})

	log.Fatal(http.ListenAndServe(listenAddr, nil))
}

func connectProxy(proxy string) (net.Conn, error) {
	if len(proxy) > 0 {
		return net.Dial("tcp", proxy)
	}

	return nil, nil
}

func read(r io.Reader) ([]byte, error) {
	buf := make([]byte, bufferSize)
	n, err := r.Read(buf)
	//log.Println("read data", n)
	return buf[:n], err
}

func readAll(r io.Reader) ([]byte, error) {
	var rbuf [4096]byte
	buf := new(bytes.Buffer)
	for {
		n, err := r.Read(rbuf[:])
		if n > 0 {
			buf.Write(rbuf[:n])
		}
		//log.Println(n, err)
		if err != nil {
			log.Println(err)
			return buf.Bytes(), err
		}
	}
	return buf.Bytes(), nil
}

/*
func requestData(req *http.Request) (<-chan []byte, error) {
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
		reader := bufio.NewReader(proxy)
		for {
			line, err := reader.ReadString("\r\n")
			if err != nil {
				log.Println(err)
				return nil, err
			}
			log.Println(line)
			if len(line) == 0 {
				break
			}
		}

	}
}
*/
func request(req *http.Request) (*http.Response, error) {
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

		return http.ReadResponse(bufio.NewReader(proxy), req)
	}

	req.RequestURI = ""
	return http.DefaultClient.Do(req)
}
