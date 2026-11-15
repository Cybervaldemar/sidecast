//go:build windows && wails

package main

import (
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	mf "github.com/deploymenttheory/go-bindings-win32/bindings/win32/media/mediafoundation"
	com "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com"
	variant "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/variant"
)

var (
	codecMeanBitrate   = win32.GUID{Data1: 0xf7222374, Data2: 0x2144, Data3: 0x4815, Data4: [8]byte{0xb5, 0x50, 0xa3, 0x7f, 0x8e, 0x12, 0xee, 0x52}}
	codecForceKeyframe = win32.GUID{Data1: 0x398c1b98, Data2: 0x8353, Data3: 0x475a, Data4: [8]byte{0x9e, 0xf2, 0x8f, 0x26, 0x5d, 0x26, 0x03, 0x45}}
)

type hardwareH264Encoder struct {
	transform     *mf.IMFTransform
	events        *mf.IMFMediaEventGenerator
	outputInfo    mf.MFT_OUTPUT_STREAM_INFO
	width, height int
	inputRequests int
	base          time.Time
	previousInput time.Time
	frameDuration time.Duration
	codecAPI      *mf.ICodecAPI
}

// The encoder must be created, driven and closed on the same locked OS thread.
func openHardwareH264(width, height, fps, bitrate int) (_ *hardwareH264Encoder, err error) {
	if width%2 != 0 || height%2 != 0 {
		return nil, errors.New("H.264 requires even dimensions")
	}
	if _, err = com.CoInitializeEx(0); err != nil {
		return nil, fmt.Errorf("COM: %w", err)
	}
	defer func() {
		if err != nil {
			com.CoUninitialize()
		}
	}()
	if err = mf.MFStartup(mf.MF_VERSION, 0); err != nil {
		return nil, fmt.Errorf("Media Foundation: %w", err)
	}
	defer func() {
		if err != nil {
			_ = mf.MFShutdown()
		}
	}()
	list, err := hardwareH264Activations()
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, activation := range list {
			activation.Release()
		}
	}()
	if len(list) == 0 {
		return nil, errors.New("Аппаратный кодировщик H.264 не найден")
	}
	var failures []error
	for _, activation := range list {
		unknown, activationErr := activation.ActivateObject(&mf.IID_IMFTransform)
		if activationErr != nil {
			failures = append(failures, activationErr)
			continue
		}
		transform := (*mf.IMFTransform)(unsafe.Pointer(unknown))
		encoder, configureErr := configureHardwareH264(transform, width, height, fps, bitrate)
		if configureErr == nil {
			return encoder, nil
		}
		transform.Release()
		failures = append(failures, configureErr)
	}
	return nil, fmt.Errorf("Ни один аппаратный H.264 кодировщик не принял поток: %w", errors.Join(failures...))
}

func configureHardwareH264(transform *mf.IMFTransform, width, height, fps, bitrate int) (*hardwareH264Encoder, error) {
	var attributes *mf.IMFAttributes
	if err := transform.GetAttributes(&attributes); err != nil {
		return nil, err
	}
	if err := attributes.SetUINT32(&mf.MF_TRANSFORM_ASYNC_UNLOCK, 1); err != nil {
		attributes.Release()
		return nil, err
	}
	attributes.Release()
	out, err := h264MediaType(width, height, fps, bitrate, true)
	if err != nil {
		return nil, err
	}
	defer out.Release()
	if err := transform.SetOutputType(0, out, 0); err != nil {
		return nil, fmt.Errorf("H.264 output type: %w", err)
	}
	in, err := h264MediaType(width, height, fps, bitrate, false)
	if err != nil {
		return nil, err
	}
	defer in.Release()
	if err := transform.SetInputType(0, in, 0); err != nil {
		return nil, fmt.Errorf("NV12 input type: %w", err)
	}
	var info mf.MFT_OUTPUT_STREAM_INFO
	if err := transform.GetOutputStreamInfo(0, &info); err != nil {
		return nil, err
	}
	events, err := win32.QueryInterface[mf.IMFMediaEventGenerator](transform, &mf.IID_IMFMediaEventGenerator)
	if err != nil {
		return nil, fmt.Errorf("asynchronous events: %w", err)
	}
	if err := transform.ProcessMessage(mf.MFT_MESSAGE_NOTIFY_BEGIN_STREAMING, 0); err != nil {
		events.Release()
		return nil, err
	}
	if err := transform.ProcessMessage(mf.MFT_MESSAGE_NOTIFY_START_OF_STREAM, 0); err != nil {
		events.Release()
		return nil, err
	}
	codecAPI, _ := win32.QueryInterface[mf.ICodecAPI](transform, &mf.IID_ICodecAPI)
	return &hardwareH264Encoder{transform: transform, events: events, outputInfo: info, width: width, height: height, frameDuration: time.Second / time.Duration(fps), codecAPI: codecAPI}, nil
}

func h264MediaType(width, height, fps, bitrate int, encoded bool) (*mf.IMFMediaType, error) {
	var mediaType *mf.IMFMediaType
	if err := mf.MFCreateMediaType(&mediaType); err != nil {
		return nil, err
	}
	subtype := &mf.MFVideoFormat_NV12
	if encoded {
		subtype = &mf.MFVideoFormat_H264
	}
	for _, item := range []struct{ key, value *win32.GUID }{{&mf.MF_MT_MAJOR_TYPE, &mf.MFMediaType_Video}, {&mf.MF_MT_SUBTYPE, subtype}} {
		if err := mediaType.SetGUID(item.key, item.value); err != nil {
			mediaType.Release()
			return nil, err
		}
	}
	for _, item := range []struct {
		key   *win32.GUID
		value uint64
	}{{&mf.MF_MT_FRAME_SIZE, uint64(width)<<32 | uint64(height)}, {&mf.MF_MT_FRAME_RATE, uint64(fps)<<32 | 1}, {&mf.MF_MT_PIXEL_ASPECT_RATIO, uint64(1)<<32 | 1}} {
		if err := mediaType.SetUINT64(item.key, item.value); err != nil {
			mediaType.Release()
			return nil, err
		}
	}
	if err := mediaType.SetUINT32(&mf.MF_MT_INTERLACE_MODE, uint32(mf.MFVideoInterlace_Progressive)); err != nil {
		mediaType.Release()
		return nil, err
	}
	if encoded {
		if err := mediaType.SetUINT32(&mf.MF_MT_AVG_BITRATE, uint32(bitrate)); err != nil {
			mediaType.Release()
			return nil, err
		}
		if err := mediaType.SetUINT32(&mf.MF_MT_MPEG2_PROFILE, uint32(mf.EAVEncH264VProfile_Base)); err != nil {
			mediaType.Release()
			return nil, err
		}
		if err := mediaType.SetUINT32(&mf.MF_MT_MAX_KEYFRAME_SPACING, uint32(max(1, fps))); err != nil {
			mediaType.Release()
			return nil, err
		}
	}
	return mediaType, nil
}

func (e *hardwareH264Encoder) Close() {
	if e == nil {
		return
	}
	_ = e.transform.ProcessMessage(mf.MFT_MESSAGE_NOTIFY_END_OF_STREAM, 0)
	_ = e.transform.ProcessMessage(mf.MFT_MESSAGE_NOTIFY_END_STREAMING, 0)
	e.events.Release()
	if e.codecAPI != nil {
		e.codecAPI.Release()
	}
	e.transform.Release()
	_ = mf.MFShutdown()
	com.CoUninitialize()
}

type encodedH264Frame struct {
	Data []byte
	At   time.Time
}

func codecUint32(value uint32) variant.VARIANT {
	var result variant.VARIANT
	*(*uint16)(unsafe.Pointer(&result)) = uint16(variant.VT_UI4)
	*(*uint32)(unsafe.Add(unsafe.Pointer(&result), 8)) = value
	return result
}

func (e *hardwareH264Encoder) SetBitrate(bitrate int) error {
	if e.codecAPI == nil {
		return errors.New("H.264 bitrate control unavailable")
	}
	value := codecUint32(uint32(bitrate))
	return e.codecAPI.SetValue(&codecMeanBitrate, &value)
}

func (e *hardwareH264Encoder) ForceKeyframe() error {
	if e.codecAPI == nil {
		return errors.New("H.264 keyframe control unavailable")
	}
	value := codecUint32(1)
	return e.codecAPI.SetValue(&codecForceKeyframe, &value)
}

// Poll drains asynchronous MFT events, submits the newest capture when the
// encoder asks for input, and returns any completed H.264 access units.
func (e *hardwareH264Encoder) Poll(nv12 []byte, at time.Time) ([]encodedH264Frame, error) {
	var output []encodedH264Frame
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 64; i++ {
			var event *mf.IMFMediaEvent
			err := e.events.GetEvent(mf.MF_EVENT_FLAG_NO_WAIT, &event)
			if errors.Is(err, win32.HRESULT(mf.MF_E_NO_EVENTS_AVAILABLE)) {
				break
			}
			if err != nil {
				return output, fmt.Errorf("H.264 event: %w", err)
			}
			var kind uint32
			err = event.GetType(&kind)
			event.Release()
			if err != nil {
				return output, err
			}
			switch kind {
			case uint32(mf.METransformNeedInput):
				e.inputRequests++
			case uint32(mf.METransformHaveOutput):
				frame, err := e.readOutput()
				if err != nil {
					return output, err
				}
				if len(frame.Data) > 0 {
					output = append(output, frame)
				}
			}
		}
		if pass == 0 && nv12 != nil && e.inputRequests > 0 {
			if err := e.writeInput(nv12, at); err != nil {
				return output, err
			}
			e.inputRequests--
		}
	}
	return output, nil
}

func (e *hardwareH264Encoder) writeInput(nv12 []byte, at time.Time) error {
	want := e.width * e.height * 3 / 2
	if len(nv12) != want {
		return fmt.Errorf("NV12 input has %d bytes, expected %d", len(nv12), want)
	}
	var buffer *mf.IMFMediaBuffer
	if err := mf.MFCreateMemoryBuffer(uint32(want), &buffer); err != nil {
		return err
	}
	defer buffer.Release()
	var data *byte
	if err := buffer.Lock(&data, nil, nil); err != nil {
		return err
	}
	copy(unsafe.Slice(data, want), nv12)
	if err := buffer.Unlock(); err != nil {
		return err
	}
	if err := buffer.SetCurrentLength(uint32(want)); err != nil {
		return err
	}
	var sample *mf.IMFSample
	if err := mf.MFCreateSample(&sample); err != nil {
		return err
	}
	defer sample.Release()
	if err := sample.AddBuffer(buffer); err != nil {
		return err
	}
	if e.base.IsZero() {
		e.base = at
	}
	if err := sample.SetSampleTime(at.Sub(e.base).Nanoseconds() / 100); err != nil {
		return err
	}
	duration := e.frameDuration
	if !e.previousInput.IsZero() && at.After(e.previousInput) {
		duration = at.Sub(e.previousInput)
	}
	e.previousInput = at
	if err := sample.SetSampleDuration(int64(duration) / 100); err != nil {
		return err
	}
	return e.transform.ProcessInput(0, sample, 0)
}

func (e *hardwareH264Encoder) readOutput() (encodedH264Frame, error) {
	var supplied *mf.IMFSample
	if e.outputInfo.DwFlags&uint32(mf.MFT_OUTPUT_STREAM_PROVIDES_SAMPLES|mf.MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES) == 0 {
		var buffer *mf.IMFMediaBuffer
		capacity := max(e.outputInfo.CbSize, uint32(e.width*e.height*2))
		if err := mf.MFCreateMemoryBuffer(capacity, &buffer); err != nil {
			return encodedH264Frame{}, err
		}
		if err := mf.MFCreateSample(&supplied); err != nil {
			buffer.Release()
			return encodedH264Frame{}, err
		}
		if err := supplied.AddBuffer(buffer); err != nil {
			buffer.Release()
			supplied.Release()
			return encodedH264Frame{}, err
		}
		buffer.Release()
	}
	output := []mf.MFT_OUTPUT_DATA_BUFFER{{DwStreamID: 0, PSample: supplied}}
	var status uint32
	err := e.transform.ProcessOutput(0, output, &status)
	if output[0].PEvents != nil {
		output[0].PEvents.Release()
	}
	if err != nil {
		if supplied != nil {
			supplied.Release()
		}
		return encodedH264Frame{}, fmt.Errorf("H.264 output: %w", err)
	}
	sample := output[0].PSample
	if sample == nil {
		if supplied != nil {
			supplied.Release()
		}
		return encodedH264Frame{}, errors.New("H.264 returned no output sample")
	}
	defer sample.Release()
	var buffer *mf.IMFMediaBuffer
	if err := sample.ConvertToContiguousBuffer(&buffer); err != nil {
		return encodedH264Frame{}, err
	}
	defer buffer.Release()
	var data *byte
	var length uint32
	if err := buffer.Lock(&data, nil, &length); err != nil {
		return encodedH264Frame{}, err
	}
	encoded := append([]byte(nil), unsafe.Slice(data, int(length))...)
	if err := buffer.Unlock(); err != nil {
		return encodedH264Frame{}, err
	}
	var sampleTime int64
	if err := sample.GetSampleTime(&sampleTime); err != nil {
		sampleTime = 0
	}
	return encodedH264Frame{Data: encoded, At: e.base.Add(time.Duration(sampleTime) * 100 * time.Nanosecond)}, nil
}

// hardwareH264Activations returns only genuine hardware encoder MFTs. Windows
// registers these separately from its software H.264 encoder.
func hardwareH264Activations() ([]*mf.IMFActivate, error) {
	output := mf.MFT_REGISTER_TYPE_INFO{GuidMajorType: mf.MFMediaType_Video, GuidSubtype: mf.MFVideoFormat_H264}
	var list **mf.IMFActivate
	var count uint32
	flags := uint32(mf.MFT_ENUM_FLAG_HARDWARE | mf.MFT_ENUM_FLAG_SORTANDFILTER)
	if err := mf.MFTEnumEx(mf.MFT_CATEGORY_VIDEO_ENCODER, flags, nil, &output, &list, &count); err != nil {
		return nil, err
	}
	if list == nil {
		return nil, nil
	}
	activations := append([]*mf.IMFActivate(nil), unsafe.Slice(list, int(count))...)
	com.CoTaskMemFree(unsafe.Pointer(list))
	return activations, nil
}

func countHardwareH264() (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := com.CoInitializeEx(0); err != nil {
		return 0, fmt.Errorf("COM: %w", err)
	}
	defer com.CoUninitialize()
	if err := mf.MFStartup(mf.MF_VERSION, 0); err != nil {
		return 0, fmt.Errorf("Media Foundation: %w", err)
	}
	defer mf.MFShutdown()
	list, err := hardwareH264Activations()
	if err != nil {
		return 0, err
	}
	for _, activation := range list {
		activation.Release()
	}
	return len(list), nil
}
