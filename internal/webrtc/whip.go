package webrtc

import (
	"errors"
	"io"
	"log"
	"os"
	"strconv"

	"stream-sniff/internal/audio"
	"stream-sniff/internal/h264"

	"github.com/pion/webrtc/v4"
)

func readAndLogVideoRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	analyzer := h264.NewAnalyzer()
	for {
		packet, _, err := remoteTrack.ReadRTP()
		switch {
		case errors.Is(err, io.EOF):
			return
		case err != nil:
			log.Printf("bearerToken=%s session=%s track-end kind=%s err=%v", bearerToken, sessionID, remoteTrack.Kind().String(), err)
			return
		}

		writeAnalyzeMessage(bearerToken, analyzer.WriteRTP(packet))
	}
}

func readAndLogAudioRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	windowSeconds := 5
	if v := os.Getenv("ANALYSIS_WINDOW_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			windowSeconds = parsed
		}
	}

	analyzer := audio.NewAnalyzer(windowSeconds)
	for {
		packet, _, err := remoteTrack.ReadRTP()
		switch {
		case errors.Is(err, io.EOF):
			return
		case err != nil:
			log.Printf("bearerToken=%s session=%s track-end kind=%s err=%v", bearerToken, sessionID, remoteTrack.Kind().String(), err)
			return
		}

		writeAnalyzeMessage(bearerToken, analyzer.WriteRTP(packet))
	}
}

func WHIP(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken, func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string) {
		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			switch remoteTrack.Codec().MimeType {
			case webrtc.MimeTypeH264:
				go readAndLogVideoRTP(bearerToken, sessionID, remoteTrack)
			case webrtc.MimeTypeOpus:
				go readAndLogAudioRTP(bearerToken, sessionID, remoteTrack)
			}
		})
	})
}
