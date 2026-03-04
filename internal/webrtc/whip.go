package webrtc

import (
	"errors"
	"io"

	"github.com/pion/webrtc/v4"
)

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	writeAnalyzeMessage(
		bearerToken,
		"bearerToken=%s session=%s track-start kind=%s id=%s rid=%q stream=%s codec=%s payload=%d clock=%d ssrc=%d",
		bearerToken,
		sessionID,
		remoteTrack.Kind().String(),
		remoteTrack.ID(),
		remoteTrack.RID(),
		remoteTrack.StreamID(),
		remoteTrack.Codec().MimeType,
		remoteTrack.Codec().PayloadType,
		remoteTrack.Codec().ClockRate,
		remoteTrack.SSRC(),
	)

	for {
		packet, _, err := remoteTrack.ReadRTP()
		switch {
		case errors.Is(err, io.EOF):
			writeAnalyzeMessage(bearerToken, "bearerToken=%s session=%s track-end kind=%s err=eof", bearerToken, sessionID, remoteTrack.Kind().String())
			return
		case err != nil:
			writeAnalyzeMessage(
				bearerToken,
				"bearerToken=%s session=%s track-end kind=%s err=%v",
				bearerToken,
				sessionID,
				remoteTrack.Kind().String(),
				err,
			)
			return
		}

		writeAnalyzeMessage(
			bearerToken,
			"bearerToken=%s session=%s kind=%s seq=%d ts=%d marker=%t payload=%d ssrc=%d",
			bearerToken,
			sessionID,
			remoteTrack.Kind().String(),
			packet.SequenceNumber,
			packet.Timestamp,
			packet.Marker,
			len(packet.Payload),
			packet.SSRC,
		)
	}
}

func WHIP(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken, func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string) {
		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			go readAndLogRTP(bearerToken, sessionID, remoteTrack)
		})
	})
}
