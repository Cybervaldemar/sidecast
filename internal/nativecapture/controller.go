package nativecapture

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"lanplay/internal/channels"
)

type Stats struct {
	FPS     float64 `json:"fps"`
	Bitrate float64 `json:"bitrate"`
	Codec   string  `json:"codec"`
}
type Status struct {
	Phase     string `json:"phase"`
	SessionID string `json:"sessionId,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	Error     string `json:"error,omitempty"`
	Encoder   string `json:"encoder,omitempty"`
	Stats     Stats  `json:"stats"`
}
type Controller struct {
	mu                  sync.Mutex
	ctx                 context.Context
	server              *url.URL
	client              *http.Client
	ffmpeg, audioHelper string
	capabilities        Capabilities
	current             *publication
	starting            bool
	lastError           string
}
type publication struct {
	mu                                sync.Mutex
	ctx                               context.Context
	cancel                            context.CancelFunc
	once                              sync.Once
	pc                                *webrtc.PeerConnection
	video, audio                      *net.UDPConn
	nickname, token, session, encoder string
	frames, bytes                     atomic.Uint64
	status                            Status // guarded by Controller.mu
}

func New(ctx context.Context, server *url.URL, ffmpeg, audioHelper string) *Controller {
	c := &Controller{ctx: ctx, server: server, ffmpeg: findFFmpeg(ffmpeg), audioHelper: audioHelper, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.capabilities.Platform = runtime.GOOS
	if !supported() {
		c.capabilities.Reason = "На Linux используется браузерный захват видео и звука. Нативный модуль доступен только в Windows."
		return c
	}
	if !probeFFmpeg(ctx, c.ffmpeg) {
		c.capabilities.Reason = "Нужен FFmpeg с фильтром gfxcapture рядом с приложением или параметр --ffmpeg. Браузерный захват доступен."
		return c
	}
	c.capabilities.Available = true
	if c.audioHelper == "" {
		if executable, err := os.Executable(); err == nil {
			c.audioHelper = filepath.Join(filepath.Dir(executable), "lanplay-audio.exe")
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, c.audioHelper, "--capabilities")
	hideProcess(cmd)
	if output, err := cmd.Output(); err == nil {
		var capabilities struct {
			System      bool `json:"systemAudio"`
			Application bool `json:"applicationAudio"`
		}
		if json.Unmarshal(output, &capabilities) == nil {
			c.capabilities.Audio = capabilities.System
			c.capabilities.ApplicationAudio = capabilities.Application
		}
	}
	return c
}

func (c *Controller) Capabilities() Capabilities { return c.capabilities }
func (c *Controller) Sources() ([]Source, error) {
	if !c.capabilities.Available {
		return nil, errors.New(c.capabilities.Reason)
	}
	return sources()
}

func (c *Controller) request(ctx context.Context, method, path, token string, body, result any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	endpoint := *c.server
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return errors.New("Сервер трансляций недоступен.")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 128*1024)).Decode(&failure)
		switch failure.Error {
		case "owner_required":
			return errors.New("Ключ не подходит к этому каналу.")
		case "stream_busy":
			return errors.New("Канал уже ведёт эфир.")
		}
		return fmt.Errorf("Сервер отклонил подключение (%d).", response.StatusCode)
	}
	if result != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 128*1024)).Decode(result)
	}
	return nil
}

func listenRTP() (*net.UDPConn, error) {
	connection, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err == nil {
		_ = connection.SetReadBuffer(4 * 1024 * 1024)
	}
	return connection, err
}

func (c *Controller) Start(ctx context.Context, nickname, token string, settings Settings) (_ Status, err error) {
	if !c.capabilities.Available {
		return Status{}, errors.New(c.capabilities.Reason)
	}
	if !channels.Valid(nickname) || len(token) != 64 {
		return Status{}, errors.New("Выберите канал и восстановите ключ владельца.")
	}
	if err = settings.validate(); err != nil {
		return Status{}, err
	}
	if settings.Audio != "off" && !c.capabilities.Audio {
		return Status{}, errors.New("Компонент нативного звука недоступен. Установите полную Windows-сборку или выберите браузерный захват.")
	}
	if settings.Audio == "window" && !c.capabilities.ApplicationAudio {
		return Status{}, errors.New("Звук приложения требует Windows build 20348 или новее. Системный звук не будет включён автоматически.")
	}
	available, err := c.Sources()
	if err != nil {
		return Status{}, err
	}
	var source Source
	var audioPID uint32
	for _, item := range available {
		if item.ID == settings.SourceID {
			source = item
		}
		if item.ID == settings.AudioSourceID && item.Kind == "window" {
			audioPID = item.ProcessID
		}
	}
	if source.ID == "" {
		return Status{}, errors.New("Источник закрыт. Обновите список окон.")
	}
	if settings.Audio == "window" && audioPID == 0 {
		return Status{}, errors.New("Выберите приложение, звук которого нужно передавать.")
	}
	c.mu.Lock()
	if c.current != nil || c.starting {
		c.mu.Unlock()
		return Status{}, errors.New("Нативная трансляция уже запущена.")
	}
	c.starting = true
	c.lastError = ""
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.starting = false; c.mu.Unlock() }()
	encoder, err := chooseEncoder(ctx, c.ffmpeg, settings.Encoder)
	if err != nil {
		return Status{}, err
	}
	if c.ctx.Err() != nil {
		return Status{}, errors.New("Приложение закрывается.")
	}
	var config struct {
		ICEServers []webrtc.ICEServer `json:"iceServers"`
	}
	if err = c.request(ctx, http.MethodGet, "/api/config", "", nil, &config); err != nil {
		return Status{}, err
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: config.ICEServers})
	if err != nil {
		return Status{}, err
	}
	streamCtx, cancel := context.WithCancel(c.ctx)
	a := &publication{ctx: streamCtx, cancel: cancel, pc: pc, nickname: nickname, token: token, encoder: encoder, status: Status{Phase: "starting", Nickname: nickname, Encoder: encoder}}
	defer func() {
		if err != nil {
			c.finish(a, "")
		}
	}()
	c.mu.Lock()
	c.current = a
	c.mu.Unlock()
	connected := make(chan struct{})
	var connectedOnce sync.Once
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateConnected {
			connectedOnce.Do(func() { close(connected) })
		}
		if state == webrtc.PeerConnectionStateFailed {
			go c.finish(a, "Соединение с сервером потеряно.")
		}
	})
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "video", "native")
	if err != nil {
		return Status{}, err
	}
	if err = addSender(pc, videoTrack); err != nil {
		return Status{}, err
	}
	videoConnection, err := listenRTP()
	if err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	a.video = videoConnection
	stopped := a.ctx.Err() != nil
	a.mu.Unlock()
	if stopped {
		_ = videoConnection.Close()
		return Status{}, errors.New("Запуск остановлен.")
	}
	var audioTrack *webrtc.TrackLocalStaticRTP
	if settings.Audio != "off" {
		audioTrack, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "native")
		if err != nil {
			return Status{}, err
		}
		if err = addSender(pc, audioTrack); err != nil {
			return Status{}, err
		}
		audioConnection, listenErr := listenRTP()
		err = listenErr
		if err != nil {
			return Status{}, err
		}
		a.mu.Lock()
		a.audio = audioConnection
		stopped = a.ctx.Err() != nil
		a.mu.Unlock()
		if stopped {
			_ = audioConnection.Close()
			return Status{}, errors.New("Запуск остановлен.")
		}
	}
	description, err := pc.CreateOffer(nil)
	if err != nil {
		return Status{}, err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(description); err != nil {
		return Status{}, err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return Status{}, ctx.Err()
	case <-time.After(12 * time.Second):
		return Status{}, errors.New("Сбор сетевых адресов занял слишком много времени.")
	}
	var negotiated struct {
		Description webrtc.SessionDescription `json:"description"`
		SessionID   string                    `json:"sessionId"`
	}
	if err = c.request(ctx, http.MethodPost, "/api/channels/"+nickname+"/publish", token, map[string]any{"description": pc.LocalDescription(), "audio": audioTrack != nil}, &negotiated); err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	a.session = negotiated.SessionID
	stopped = a.ctx.Err() != nil
	a.mu.Unlock()
	if stopped {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = c.request(cleanupCtx, http.MethodDelete, "/api/channels/"+nickname+"/publish/"+negotiated.SessionID, token, nil, nil)
		return Status{}, errors.New("Запуск остановлен.")
	}
	if err = pc.SetRemoteDescription(negotiated.Description); err != nil {
		return Status{}, err
	}
	select {
	case <-connected:
	case <-a.ctx.Done():
		return Status{}, errors.New("Запуск остановлен.")
	case <-ctx.Done():
		return Status{}, ctx.Err()
	case <-time.After(12 * time.Second):
		return Status{}, errors.New("Не удалось соединиться с сервером трансляций.")
	}
	videoPort := a.video.LocalAddr().(*net.UDPAddr).Port
	audioPort := 0
	if a.audio != nil {
		audioPort = a.audio.LocalAddr().(*net.UDPAddr).Port
	}
	args, err := ffmpegArgs(source, settings, encoder, videoPort, audioPort)
	if err != nil {
		return Status{}, err
	}
	diagnostics := &diagnosticBuffer{}
	command := exec.CommandContext(a.ctx, c.ffmpeg, args...)
	hideProcess(command)
	command.Stderr = diagnostics
	var audioCommand *exec.Cmd
	if a.audio != nil {
		input, pipeErr := command.StdinPipe()
		if pipeErr != nil {
			return Status{}, pipeErr
		}
		audioArgs := []string{"--system"}
		if settings.Audio == "window" {
			audioArgs = []string{"--pid", strconv.FormatUint(uint64(audioPID), 10)}
		}
		audioCommand = exec.CommandContext(a.ctx, c.audioHelper, audioArgs...)
		hideProcess(audioCommand)
		audioCommand.Stdout = input
		audioCommand.Stderr = diagnostics
	}
	firstVideo := make(chan struct{})
	firstAudio := make(chan struct{})
	go c.forward(a, a.video, videoTrack, true, firstVideo)
	if a.audio != nil {
		go c.forward(a, a.audio, audioTrack, false, firstAudio)
	} else {
		close(firstAudio)
	}
	if err = command.Start(); err != nil {
		return Status{}, errors.New("Не удалось запустить FFmpeg.")
	}
	go func() {
		runErr := command.Wait()
		if a.ctx.Err() == nil {
			message := "Источник захвата закрыт."
			if runErr != nil {
				log.Printf("native video: %v\n%s", runErr, diagnostics.String())
				message = "Нативный захват остановлен. Попробуйте другой кодировщик или браузерный режим."
			}
			c.finish(a, message)
		}
	}()
	if audioCommand != nil {
		if err = audioCommand.Start(); err != nil {
			return Status{}, errors.New("Не удалось запустить компонент звука.")
		}
		go func() {
			runErr := audioCommand.Wait()
			if a.ctx.Err() == nil {
				log.Printf("native audio: %v\n%s", runErr, diagnostics.String())
				c.finish(a, "Захват выбранного звука остановлен. Системный звук не будет включён вместо него.")
			}
		}()
	}
	startup := time.NewTimer(12 * time.Second)
	defer startup.Stop()
	for _, ready := range []chan struct{}{firstVideo, firstAudio} {
		select {
		case <-ready:
		case <-a.ctx.Done():
			return Status{}, errors.New("Не удалось начать нативный захват. Проверьте источник и кодировщик.")
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-startup.C:
			return Status{}, errors.New("Нет видео или аудиоданных от нативного модуля. Проверьте источник и кодировщик.")
		}
	}
	c.mu.Lock()
	if c.current != a || a.ctx.Err() != nil {
		c.mu.Unlock()
		return Status{}, errors.New("Запуск остановлен.")
	}
	a.status.Phase = "live"
	a.status.SessionID = a.session
	status := a.status
	c.mu.Unlock()
	go c.sample(a)
	return status, nil
}

func addSender(pc *webrtc.PeerConnection, track *webrtc.TrackLocalStaticRTP) error {
	sender, err := pc.AddTrack(track)
	if err != nil {
		return err
	}
	go func() {
		for {
			if _, _, err := sender.ReadRTCP(); err != nil {
				return
			}
		}
	}()
	return nil
}
func (c *Controller) forward(a *publication, connection *net.UDPConn, track *webrtc.TrackLocalStaticRTP, video bool, first chan struct{}) {
	buffer := make([]byte, 65536)
	var ready sync.Once
	for {
		n, _, err := connection.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		packet := &rtp.Packet{}
		if packet.Unmarshal(buffer[:n]) != nil {
			continue
		}
		if err = track.WriteRTP(packet); err != nil {
			if a.ctx.Err() == nil {
				go c.finish(a, "Передача на сервер прервана.")
			}
			return
		}
		if video {
			a.bytes.Add(uint64(len(packet.Payload)))
			if packet.Marker {
				a.frames.Add(1)
			}
		}
		ready.Do(func() { close(first) })
	}
}
func (c *Controller) sample(a *publication) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := time.Now()
	frames, bytes := a.frames.Load(), a.bytes.Load()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			nextFrames, nextBytes := a.frames.Load(), a.bytes.Load()
			seconds := now.Sub(last).Seconds()
			c.mu.Lock()
			a.status.Stats = Stats{FPS: float64(nextFrames-frames) / seconds, Bitrate: float64(nextBytes-bytes) * 8 / seconds / 1e6, Codec: "H264"}
			c.mu.Unlock()
			last = now
			frames = nextFrames
			bytes = nextBytes
		}
	}
}
func (c *Controller) finish(a *publication, message string) {
	a.once.Do(func() {
		a.cancel()
		a.mu.Lock()
		video, audio, session := a.video, a.audio, a.session
		a.mu.Unlock()
		if video != nil {
			_ = video.Close()
		}
		if audio != nil {
			_ = audio.Close()
		}
		_ = a.pc.Close()
		c.mu.Lock()
		if c.current == a {
			c.current = nil
			c.lastError = message
		}
		c.mu.Unlock()
		if session != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = c.request(ctx, http.MethodDelete, "/api/channels/"+a.nickname+"/publish/"+session, a.token, nil, nil)
		}
	})
}
func (c *Controller) Status(token string) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return Status{Phase: "idle", Error: c.lastError}, nil
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(c.current.token)) != 1 {
		return Status{}, errors.New("Нужен ключ владельца активного канала.")
	}
	return c.current.status, nil
}
func (c *Controller) Stop(token string) error {
	c.mu.Lock()
	a := c.current
	if a != nil && subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) != 1 {
		c.mu.Unlock()
		return errors.New("Нужен ключ владельца активного канала.")
	}
	c.mu.Unlock()
	if a != nil {
		c.finish(a, "")
	}
	return nil
}
func (c *Controller) Close() {
	c.mu.Lock()
	a := c.current
	c.mu.Unlock()
	if a != nil {
		c.finish(a, "")
	}
}
