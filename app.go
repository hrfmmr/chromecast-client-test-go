package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	googlehomeIP          = "192.168.11.4"
	chromecastDefaultPort = 8009
)

func connect(addr string, port int) {
	dialer := &net.Dialer{
		Timeout: time.Second * 15,
	}
	_, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", addr, port), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("✅ connected!")
}

func main() {
	connect(googlehomeIP, chromecastDefaultPort)
}
