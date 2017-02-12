MPEG Streaming Demo with jsmpeg.js
==================================

MPEG streaming demo with [jsmpeg.js](https://github.com/phoboslab/jsmpeg) implemented by Go

Setup build environment
-----------------------

```
$ go get github.com/gorilla/websocket
$ go get github.com/gorilla/mux
$ go build
```


Run
---

Start streaming WebSocket and homepage server
```
$ go run stream-server.go
StreamServer parameters
  SECRET: secret
  IncomingPort: 8082
  WebSocketPort: 8084
IncomingStreamHandler starting
Demo web page listening at port 8080
WebSocketHandler starting
```

Start ffmpeg for incoming stream from iSight
```
$ ffmpeg -s 1024x576 -f avfoundation -i "0:1" \
  -f mpegts -codec:v mpeg1video -b:v 800k -r 24 -framerate 24 \
  -codec:a mp2 -b:a 128k -muxdelay 0.001 http://localhost:8082/secret
```

Open the page http://localhost:8080
