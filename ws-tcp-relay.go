package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"crypto/tls"
	"net/http"
	"os"

	"golang.org/x/net/websocket"
)

var targetAddress string
var binaryMode bool
var wsAddress string
var certFile string
var keyFile string
var tlsEnable bool
var subprotocols string

func copyWorker(dst io.Writer, src io.Reader, doneCh chan<- bool) {
	io.Copy(dst, src)
	doneCh <- true
}

func wsRelayHandler(ws *websocket.Conn) {
	conn, err := net.Dial("tcp", targetAddress)
	if err != nil {
		log.Printf("[ERROR] %v\n", err)
		ws.Close()
		return
	}

	protocol := ws.Config().Protocol
	if protocol != nil && protocol[0] != subprotocols {
		log.Printf("[ERROR] invalid '%v'\n", subprotocols)
		ws.Close()
		conn.Close()
		return
	}

	if binaryMode {
		ws.PayloadType = websocket.BinaryFrame
	}

	doneCh := make(chan bool)

	go copyWorker(conn, ws, doneCh)
	go copyWorker(ws, conn, doneCh)

	<-doneCh
	conn.Close()
	ws.Close()
	<-doneCh
}

func tcpRelayHandle(conn net.Conn) {
	var wsAddr string
	var origAddr string
	if tlsEnable {
		wsAddr = fmt.Sprintf("wss://%s", targetAddress)
		origAddr = fmt.Sprintf("https://%s", targetAddress)
	} else {
		wsAddr = fmt.Sprintf("ws://%s", targetAddress)
		origAddr = fmt.Sprintf("http://%s", targetAddress)
	}

	config, err := websocket.NewConfig(wsAddr, origAddr)
	if err != nil {
		log.Printf("[ERROR] %v\n", err)
		conn.Close()
		return
	}
	config.TlsConfig = &tls.Config{InsecureSkipVerify: true}
	config.Protocol = []string{subprotocols}
	ws, err := websocket.DialConfig(config)
	if err != nil {
		log.Printf("[ERROR] %v\n", err)
		conn.Close()
		return
	}
	if binaryMode {
		ws.PayloadType = websocket.BinaryFrame
	}

	doneCh := make(chan bool)

	go copyWorker(conn, ws, doneCh)
	go copyWorker(ws, conn, doneCh)

	<-doneCh
	conn.Close()
	ws.Close()
	<-doneCh
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <tcpTargetAddress>\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	var port uint
	var reverse bool

	flag.UintVar(&port, "p", 4223, "The port to listen on")
	flag.UintVar(&port, "port", 4223, "The port to listen on")
	flag.StringVar(&certFile, "tlscert", "", "TLS cert file path")
	flag.StringVar(&keyFile, "tlskey", "", "TLS key file path")
	flag.BoolVar(&tlsEnable, "tls", false, "TLS enable (clients)")
	flag.BoolVar(&binaryMode, "b", false, "Use binary frames instead of text frames")
	flag.BoolVar(&binaryMode, "binary", false, "Use binary frames instead of text frames")
	flag.BoolVar(&reverse, "r", false, "Reverse the connection")
	flag.StringVar(&subprotocols, "attach", "", "Attach a string on connected")
	flag.Usage = usage
	flag.Parse()

	targetAddress = flag.Arg(0)
	if targetAddress == "" {
		fmt.Fprintln(os.Stderr, "No address specified")
		os.Exit(1)
	}
	portString := fmt.Sprintf(":%d", port)

	log.Printf("[INFO] Listening on %s\n", portString)
	if reverse {
		tcpAddr, _ := net.ResolveTCPAddr("tcp", portString)
		tcpListener, err := net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			log.Fatal(err)
		} else {
			for {
				tcpConn, _ := tcpListener.AcceptTCP()
				go tcpRelayHandle(tcpConn)
			}
		}
	} else {

		http.Handle("/", websocket.Handler(wsRelayHandler))

		var err error
		if certFile != "" && keyFile != "" {
			err = http.ListenAndServeTLS(portString, certFile, keyFile, nil)
		} else {
			err = http.ListenAndServe(portString, nil)
		}
		if err != nil {
			log.Fatal(err)
		}
	}
}
