package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
)

/*
-Keeps track of all connected clients
-clients that are trying to be registered
-clients that have been destroyed or are waiting to be removed
-messages that are to be broadcasted between connected clients
*/
type ClientManager struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

/*
-each client has a unique ID, socket connection and a message to be sent
*/
type Client struct {
	id     string
	socket *websocket.Conn
	send   chan []byte
}

/*
-message information
*/
type Message struct {
	Sender    string `json: "sender,omitempty"`
	Recipient string `json:"recipient,omitempty"`
	Content   string `json:"content,omitempty"`
}

// global ClientManager
var manager = ClientManager{
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[*Client]bool),
}

func (mn *ClientManager) start() {
	for {
		select {
		/*
			Every time the manager.register channel has data, the client will be added to the map of available clients managed by the client manager. After adding the client, a JSON message is sent to all other clients, not including the one that just connected.
		*/
		case conn := <-mn.register:
			mn.clients[conn] = true
			jsonMessage, _ := json.Marshal(&Message{Content: "/A new socket has connected"})
			mn.send(jsonMessage, conn)

			/*
				If a client disconnects for any reason, the manager.unregister channel will have data. The channel data in the disconnected client will be closed and the client will be removed from the client manager. A message announcing the disappearance of a socket will be sent to all remaining connections.
			*/
		case conn := <-mn.unregister:
			if _, ok := mn.clients[conn]; ok {
				close(conn.send)
				delete(manager.clients, conn)
				jsonMessage, _ := json.Marshal(&Message{Content: "/A socket has disconnected"})
				manager.send(jsonMessage, conn)
			}

			/*
				If the manager.broadcast channel has data it means that we’re trying to send and receive messages. We want to loop through each managed client sending the message to each of them. If for some reason the channel is clogged or the message can’t be sent, we assume the client has disconnected and we remove them instead.
			*/
		case message := <-mn.broadcast:
			for conn := range mn.clients {
				select {
				case conn.send <- message:
				default:
					close(conn.send)
					delete(mn.clients, conn)
				}
			}

		}
	}
}

func (mn *ClientManager) send(message []byte, ignore *Client) {
	for conn := range mn.clients {
		if conn != ignore {
			conn.send <- message
		}
	}
}

func (cli *Client) read() {
	defer func() {
		manager.unregister <- cli
		cli.socket.Close()
	}()

	for {
		_, message, err := cli.socket.ReadMessage()
		if err != nil {
			manager.unregister <- cli
			cli.socket.Close()
			break
		}
		jsonMessage, _ := json.Marshal(&Message{Sender: cli.id, Content: string(message)})
		manager.broadcast <- jsonMessage
	}
}

func (cli *Client) write() {
	defer func() {
		cli.socket.Close()
	}()

	for message := range cli.send {
		if err := cli.socket.WriteMessage(websocket.TextMessage, message); err != nil {
			// Handle the error
			fmt.Println("Error writing message to socket:", err)
			break
		}
	}
	cli.socket.WriteMessage(websocket.CloseMessage, []byte{})

}

func wsPage(res http.ResponseWriter, req *http.Request) {
	conn, err := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}).Upgrade(res, req, nil)
	if err != nil {
		http.NotFound(res, req)
		return
	}

	uuid, err := uuid.NewV4()
	if err != nil {
		// Handle the error
		fmt.Println("Error generating UUID:", err)
		return
	}
	client := &Client{id: uuid.String(), socket: conn, send: make(chan []byte)}
	manager.register <- client

	go client.read()
	go client.write()
}

func main() {
	fmt.Println("Starting application....")
	go manager.start()
	http.HandleFunc("ws", wsPage)
	http.ListenAndServe(":12345", nil)
}
