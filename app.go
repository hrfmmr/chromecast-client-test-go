package main

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/gogo/protobuf/proto"
	pb "github.com/hrfmmr/chromecast-client-test-go/protobuf"
)

const (
	googlehomeIP          = "192.168.11.4"
	chromecastDefaultPort = 8009
	defaultSender         = "sender-0"
	defaultReceiver       = "receiver-0"
	namespaceConnection   = "urn:x-cast:com.google.cast.tp.connection"
)

var (
	requestID = 0
)

type Payload struct {
	Type      string `json:"type"`
	RequestId int    `json:"requestId,omitempty"`
}

func NewPayload(t string) *Payload {
	requestID += 1
	return &Payload{
		Type:      t,
		RequestId: requestID,
	}
}

func connect(addr string, port int) *tls.Conn {
	dialer := &net.Dialer{
		Timeout: time.Second * 15,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", addr, port), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("✅ connected!")
	return conn
}

func sendMessage(conn *tls.Conn, payload *Payload, sourceID, destinationID, namespace string) {
	payloadJson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal(err)
	}
	payloadUtf8 := string(payloadJson)
	msg := &pb.CastMessage{
		ProtocolVersion: pb.CastMessage_CASTV2_1_0.Enum(),
		SourceId:        &sourceID,
		DestinationId:   &destinationID,
		Namespace:       &namespace,
		PayloadType:     pb.CastMessage_STRING.Enum(),
		PayloadUtf8:     &payloadUtf8,
	}
	proto.SetDefaults(msg)
	data, err := proto.Marshal(msg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("🚚 reqID:%d %s -> %s[%s]: payload:%s", payload.RequestId, sourceID, destinationID, namespace, payloadJson)
	if err := binary.Write(conn, binary.BigEndian, uint32(len(data))); err != nil {
		log.Fatal(err)
	}
	if _, err := conn.Write(data); err != nil {
		log.Fatal(err)
	}
}

func main() {
	conn := connect(googlehomeIP, chromecastDefaultPort)
	sendMessage(conn, NewPayload("CONNECT"), defaultSender, defaultReceiver, namespaceConnection)
}
