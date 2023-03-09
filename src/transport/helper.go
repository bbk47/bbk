package transport

import (
	"github.com/gorilla/websocket"
	"net"
)

func BindStreamSocket(socket net.Conn, events *Events) {
	events.Status <- "open"
	// send ready event
	defer func() {
		socket.Close()
		events.Status <- "close"
	}()
	for {
		// 接收数据
		lenbuf := make([]byte, 2)
		_, err := socket.Read(lenbuf)
		if err != nil {
			// send error event
			events.Status <- "read err:" + err.Error()
			return
		}
		length1 := (int(lenbuf[0]))*256 + (int(lenbuf[1]))
		databuf := make([]byte, length1)
		_, err = socket.Read(databuf)
		events.Data <- databuf
		// send data event
	}
}

func SendStreamSocket(socket net.Conn, data []byte) (err error) {
	length := len(data)
	data2 := append([]byte{uint8(length >> 8), uint8(length % 256)}, data...)
	_, err = socket.Write(data2)
	return err
}

func BindWsSocket(wss *websocket.Conn, events *Events) {
	events.Status <- "open"
	// send ready event
	defer func() {
		wss.Close()
		events.Status <- "close"
	}()
	for {
		// 接收数据
		_, packet, err := wss.ReadMessage()
		if err != nil {
			// send error event
			events.Status <- "read ws err:" + err.Error()
			return
		}
		events.Data <- packet
		// send data event
	}
}

func SendWsSocket(wss *websocket.Conn, data []byte) (err error) {
	err = wss.WriteMessage(websocket.BinaryMessage, data)
	return err
}
