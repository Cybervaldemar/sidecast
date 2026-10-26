package media

import (
	"strings"

	"github.com/pion/webrtc/v4"
)

// configureAudio runs after the remote offer is installed so negotiated payload
// types are preserved. A sender's maxBitrate alone does not request high quality:
// Opus defaults to mono unless its receiver explicitly advertises stereo.
// These parameters also describe the unchanged stereo stream sent to viewers.
func configureAudio(pc *webrtc.PeerConnection) error {
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeAudio {
			continue
		}
		var codecs []webrtc.RTPCodecParameters
		if receiver := transceiver.Receiver(); receiver != nil {
			codecs = receiver.GetParameters().Codecs
		} else if sender := transceiver.Sender(); sender != nil {
			codecs = sender.GetParameters().Codecs
		}
		for i := range codecs {
			if strings.EqualFold(codecs[i].MimeType, webrtc.MimeTypeOpus) {
				codecs[i].SDPFmtpLine = gameAudioFMTP(codecs[i].SDPFmtpLine)
			}
		}
		if err := transceiver.SetCodecPreferences(codecs); err != nil {
			return err
		}
	}
	return nil
}

func gameAudioFMTP(line string) string {
	preferences := []string{
		"stereo=1", "sprop-stereo=1", "maxaveragebitrate=256000",
		"maxplaybackrate=48000", "useinbandfec=1", "usedtx=0",
	}
	var preserved []string
	for _, parameter := range strings.Split(line, ";") {
		parameter = strings.TrimSpace(parameter)
		key, _, _ := strings.Cut(parameter, "=")
		replaced := false
		for _, preference := range preferences {
			name, _, _ := strings.Cut(preference, "=")
			if strings.EqualFold(key, name) {
				replaced = true
				break
			}
		}
		if parameter != "" && !replaced {
			preserved = append(preserved, parameter)
		}
	}
	return strings.Join(append(preserved, preferences...), ";")
}
