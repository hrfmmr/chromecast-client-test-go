package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"

	pb "github.com/hrfmmr/chromecast-client-test-go/protobuf"
)

const (
	googlehomeIP            = "192.168.11.4"
	defaultChromecastPort   = 8009
	defaultSender           = "sender-0"
	defaultReceiver         = "receiver-0"
	namespaceConnection     = "urn:x-cast:com.google.cast.tp.connection"
	namespaceRecv           = "urn:x-cast:com.google.cast.receiver"
	namespaceMedia          = "urn:x-cast:com.google.cast.media"
	defaultMediaReceiverApp = "CC1AD845"
	audioURL                = "http://192.168.11.2:8001/hello"
	streamTypeBuffered      = "BUFFERED"
)

var (
	requestID       = RequestID(0)
	ConnectHeader   = PayloadHeader{Type: "CONNECT"}
	GetStatusHeader = PayloadHeader{Type: "GET_STATUS"}
	LaunchHeader    = PayloadHeader{Type: "LAUNCH"}
	LoadHeader      = PayloadHeader{Type: "LOAD"}
)

type RequestID int

type Payload interface {
	SetRequestID(id RequestID)
}

type PayloadHeader struct {
	Type      string `json:"type"`
	RequestId int    `json:"requestId,omitempty"`
}

func (p *PayloadHeader) SetRequestID(id RequestID) {
	p.RequestId = int(id)
}

type LaunchRequest struct {
	PayloadHeader
	AppID string `json:"appId"`
}

type LoadMediaCommand struct {
	PayloadHeader
	Media      MediaItem   `json:"media"`
	Autoplay   bool        `json:"autoplay"`
	CustomData interface{} `json:"customData"`
}

type MediaItem struct {
	ContentID   string      `json:"contentId"`
	ContentType string      `json:"contentType"`
	StreamType  string      `json:"streamType"`
	Metadata    interface{} `json:"metadata"`
}

type ReceiverStatusResponse struct {
	RequestID int            `json:"requestId"`
	Status    ReceiverStatus `json:"status"`
	Type      string         `json:"type"`
}

type ReceiverStatus struct {
	Applications []CastApplication `json:"applications"`
}

type CastApplication struct {
	AppID       string `json:"appId"`
	SessionID   string `json:"sessionId"`
	TransportID string `json:"transportId"`
}

func connect(addr string, port int) (*tls.Conn, error) {
	dialer := &net.Dialer{
		Timeout: time.Second * 15,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", addr, port), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return nil, err
	}
	log.Println("✅ connected!")
	return conn, nil
}

func recvLoop(conn *tls.Conn, recvMsgChan chan<- *pb.CastMessage) {
	for {
		var length uint32
		if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
			log.Fatal(err)
		}
		if length == 0 {
			log.Fatal("empty payload received")
		}
		payload := make([]byte, length)
		n, err := io.ReadFull(conn, payload)
		if err != nil {
			log.Fatal(err)
		}
		if n != int(length) {
			log.Fatalf("invalid payload, wanted:%d but read:%d", length, n)
		}
		log.Printf("🐛recv payload:%v", string(payload))
		msg := &pb.CastMessage{}
		if err := proto.Unmarshal(payload, msg); err != nil {
			log.Printf("failed to unmarshal proto cast message '%s': %v", payload, err)
			continue
		}
		recvMsgChan <- msg
	}
}

func recvMsg(recvMsgChan <-chan *pb.CastMessage, resultChanMap map[RequestID]chan *pb.CastMessage) {
	for msg := range recvMsgChan {
		reqID, err := jsonparser.GetInt([]byte(*msg.PayloadUtf8), "requestId")
		if err != nil {
			log.Printf("👀failed to extract requestId from message:%s", *msg.PayloadUtf8)
			continue
		}
		log.Printf("📦 reqID:%d %s <- %s[%s] response:%s", reqID, *msg.DestinationId, *msg.SourceId, *msg.Namespace, *msg.PayloadUtf8)
		if ch, ok := resultChanMap[RequestID(reqID)]; ok {
			ch <- msg
		}
	}
}

func sendAndWait(conn *tls.Conn, payload Payload, sourceID, destinationID, namespace string, resultChanMap map[RequestID]chan *pb.CastMessage) (*pb.CastMessage, error) {
	requestID, err := sendMessage(conn, payload, sourceID, destinationID, namespace)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resultChan := make(chan *pb.CastMessage, 1)
	resultChanMap[requestID] = resultChan
	defer delete(resultChanMap, requestID)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-resultChan:
		return msg, nil
	}
}

func sendMessage(conn *tls.Conn, payload Payload, sourceID, destinationID, namespace string) (RequestID, error) {
	requestID += 1
	payload.SetRequestID(requestID)
	payloadJson, err := json.Marshal(payload)
	if err != nil {
		return requestID, err
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
		return requestID, err
	}
	log.Printf("🚚 reqID:%d %s -> %s[%s]: payload:%s", requestID, sourceID, destinationID, namespace, payloadJson)
	if err := binary.Write(conn, binary.BigEndian, uint32(len(data))); err != nil {
		return requestID, err
	}
	if _, err := conn.Write(data); err != nil {
		return requestID, err
	}
	return requestID, nil
}

func playAudio(conn *tls.Conn, destinationID, audioURL string, resultChanMap map[RequestID]chan *pb.CastMessage) {
	media := MediaItem{
		ContentID:   audioURL,
		ContentType: "audio/mp3",
		StreamType:  streamTypeBuffered,
	}
	cmd := LoadMediaCommand{
		PayloadHeader: LoadHeader,
		Media:         media,
		Autoplay:      true,
	}
	msg, err := sendAndWait(conn, &cmd, defaultSender, destinationID, namespaceMedia, resultChanMap)
	if err != nil {
		log.Fatal(errors.Wrap(err, "❗failed to play media"))
	}
	log.Printf("playAudio response msg:%s", *msg.PayloadUtf8)
}

func main() {
	conn, err := connect(googlehomeIP, defaultChromecastPort)
	if err != nil {
		log.Fatal(err)
	}
	recvMsgChan := make(chan *pb.CastMessage)
	resultChanMap := map[RequestID]chan *pb.CastMessage{}
	go recvLoop(conn, recvMsgChan)
	go recvMsg(recvMsgChan, resultChanMap)
	log.Printf("🐛try to connect to:%s", defaultReceiver)
	if _, err := sendMessage(conn, &ConnectHeader, defaultSender, defaultReceiver, namespaceConnection); err != nil {
		log.Fatal(errors.Wrap(err, fmt.Sprintf("❗failed to connect to:%s", defaultReceiver)))
	}
	msg, err := sendAndWait(conn, &GetStatusHeader, defaultSender, defaultReceiver, namespaceRecv, resultChanMap)
	if err != nil {
		log.Fatal(err)
	}
	var receiverStatusResp ReceiverStatusResponse
	var defaultApp CastApplication
	if _, _, _, err := jsonparser.Get([]byte(*msg.PayloadUtf8), "status", "applications"); err != nil {
		log.Println("🚀 app is not launched, trying to launch app")
		payload := &LaunchRequest{
			PayloadHeader: LaunchHeader,
			AppID:         defaultMediaReceiverApp,
		}
		msg, err := sendAndWait(conn, payload, defaultSender, defaultReceiver, namespaceRecv, resultChanMap)
		if err != nil {
			log.Fatal(errors.Wrap(err, "❗failed to launch app"))
		}
		if err := json.Unmarshal([]byte(*msg.PayloadUtf8), &receiverStatusResp); err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("❗failed to unmarshal json:%s", *msg.PayloadUtf8)))
		}
		defaultApp = receiverStatusResp.Status.Applications[0]
	} else {
		if err := json.Unmarshal([]byte(*msg.PayloadUtf8), &receiverStatusResp); err != nil {
			log.Fatal(errors.Wrap(err, fmt.Sprintf("❗failed to unmarshal json:%s", *msg.PayloadUtf8)))
		}
		defaultApp = receiverStatusResp.Status.Applications[0]
	}
	destinationID := defaultApp.TransportID
	log.Printf("🐛try to connect to:%s", destinationID)
	if _, err := sendMessage(conn, &ConnectHeader, defaultSender, destinationID, namespaceConnection); err != nil {
		log.Fatal(errors.Wrap(err, fmt.Sprintf("❗failed to connect to:%s", destinationID)))
	}
	playAudio(conn, destinationID, audioURL, resultChanMap)
	fmt.Println("✨Done")
}
