package main

import (
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
)

type Client struct {
	ws       *websocket.Conn
	sendChan chan *[]byte

	unregisterChan chan *Client
}

func NewClient(ws *websocket.Conn, unregisterChan chan *Client) *Client {
	client := &Client{
		ws: ws,
		sendChan: make(chan *[]byte, 512),
		unregisterChan: unregisterChan,
	}

	return client
}

func (c *Client) Close() {
	log.Println("Closing client's send channel")
	close(c.sendChan)
}

func (c *Client) ReadHandler() {
	defer func() {
		c.unregisterChan <- c
	}()

	for {
		msgType, msg, err := c.ws.ReadMessage()
		if err != nil {
			break
		}

		if msgType == websocket.CloseMessage {
			break
		}

		log.Println("Received from client: " + string(msg))
	}
}

func (c *Client) WriteHandler() {
	defer func() {
		c.unregisterChan <- c
	}()

	for {
		select {
		case data, ok := <- c.sendChan:
			if !ok {
				log.Println("Client send failed")
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.ws.WriteMessage(websocket.BinaryMessage, *data)
		}
	}
}

func (c *Client) Run() {
	go c.ReadHandler()
	go c.WriteHandler()
}

type WebSocketHandler struct {
	clients map[*Client]bool  // *client -> is connected (true/false)
	register chan *Client
	unregister chan *Client
	broadcast chan *[]byte

	upgrader *websocket.Upgrader

	portNum int
}

func NewWebSocketHandler(params *Params) *WebSocketHandler {
	clientManager := &WebSocketHandler{
		clients: make(map[*Client]bool),
		register: make(chan *Client),
		unregister: make(chan *Client),
		broadcast: make(chan *[]byte),
		portNum: params.websocketPort,
		upgrader: &websocket.Upgrader{
			ReadBufferSize: params.readBufferSize,
			WriteBufferSize: params.writeBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	return clientManager
}

func (h *WebSocketHandler) BroadcastData(data *[]byte) {
	for client := range h.clients {
		select {
		case client.sendChan <- data:
			break
		}
	}
}

func (h *WebSocketHandler) Run() {
	go h.RunHTTPServer()

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("New client registered. Total: %d\n", len(h.clients))
			break

		case client := <- h.unregister:
			_, ok := h.clients[client]
			if ok {
				delete(h.clients, client)
			}
			log.Printf("Client unregistered.   Total: %d\n", len(h.clients))
			break

		case data := <- h.broadcast:
			h.BroadcastData(data)
			break
		}
	}
}

func (h *WebSocketHandler) RunHTTPServer() {
	r := mux.NewRouter()
	r.HandleFunc("/", h.ServeWS)

	srv := &http.Server{
		Handler: r,
		Addr: fmt.Sprintf("0.0.0.0:%d", h.portNum),
	}

	log.Println("WebSocketHandler starting")

	srv.ListenAndServe()
}

func (h *WebSocketHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	log.Println("New client connected")
	client := NewClient(ws, h.unregister)

	h.register <- client

	go client.Run()
}

type IncomingStreamHandler struct {
	clientManager *WebSocketHandler
	width uint16
	height uint16

	secret string
	portNum int
}

func NewIncomingStreamHandler(params *Params, clientManager *WebSocketHandler) *IncomingStreamHandler {
	incomingStreamHandler := &IncomingStreamHandler{
		clientManager: clientManager,
		secret: params.secret,
		portNum: params.incomingPort,
	}

	return incomingStreamHandler
}

func (s *IncomingStreamHandler) HandlePost(w http.ResponseWriter, r *http.Request) {
	log.Printf("IncomingStream connected: %s\n", r.RemoteAddr)

	for {
		data, err := ioutil.ReadAll(io.LimitReader(r.Body, 1024))
		if err != nil || len(data) == 0 {
			break
		}

		s.clientManager.BroadcastData(&data)
	}

	log.Printf("IncomingStream disconnected: %s\n", r.RemoteAddr)
}

func (s *IncomingStreamHandler) Run() {
	log.Println("IncomingStreamHandler starting")

	r := mux.NewRouter()
	r.HandleFunc(fmt.Sprintf("/%s", s.secret), s.HandlePost)

	srv := &http.Server{
		Handler: r,
		Addr: fmt.Sprintf("0.0.0.0:%d", s.portNum),
	}

	srv.ListenAndServe()
}

type Params struct {
	secret string
	websocketPort int
	incomingPort int

	readBufferSize int
	writeBufferSize int
}

func ParseParams() *Params {
	params := &Params{}

	flag.StringVar(&params.secret, "secret", "secret", "SECRET code for distinct incoming stream data")
	flag.IntVar(&params.incomingPort, "incoming", 8082, "Incoming stream port number")
	flag.IntVar(&params.websocketPort, "websocket", 8084, "WebSocket port number")
	flag.IntVar(&params.readBufferSize, "readbuffer", 8192, "ReadBufferSize used by WebSocket")
	flag.IntVar(&params.writeBufferSize, "writebuffer", 8192, "WriteBufferSize used by WebSocket")

	flag.Parse()

	return params
}

func main() {
	params := ParseParams()

	log.Println("StreamServer parameters")
	log.Println("  SECRET: " + params.secret)
	log.Println("  IncomingPort: " + strconv.Itoa(params.incomingPort))
	log.Println("  WebSocketPort: " + strconv.Itoa(params.websocketPort))

	websocketHandler := NewWebSocketHandler(params)
	incomingStreamHandler := NewIncomingStreamHandler(params, websocketHandler)

	go websocketHandler.Run()
	go incomingStreamHandler.Run()

	r := mux.NewRouter()
	r.PathPrefix("/static").Handler(http.StripPrefix("/static", http.FileServer(http.Dir("static/"))))
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./")))

	log.Println("Demo web page listening at port 8080")
	http.ListenAndServe("0.0.0.0:8080", r)
}
