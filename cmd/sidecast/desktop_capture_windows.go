//go:build windows && wails

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-mswin/screencapture"
	"github.com/pion/webrtc/v4"
	"github.com/thesyncim/govpx"
	"sidecast/internal/channels"
	"sidecast/internal/media"
)

type captureSource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Icon   string `json:"icon,omitempty"`
}

type captureStatus struct {
	SessionID string  `json:"sessionId"`
	FPS       float64 `json:"fps"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Bitrate   int     `json:"bitrate"`
	Audio     bool    `json:"audio"`
	Codec     string  `json:"codec"`
	Error     string  `json:"error"`
}

type captureSession struct {
	nick, id      string
	cancel        context.CancelFunc
	done          chan struct{}
	audioDone     chan struct{}
	stream        *screencapture.Stream
	publisher     *media.NativePublisher
	keyframe      atomic.Bool
	audioError    atomic.Value
	frames        atomic.Uint64
	bytes         atomic.Uint64
	started       time.Time
	width, height int
	audio         bool
	codec         string
}

type CaptureController struct {
	mu        sync.Mutex
	store     *channels.Store
	hub       *media.Hub
	active    *captureSession
	lastError string
}

func newCaptureController(store *channels.Store, hub *media.Hub) *CaptureController {
	return &CaptureController{store: store, hub: hub}
}

func (c *CaptureController) Sources() ([]captureSource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	displays, err := screencapture.Displays(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]captureSource, 0, len(displays))
	for i, display := range displays {
		result = append(result, captureSource{ID: "display:" + strconv.FormatUint(display.ID, 10), Name: fmt.Sprintf("Монитор %d · %dx%d", i+1, display.PixelWidth, display.PixelHeight), Width: display.PixelWidth, Height: display.PixelHeight})
	}
	windows, err := screencapture.Windows(ctx)
	if err != nil {
		return result, nil
	}
	icons := make(map[string]string)
	for _, window := range windows {
		if window.Title == "" || window.Minimized || window.Cloaked || window.Bounds.W <= 0 || window.Bounds.H <= 0 {
			continue
		}
		name := window.Title
		if window.AppName != "" {
			name += " · " + window.AppName
		}
		icon, ok := icons[window.ExePath]
		if !ok {
			icon = sourceIcon(window.ExePath)
			icons[window.ExePath] = icon
		}
		result = append(result, captureSource{ID: "window:" + strconv.FormatUint(window.ID, 10), Name: name, Width: window.Bounds.W, Height: window.Bounds.H, Icon: icon})
	}
	return result, nil
}

func (c *CaptureController) Start(nick, token, sourceID string, audio bool) (string, error) {
	if !c.store.Owns(nick, token) {
		return "", errors.New("Нет доступа к каналу")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		return "", media.ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := openCapture(ctx, sourceID)
	if err != nil {
		cancel()
		return "", err
	}
	width, height := stream.Options().Width, stream.Options().Height
	// Video codecs use 4:2:0 chroma. Crop a possible one-pixel edge from
	// windows with odd client dimensions instead of feeding an odd frame.
	width &^= 1
	height &^= 1
	if width < 2 || height < 2 {
		_ = stream.Close()
		cancel()
		return "", errors.New("Источник не возвращает кадры")
	}
	s := &captureSession{nick: nick, id: channels.Token(), cancel: cancel, done: make(chan struct{}), audioDone: make(chan struct{}), stream: stream, started: time.Now(), width: width, height: height, audio: audio}
	ready := make(chan string, 1)
	proceed := make(chan struct{})
	go c.runVideo(ctx, s, ready, proceed)
	codec := <-ready
	s.codec = codec
	publisher, err := c.hub.PublishNative(nick, s.id, audio, func() { s.keyframe.Store(true) }, codec)
	if err != nil {
		cancel()
		return "", err
	}
	s.publisher = publisher
	c.active = s
	c.lastError = ""
	if audio {
		ready := make(chan error, 1)
		go func() {
			defer close(s.audioDone)
			err := runLoopback(ctx, ready, publisher.WriteAudio)
			if err != nil && ctx.Err() == nil {
				s.audioError.Store(fmt.Sprintf("Звук ПК: %v", err))
				s.cancel()
			}
		}()
		if err := <-ready; err != nil {
			c.active = nil
			cancel()
			<-s.audioDone
			publisher.Close()
			return "", fmt.Errorf("Звук ПК: %w", err)
		}
	} else {
		close(s.audioDone)
	}
	close(proceed)
	return s.id, nil
}

func openCapture(ctx context.Context, sourceID string) (*screencapture.Stream, error) {
	kind, raw, ok := strings.Cut(sourceID, ":")
	if !ok {
		return nil, errors.New("Выберите монитор или окно")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, errors.New("Недопустимый источник")
	}
	opt := screencapture.Options{FPS: 240, QueueDepth: 3}
	switch kind {
	case "display":
		displays, err := screencapture.Displays(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range displays {
			if d.ID == id {
				return screencapture.CaptureDisplay(ctx, d, opt)
			}
		}
	case "window":
		windows, err := screencapture.Windows(ctx)
		if err != nil {
			return nil, err
		}
		for _, w := range windows {
			if w.ID == id {
				return screencapture.CaptureWindow(ctx, w, opt)
			}
		}
	}
	return nil, screencapture.ErrNotFound
}

func (c *CaptureController) runVideo(ctx context.Context, s *captureSession, ready chan<- string, proceed <-chan struct{}) {
	defer close(s.done)
	defer s.stream.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var hardware *hardwareH264Encoder
	var hardwareErr error
	for _, fps := range []int{240, 160, 120, 60} {
		hardware, hardwareErr = openHardwareH264(s.width, s.height, fps, media.MaxVideoBitrate)
		if hardwareErr == nil {
			break
		}
	}
	codec := webrtc.MimeTypeH264
	if hardwareErr != nil {
		codec = webrtc.MimeTypeVP8
	} else {
		defer hardware.Close()
	}
	ready <- codec
	select {
	case <-proceed:
	case <-ctx.Done():
		return
	}
	if s.publisher == nil {
		return
	}
	defer s.publisher.Close()
	var failed error
	defer func() {
		c.mu.Lock()
		if c.active == s {
			c.active = nil
			if failed != nil {
				c.lastError = failed.Error()
			} else if audioErr := s.audioError.Load(); audioErr != nil {
				c.lastError = audioErr.(string)
			}
		}
		c.mu.Unlock()
	}()
	if hardware != nil {
		failed = c.runHardwareVideo(ctx, s, hardware)
	} else {
		failed = c.runSoftwareVideo(ctx, s)
	}
	s.cancel()
}

func (c *CaptureController) runSoftwareVideo(ctx context.Context, s *captureSession) error {
	encoder, err := govpx.NewVP8Encoder(govpx.EncoderOptions{Width: s.width, Height: s.height, FPS: 240, TimebaseNum: 1, TimebaseDen: 1_000_000, Threads: 1, RateControlMode: govpx.RateControlCBR, TargetBitrateKbps: media.MaxVideoBitrate / 1000, MaxBitrateKbps: media.MaxVideoBitrate / 1000, Deadline: govpx.DeadlineRealtime, CpuUsed: 8, KeyFrameInterval: 240, ErrorResilient: true, ScreenContentMode: 1})
	if err != nil {
		return fmt.Errorf("Кодировщик VP8: %w", err)
	}
	defer encoder.Close()
	yuv := newI420(s.width, s.height)
	encoded := make([]byte, max(4*1024*1024, s.width*s.height*2))
	var first, previous time.Time
	var lastTarget time.Time
	currentTarget := media.MaxVideoBitrate
	for ctx.Err() == nil && s.publisher.Active() {
		waitCtx, cancel := context.WithTimeout(ctx, time.Second)
		frame, err := s.stream.WaitFrame(waitCtx)
		cancel()
		if errors.Is(err, screencapture.ErrNoFrame) {
			continue
		}
		if err != nil {
			if ctx.Err() == nil {
				return err
			}
			break
		}
		if !frame.Valid() {
			continue
		}
		if frame.Width < s.width || frame.Height < s.height {
			return errors.New("Размер кадра стал меньше выбранного источника. Запустите эфир снова")
		}
		if first.IsZero() {
			first = frame.At
			previous = first
		}
		if time.Since(lastTarget) >= 2*time.Second {
			if target, err := c.hub.TargetBitrate(s.nick, s.id); err == nil && target != currentTarget {
				if err := encoder.SetBitrateKbps(target / 1000); err == nil {
					currentTarget = target
				}
			}
			lastTarget = time.Now()
		}
		bgraToI420(frame, &yuv)
		pts := uint64(frame.At.Sub(first).Microseconds())
		duration := uint64(max(1, frame.At.Sub(previous).Microseconds()))
		if frame.At.Equal(first) {
			duration = 1_000_000 / 60
		}
		previous = frame.At
		var flags govpx.EncodeFlags
		if s.keyframe.Swap(false) {
			flags = govpx.EncodeForceKeyFrame
		}
		result, err := encoder.EncodeInto(encoded, yuv, pts, duration, flags)
		if err != nil {
			return fmt.Errorf("Кодирование видео: %w", err)
		}
		if result.Dropped || len(result.Data) == 0 {
			continue
		}
		if err := s.publisher.WriteVideo(result.Data, frame.At); err != nil {
			return err
		}
		s.frames.Add(1)
		s.bytes.Add(uint64(len(result.Data)))
	}
	return nil
}

func (c *CaptureController) runHardwareVideo(ctx context.Context, s *captureSession, encoder *hardwareH264Encoder) error {
	nv12 := make([]byte, s.width*s.height*3/2)
	currentTarget := media.MaxVideoBitrate
	var lastTarget time.Time
	for ctx.Err() == nil && s.publisher.Active() {
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
		frame, err := s.stream.WaitFrame(waitCtx)
		cancel()
		if err != nil && !errors.Is(err, screencapture.ErrNoFrame) {
			if ctx.Err() == nil {
				return err
			}
			break
		}
		var input []byte
		var at time.Time
		if err == nil && frame.Valid() {
			if frame.Width < s.width || frame.Height < s.height {
				return errors.New("Размер кадра стал меньше выбранного источника. Запустите эфир снова")
			}
			bgraToNV12(frame, nv12, s.width, s.height)
			input, at = nv12, frame.At
			if s.keyframe.Swap(false) {
				_ = encoder.ForceKeyframe()
			}
		}
		if time.Since(lastTarget) >= 2*time.Second {
			if target, err := c.hub.TargetBitrate(s.nick, s.id); err == nil && target != currentTarget {
				if encoder.SetBitrate(target) == nil {
					currentTarget = target
				}
			}
			lastTarget = time.Now()
		}
		output, err := encoder.Poll(input, at)
		if err != nil {
			return fmt.Errorf("Кодирование H.264: %w", err)
		}
		for _, encoded := range output {
			if err := s.publisher.WriteVideo(encoded.Data, encoded.At); err != nil {
				return err
			}
			s.frames.Add(1)
			s.bytes.Add(uint64(len(encoded.Data)))
		}
	}
	return nil
}

func (c *CaptureController) Stop() {
	c.mu.Lock()
	s := c.active
	if s != nil {
		c.active = nil
		s.cancel()
	}
	c.mu.Unlock()
	if s != nil {
		<-s.done
		<-s.audioDone
		s.publisher.Close()
	}
}

func (c *CaptureController) Status() captureStatus {
	c.mu.Lock()
	s := c.active
	lastError := c.lastError
	c.mu.Unlock()
	if s == nil {
		return captureStatus{Error: lastError}
	}
	seconds := time.Since(s.started).Seconds()
	if seconds < 1 {
		seconds = 1
	}
	return captureStatus{SessionID: s.id, FPS: float64(s.frames.Load()) / seconds, Width: s.width, Height: s.height, Bitrate: int(float64(s.bytes.Load()) * 8 / seconds), Audio: s.audio, Codec: s.codec}
}
