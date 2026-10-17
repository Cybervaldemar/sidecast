package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"lanplay/internal/channels"
	"lanplay/internal/httpapi"
	"lanplay/internal/media"
	"lanplay/web"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP(S) listen address")
	data := flag.String("data", "data", "Persistent channel storage directory")
	publicURL := flag.String("public-url", "", "Permanent public origin, e.g. https://stream.example.com")
	tlsCert := flag.String("tls-cert", "", "TLS certificate PEM")
	tlsKey := flag.String("tls-key", "", "TLS private key PEM")
	ice := flag.String("ice-servers", "[]", "WebRTC ICE servers JSON (STUN/TURN)")
	udp := flag.String("udp-ports", "", "Optional WebRTC UDP range, e.g. 50000-50100")
	maxViewers := flag.Int("max-viewers", 8, "Maximum viewers per channel (1-64)")
	flag.Parse()
	if *maxViewers < 1 || *maxViewers > 64 {
		log.Fatal("max-viewers must be 1-64")
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatal("tls-cert and tls-key must be provided together")
	}
	if *publicURL != "" {
		u, err := url.Parse(*publicURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			log.Fatal("public-url must be an http(s) origin without a path")
		}
		*publicURL = strings.TrimRight(*publicURL, "/")
	}
	var iceServers []webrtc.ICEServer
	if err := json.Unmarshal([]byte(*ice), &iceServers); err != nil {
		log.Fatal("invalid ice-servers JSON: ", err)
	}
	settings := webrtc.SettingEngine{}
	if *udp != "" {
		parts := strings.Split(*udp, "-")
		if len(parts) != 2 {
			log.Fatal("udp-ports must be min-max")
		}
		min, e1 := strconv.ParseUint(parts[0], 10, 16)
		max, e2 := strconv.ParseUint(parts[1], 10, 16)
		if e1 != nil || e2 != nil || min == 0 || min > max {
			log.Fatal("invalid UDP port range")
		}
		if err := settings.SetEphemeralUDPPortRange(uint16(min), uint16(max)); err != nil {
			log.Fatal(err)
		}
	}
	store, err := channels.Open(*data)
	if err != nil {
		log.Fatal("open channel storage: ", err)
	}
	hub := media.New(webrtc.NewAPI(webrtc.WithSettingEngine(settings)), webrtc.Configuration{ICEServers: iceServers}, *maxViewers, media.WithViewerSettings(settings))
	defer hub.Close()
	app := httpapi.New(store, hub, httpapi.Config{PublicURL: *publicURL, ICEServers: iceServers, MaxViewers: *maxViewers, Addresses: httpapi.Addresses()}, web.Assets())
	server := &http.Server{Addr: *listen, Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("LANPlay v2 · listening on %s · data: %s\n", *listen, *data)
	if *tlsCert != "" {
		err = server.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
