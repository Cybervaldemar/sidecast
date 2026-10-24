package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"sidecast/internal/channels"
	"sidecast/internal/desktop"
	"sidecast/internal/httpapi"
	"sidecast/internal/media"
	"sidecast/web"
)

var noTray bool

func main() {
	if err := run(); err != nil {
		log.Print(err)
		if !noTray {
			desktop.ShowError(err)
		}
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", ":8080", "HTTP(S) listen address")
	data := flag.String("data", desktop.DataDirectory(), "Persistent channel storage directory")
	flag.BoolVar(&noTray, "no-tray", false, "Run without the Windows tray icon (services and automation)")
	publicURL := flag.String("public-url", "", "Permanent public origin, e.g. https://stream.example.com")
	tlsCert := flag.String("tls-cert", "", "TLS certificate PEM")
	tlsKey := flag.String("tls-key", "", "TLS private key PEM")
	ice := flag.String("ice-servers", "[]", "WebRTC ICE servers JSON (STUN/TURN)")
	udp := flag.String("udp-ports", "", "Optional WebRTC UDP range, e.g. 50000-50100")
	maxViewers := flag.Int("max-viewers", 8, "Maximum viewers per channel (1-64)")
	flag.Parse()
	if *maxViewers < 1 || *maxViewers > 64 {
		return fmt.Errorf("max-viewers must be 1-64")
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return fmt.Errorf("tls-cert and tls-key must be provided together")
	}
	if *publicURL != "" {
		u, err := url.Parse(*publicURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("public-url must be an http(s) origin without a path")
		}
		*publicURL = strings.TrimRight(*publicURL, "/")
	}
	var iceServers []webrtc.ICEServer
	if err := json.Unmarshal([]byte(*ice), &iceServers); err != nil {
		return fmt.Errorf("invalid ice-servers JSON: %w", err)
	}
	settings := webrtc.SettingEngine{}
	if *udp != "" {
		parts := strings.Split(*udp, "-")
		if len(parts) != 2 {
			return fmt.Errorf("udp-ports must be min-max")
		}
		min, e1 := strconv.ParseUint(parts[0], 10, 16)
		max, e2 := strconv.ParseUint(parts[1], 10, 16)
		if e1 != nil || e2 != nil || min == 0 || min > max {
			return fmt.Errorf("invalid UDP port range")
		}
		if err := settings.SetEphemeralUDPPortRange(uint16(min), uint16(max)); err != nil {
			return err
		}
	}
	store, err := channels.Open(*data)
	if err != nil {
		return fmt.Errorf("open channel storage: %w", err)
	}
	publisherAPI, err := media.NewPublisherAPI(settings)
	if err != nil {
		return fmt.Errorf("configure streaming: %w", err)
	}
	hub := media.New(publisherAPI, webrtc.Configuration{ICEServers: iceServers}, *maxViewers, media.WithViewerSettings(settings))
	defer hub.Close()
	app := httpapi.New(store, hub, httpapi.Config{PublicURL: *publicURL, ICEServers: iceServers, MaxViewers: *maxViewers}, web.Assets())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Addr: *listen, Handler: desktop.WithControls(app, cancel), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("start Sidecast: %w", err)
	}
	defer listener.Close()
	secure := *tlsCert != ""
	studio := desktop.LocalURL(listener.Addr().String(), secure) + "/studio"
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	fmt.Printf("Sidecast v2 · listening on %s · data: %s\n", *listen, *data)
	if studio != "" {
		fmt.Printf("Sidecast studio: %s\n", studio)
	}
	serveDone := make(chan error, 1)
	go func() {
		var serveErr error
		if secure {
			serveErr = server.ServeTLS(listener, *tlsCert, *tlsKey)
		} else {
			serveErr = server.Serve(listener)
		}
		serveDone <- serveErr
		cancel()
	}()
	if noTray {
		<-ctx.Done()
	} else {
		err = desktop.Run(ctx, studio, cancel)
		cancel()
	}
	<-shutdownDone
	serveErr := <-serveDone
	if err != nil {
		return err
	}
	if serveErr != nil && serveErr != http.ErrServerClosed {
		return serveErr
	}
	return nil
}
