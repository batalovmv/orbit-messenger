package sfu

import (
	"github.com/pion/webrtc/v4"
)

// newAPI creates a Pion API instance with a MediaEngine restricted to the
// codecs we ship in production: Opus for audio and VP8 for video.
//
// Why VP8 and not VP9/H264:
//   - VP8 has the broadest browser support and behaves the same across Chrome,
//     Firefox and Safari without quirks.
//   - VP9/H264 require codec preferences negotiation that adds complexity for
//     no perceptible quality gain at the bitrates we use (≤1.5 Mbps per peer).
//   - Switching to VP9/H264 later is a one-line addition once a real need
//     appears (e.g. >10 participant SFU sessions).
func newAPI() (*webrtc.API, error) {
	m := &webrtc.MediaEngine{}

	// Opus — RTP payload type 111 is the de-facto default in browsers
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}

	// VP8 — payload type 96 matches what browsers send by default
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP8,
			ClockRate:   90000,
			SDPFmtpLine: "",
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}

	return webrtc.NewAPI(webrtc.WithMediaEngine(m)), nil
}
