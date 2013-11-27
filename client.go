// go-http-tunnel project client.go
package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"
)

func goClient() {
	log.Println("client connect to", serverUrl, "listen on:", listenAddr, "proxy:", proxyUrl, "buffer size:", bufferSize)
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

	data, err := read(conn)
	if err != nil {
		//log.Println(err)
		return
	}

	resp, err := request("POST", serverUrl+connectUri, bytes.NewBuffer(data))
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()

	/*
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println(err)
			return
		}

		n, err := conn.Write(data)
		if err != nil {
			log.Println(n, err)
		}
	*/

	_, err = io.Copy(conn, resp.Body)
	if err != nil {
		//log.Println(n, err)
		return
	}

	id := connId(resp)

	if id == "" {
		return
	}

	pushChan := make(chan []byte, 32)
	pollChan := make(chan []byte, 32)

	go readAll(conn, pushChan)
	go writeAll(conn, pollChan)

	transfer(id, pushChan, pollChan)
}

func connId(resp *http.Response) string {
	id := ""

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "cid" {
			id = cookie.Value
		}
	}
	return id
}

func readAll(r io.Reader, ch chan<- []byte) {
	defer close(ch)

	for {
		buf, err := read(r)
		if len(buf) > 0 {
			ch <- buf
		}
		if err != nil {
			//log.Println(len(buf), err)
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

func transfer(id string, in <-chan []byte, out chan<- []byte) {
	//defer log.Println(token, "connection closed")
	defer close(out)

	for {
		timeout := time.After(TimeoutIntvl)
		select {
		case b, ok := <-in:
			if !ok {
				request("POST", serverUrl+disconnectUri+"?id="+id, nil)
				return
			}

			//log.Println(token, "push", len(b))
			resp, err := requestData("POST", serverUrl+pollUri+"?id="+id, bytes.NewBuffer(b))
			if err != nil {
				//log.Println(err)
				return
			}
			if len(resp) > 0 {
				out <- resp
				//log.Println(token, "poll", len(resp))
			}
		case <-timeout:
			resp, err := requestData("POST", serverUrl+pollUri+"?id="+id, nil)
			if err != nil {
				//log.Println(token, err)
				return
			}
			if len(resp) > 0 {
				out <- resp
				//log.Println(token, "poll", len(resp), "timeout")
			}
		}
	}
}

func request(method string, urlStr string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}

	proxy, err := connectProxy(proxyUrl)
	if err != nil {
		//log.Println(err)
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
		//log.Println(err)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		//log.Println(err, resp.Status)
		return nil, errors.New(resp.Status)
	}

	r, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return r, nil
}
