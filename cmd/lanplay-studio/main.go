package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"lanplay/internal/localstudio"
	"lanplay/internal/nativecapture"
	"lanplay/web"
)

func main() {
	serverFlag := flag.String("server", "http://localhost:8080", "LANPlay server origin")
	port := flag.Int("port", 8765, "Local studio port (loopback only)")
	ffmpeg := flag.String("ffmpeg", "", "Path to FFmpeg with gfxcapture (Windows)")
	audio := flag.String("audio-helper", "", "Path to lanplay-audio.exe (Windows)")
	flag.Parse()
	server, err := url.Parse(*serverFlag)
	if err != nil || server.Host == "" || (server.Scheme != "http" && server.Scheme != "https") || server.User != nil || server.RawQuery != "" || server.Fragment != "" || (server.Path != "" && server.Path != "/") {
		log.Fatal("server must be an http(s) origin without a path")
	}
	if *port < 1 || *port > 65535 {
		log.Fatal("port must be 1-65535")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	capture := nativecapture.New(ctx, server, *ffmpeg, *audio)
	defer capture.Close()
	app := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *port), Handler: localstudio.New(server, capture, web.Assets()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 65 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		capture.Close()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = app.Shutdown(shutdown)
	}()
	fmt.Printf("LANPlay Studio · open http://127.0.0.1:%d · server %s\n", *port, server.Scheme+"://"+server.Host)
	if !capture.Capabilities().Available {
		fmt.Println(capture.Capabilities().Reason)
	}
	if err = app.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
