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
	bufferSize int
)

const (
	connectURI = "/connect"
	httpsURI   = "/https"
	httpURI    = "/http"
	pollURI    = "/poll"

	ProtHttp  = "http"
	ProtHttps = "https"

	ExitFlag = 1
)

func init() {
	flag.StringVar(&proxyUrl, "P", "", "http proxy for forward")
	flag.StringVar(&serverUrl, "S", "localhost:8000", "the server that client connecting to")
	flag.StringVar(&listenAddr, "L", ":8888", "listen address")
	flag.IntVar(&bufferSize, "b", 1460, "buffer size")
	flag.Parse()

	if !strings.HasPrefix(serverUrl, "http://") {
		serverUrl = "http://" + serverUrl
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println(proxyUrl, serverUrl, listenAddr, bufferSize)
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

	prot, token, resp, err := connect(conn)
	if err != nil {
		//log.Println(prot, token, err)
		return
	}

	n, err := conn.Write(resp)
	if err != nil {
		log.Println(n, err)
		return
	}

	if prot == ProtHttp {
		return
	}

	if prot == ProtHttps {
		log.Println("https:", token)

		pushChan := make(chan []byte)
		pollChan := make(chan []byte)
		exitChan := make(chan int)

		go readAll(conn, pushChan, exitChan)
		go writeAll(conn, pollChan)

		transfer(token, pushChan, pollChan, exitChan)

		return
	}
}

func connect(r io.Reader) (string, string, []byte, error) {
	token := ""
	prot := "http"
	data, err := read(r)
	if err != nil {
		//log.Println(err)
		return "", "", nil, err
	}
	//log.Println(string(r))
	resp, err := request("POST", serverUrl+connectURI, bytes.NewBuffer(data))
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		log.Println(err)
		return "", "", nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, errors.New(resp.Status)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "token" {
			token = cookie.Value
		}
		if cookie.Name == "prot" {
			prot = cookie.Value
		}
	}

	//log.Println("prot:", prot, "token:", token)

	data, err = ioutil.ReadAll(resp.Body)
	//log.Println(err)

	return prot, token, data, err
}

func readAll(r io.Reader, ch chan<- []byte, exit <-chan int) {
	defer close(ch)
	ticker := time.NewTicker(time.Millisecond * 100)

	for {
		select {
		case <-ticker.C:
			buf, err := read(r)
			if len(buf) > 0 {
				ch <- buf
			}
			if err != nil {
				log.Println(len(buf), err)
				return
			}
		case <-exit:
			return
		}
	}
}

func writeAll(w io.Writer, ch <-chan []byte) {
	for buf := range ch {
		n, err := w.Write(buf)
		if err != nil {
			log.Println(n, err)
			break
		}
	}
}

func read(r io.Reader) ([]byte, error) {
	buf := make([]byte, bufferSize)
	n, err := r.Read(buf)
	return buf[:n], err
}

func parseRequest(data []byte) (*http.Request, error) {
	return http.ReadRequest(bufio.NewReader(bytes.NewBuffer(data)))
}

func transfer(token string, in <-chan []byte, out chan<- []byte, exit chan<- int) {
	defer log.Println(token, "connection closed")
	defer close(out)

	for {
		timeout := time.After(time.Millisecond * 100)
		select {
		case b, ok := <-in:
			if !ok {
				return
			}
			log.Println(token, "push", len(b))
			resp, err := requestData("POST", serverUrl+httpsURI+"?token="+token, bytes.NewBuffer(b))
			if err != nil {
				log.Println(err)
				//exit <- ExitFlag
				return
			}
			if len(resp) > 0 {
				out <- resp
				log.Println(token, "poll", len(resp))
			}
			break
		case <-timeout:
			resp, err := requestData("POST", serverUrl+httpsURI+"?token="+token, nil)
			if err != nil {
				log.Println(token, err)
				//exit <- ExitFlag
				return
			}
			if len(resp) > 0 {
				out <- resp
				log.Println(token, "poll", len(resp), "timeout")
			}
			break
		}
	}
}

func connectProxy(proxy string) (net.Conn, error) {
	if len(proxy) > 0 {
		return net.Dial("tcp", proxy)
	}

	return nil, nil
}

func request(method string, urlStr string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}

	proxy, err := connectProxy(proxyUrl)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if proxy != nil {
		defer proxy.Close()

		//req.Header.Add("Proxy-Connection", "Keep-Alive")
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

	req.RequestURI = ""
	return http.DefaultClient.Do(req)
}

func requestData(method string, urlStr string, body io.Reader) ([]byte, error) {
	resp, err := request(method, urlStr, body)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		log.Println(err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		log.Println(err, resp.Status)
		return nil, errors.New(resp.Status)
	}

	r, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return r, nil
}
