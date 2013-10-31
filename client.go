// go-http-tunnel project client.go
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

var (
	proxyUrl   string
	serverUrl  string
	listenAddr string
)

const (
	BufferSize = 1460
	connectURI = "/connect"
	httpsURI   = "/https"
	httpURI    = "/http"
	pollURI    = "/poll"

	ProtHttp  = "http"
	ProtHttps = "https"
)

func init() {
	flag.StringVar(&proxyUrl, "P", "", "http proxy for forward")
	flag.StringVar(&serverUrl, "S", "localhost:8000", "the server that client connecting to")
	flag.StringVar(&listenAddr, "L", ":8888", "listen address")
	flag.Parse()

	if !strings.HasPrefix(serverUrl, "http://") {
		serverUrl = "http://" + serverUrl
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println(proxyUrl, serverUrl)
}

func main() {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatal(err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Println(err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	proxy, err := connectProxy(proxyUrl)
	if err != nil {
		log.Println(err)
		resp := makeResponse(http.StatusBadGateway)
		resp.Write(conn)
		return
	}
	if proxy != nil {
		defer proxy.Close()
	}

	prot, token, resp, err := connect(conn, proxy)
	if err != nil {
		//log.Println(prot, token, err)
		return
	}
	if prot == ProtHttp {
		n, err := conn.Write(resp)
		if err != nil {
			log.Println(n, err)
		}
		return
	}

	if prot == ProtHttps {

		conn.Write([]byte("HTTP/1.0 200 Connection established\r\nProxy-agent: go-http-tunnel\r\n\r\n"))

		statusChan := make(chan int64)

		go transferUp(serverUrl+httpsURI+"?token="+token, conn, proxy, statusChan)
		go transferDown(serverUrl+pollURI+"?token="+token, conn, proxy, statusChan)

		for status := range statusChan {
			//log.Println(status)
			if status < 0 {
				break
			}
		}

		return
	}
}

func connect(conn net.Conn, proxy net.Conn) (string, string, []byte, error) {
	token := ""
	prot := "http"
	r, err := read(conn)
	if err != nil {
		//log.Println(err)
		return "", "", nil, err
	}

	resp, err := request("POST", serverUrl+connectURI, bytes.NewBuffer(r), proxy)
	if err != nil {
		log.Println(err)
		return "", "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, errors.New(http.StatusText(resp.StatusCode))
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "token" {
			token = cookie.Value
		}
		if cookie.Name == "prot" {
			prot = cookie.Value
		}
	}

	log.Println("prot:", prot, "token:", token)

	r, err = ioutil.ReadAll(resp.Body)
	//log.Println(string(r))

	return prot, token, r, err
}

func read(r io.Reader) ([]byte, error) {
	buf := make([]byte, BufferSize)
	n, err := r.Read(buf)
	return buf[:n], err
}

func readAll(r io.Reader) ([]byte, error) {
	rbuf := make([]byte, BufferSize)
	buf := new(bytes.Buffer)

	for {
		n, err := r.Read(rbuf)
		log.Println(n, err)
		if n > 0 {
			buf.Write(rbuf[:n])
		}
		if n < BufferSize || err != nil {
			//log.Println(n, err)
			break
		}
	}

	return buf.Bytes(), nil
}

func write(w io.Writer, data []byte) (int64, error) {
	r := bytes.NewBuffer(data)
	return io.Copy(w, r)
}

func parseRequest(data []byte) (*http.Request, error) {
	return http.ReadRequest(bufio.NewReader(bytes.NewBuffer(data)))
}

/*
func getToken(proxy net.Conn) (string, error) {
	resp, err := request("POST", serverUrl+pollURI, nil, proxy)
	if err != nil {
		log.Println(err)
		return "", err
	}

	body, err := read(resp.Body)
	if err != nil {
		log.Println(err)
		return "", err
	}

	return string(body), nil
}
*/

func transferUp(urlStr string, client io.ReadWriter, proxy io.ReadWriter, status chan<- int64) {
	for {
		buf, err := read(client)
		if len(buf) > 0 {
			//log.Println(urlStr, "send data", len(buf))
			resp, err := request("POST", urlStr, bytes.NewBuffer(buf), proxy)
			if err != nil {
				log.Println(err)
				break
			}
			//log.Println("resp content length:", resp.ContentLength)
			if resp.ContentLength > 0 {
				n, err := io.Copy(client, resp.Body)
				if err != nil {
					log.Println(n, err)
					resp.Body.Close()
					break
				}
			}
			resp.Body.Close()
		}
		if err != nil {
			//log.Println(len(buf), err)
			break
		}
		time.Sleep(time.Millisecond * 500)
	}

	status <- -1
}

func transferDown(urlStr string, client io.ReadWriter, proxy io.ReadWriter, status chan<- int64) {
	for {
		resp, err := request("POST", urlStr, nil, proxy)
		if err != nil {
			log.Println(err)
			break
		}
		if resp.StatusCode != http.StatusOK {
			break
		}
		n, err := io.Copy(client, resp.Body)
		if err != nil {
			log.Println(n, err)
			resp.Body.Close()
			break
		}
		time.Sleep(time.Millisecond * 500)
		resp.Body.Close()
	}
	status <- -2
}

/*
func transferDown(r io.Reader, w io.Writer, status chan<- int64) {
	for {
		buf, err := read(r)
		if len(buf) > 0 {
			resp, err := http.ReadResponse(bufio.NewReader(bytes.NewBuffer(buf)), nil)
			if err != nil {
				log.Println(err)
				break
			}
			n, err := io.Copy(w, resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Println(n, err)
				break
			}
		}
		if err != nil {
			log.Println(len(buf), err)
			break
		}
	}
	status <- -2
}
*/
func readFromClient(r io.Reader, ch chan<- []byte, statusChan chan<- int64) {
	for {
		buf := make([]byte, BufferSize)
		n, err := r.Read(buf)
		//log.Println("Read from client", n)
		if n > 0 {
			ch <- buf[:n]
		}
		if err != nil {
			//log.Println(err)
			break
		}
	}
	close(ch)
	statusChan <- -1
	//log.Println("readFromClient exit")
}

func write2Client(w io.Writer, ch <-chan []byte, statusChan chan<- int64) {
	for buf := range ch {
		//log.Println(string(buf))
		n, err := w.Write(buf)
		if err != nil {
			log.Println(n, err)
			continue
		}
	}
	statusChan <- -3
	//log.Println("write2Client exit")
}

/*
func sendReadBack(reqChan <-chan []byte, respChan chan<- []byte, statusChan chan<- int64, proxy io.ReadWriter) {
	for buf := range reqChan {
		//log.Println(string(buf))
		resp, err := request("POST", serverUrl+httpURI, bytes.NewBuffer(buf), proxy)
		if err != nil {
			log.Println(resp, err)
			continue
		}

		rbuf, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()

		if len(rbuf) > 0 {
			respChan <- rbuf
			//log.Println(string(rbuf))
			statusChan <- int64(len(rbuf))
		}
		if err != nil {
			log.Println(err)
			continue
		}
	}
	close(respChan)
	statusChan <- -2
	//log.Println("sendReadBack exit")
}
*/
func makeResponse(code int) *http.Response {
	resp := new(http.Response)
	resp.StatusCode = code
	resp.Status = http.StatusText(code)
	resp.Proto = "HTTP/1.0"
	resp.ProtoMajor = 1
	resp.ProtoMinor = 0

	return resp
}

func connectProxy(proxy string) (net.Conn, error) {
	if len(proxy) > 0 {
		return net.Dial("tcp", proxy)
	}

	return nil, nil
}

func request(method string, urlStr string, body io.Reader, proxy io.ReadWriter) (*http.Response, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}

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
