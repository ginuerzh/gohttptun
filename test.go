// test.go
package main

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	ln, err := net.Listen("tcp", ":9999")
	if err != nil {
		// handle error
		log.Fatal(err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
			log.Println(err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	var rbuf [1460]byte
	buf := new(bytes.Buffer)

	defer conn.Close()

	for {
		n, err := conn.Read(rbuf[:])
		//log.Println(n, err)
		if n > 0 {
			buf.Write(rbuf[:n])
		}

		if n < len(rbuf) || err == io.EOF {
			break
		}
	}
	req, _ := http.ReadRequest(bufio.NewReader(buf))
	remote, err := net.Dial("tcp", req.URL.Host)
	if err != nil {
		log.Fatal(err)
	}

	buf.Reset()
	buf.WriteString("HTTP/1.0 200 Connection established\r\nProxy-agent: go-http-tunnel\r\n\r\n")
	conn.Write(buf.Bytes())

	var disconnect = false

	go func() {
		_, err := io.Copy(remote, conn)
		if err != nil {
			disconnect = true
			return
		}
	}()

	for {
		_, err := io.Copy(conn, remote)
		if err != nil {
			disconnect = true
		}
		if disconnect {
			break
		}
	}

	/*
		for {
			buf.Reset()
			for {
				n, err := conn.Read(rbuf[:])
				log.Println(n, err)
				if n > 0 {
					buf.Write(rbuf[:n])
				}

				if n < len(rbuf) || err == io.EOF {
					break
				}
			}

			log.Println(buf.String())
			n, err := remote.Write(buf.Bytes())
			log.Println(n, err)

			buf.Reset()
			for {
				n, err := remote.Read(rbuf[:])
				if n > 0 {
					buf.Write(rbuf[:n])
				}
				if n < len(rbuf) || err == io.EOF {
					break
				}
			}
			n, _ = conn.Write(buf.Bytes())
			log.Printf("write back %d data\n%s", n, buf.String())
		}
	*/
}
