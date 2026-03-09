package webrtc

import (
	"errors"
	"io"
	"log"

	"stream-sniff/internal/h264"

	"github.com/pion/webrtc/v4"
)

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	if remoteTrack.Codec().MimeType != webrtc.MimeTypeH264 {
		return
	}

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

func WHIP(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken, func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string) {
		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			go readAndLogRTP(bearerToken, sessionID, remoteTrack)
		})
	})
}
