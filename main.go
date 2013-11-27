// main
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

var (
	proxyUrl   string
	serverUrl  string
	listenAddr string
	bufferSize int
	isClient   bool
	poolSize   int = 32
	serverAuth bool
	authUser   string
	authPass   string

	TimeoutIntvl = TimeoutMin
)

const (
	connectUri    = "/connect"
	disconnectUri = "/disconnect"
	pollUri       = "/poll"

	connClosed    = "closed"
	connKeepAlive = "alive"

	TimeoutMin = time.Millisecond * 10
)

func init() {
	flag.StringVar(&proxyUrl, "P", "", "http proxy for forward")
	flag.StringVar(&serverUrl, "S", "localhost:8080", "the server that client connecting to")
	flag.StringVar(&listenAddr, "L", ":8888", "listen address")
	flag.IntVar(&bufferSize, "b", 8192, "buffer size")
	flag.BoolVar(&isClient, "c", false, "client")
	flag.BoolVar(&serverAuth, "auth", false, "server need auth")
	flag.StringVar(&authUser, "user", "", "username for server auth")
	flag.StringVar(&authPass, "pass", "", "password for server auth")
	flag.Parse()

	if !strings.HasPrefix(serverUrl, "http://") {
		serverUrl = "http://" + serverUrl
	}

	if !strings.HasPrefix(listenAddr, ":") {
		listenAddr = ":" + listenAddr
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	if isClient {
		goClient()
	} else {
		goServer()
	}

}

func read(r io.Reader) ([]byte, error) {
	buf := make([]byte, bufferSize)
	n, err := r.Read(buf)
	//log.Println("read data", n)
	return buf[:n], err
}

func connectProxy(proxy string) (net.Conn, error) {
	if len(proxy) > 0 {
		return net.Dial("tcp", proxy)
	}

	return nil, nil
}
