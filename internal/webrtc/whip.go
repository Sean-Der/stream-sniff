package webrtc

import (
	"errors"
	"io"
	"log"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	log.Printf(
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
			log.Printf("bearerToken=%s session=%s track-end kind=%s err=eof", bearerToken, sessionID, remoteTrack.Kind().String())
			return
		case err != nil:
			log.Printf("bearerToken=%s session=%s track-end kind=%s err=%v", bearerToken, sessionID, remoteTrack.Kind().String(), err)
			return
		}

		log.Printf(
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
	return negotiateOffer(offer, bearerToken)
}

func Analyze(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken)
}

func negotiateOffer(offer, bearerToken string) (string, error) {
	maybePrintOfferAnswer(offer, true)

	sessionID := uuid.NewString()
	peerConnection, err := newPeerConnection(apiWhip)
	if err != nil {
		return "", err
	}
	storeSession(sessionID, peerConnection)

	cleanup := func() {
		forgetSession(sessionID)
		if closeErr := peerConnection.Close(); closeErr != nil {
			log.Printf("bearerToken=%s session=%s close err=%v", bearerToken, sessionID, closeErr)
		}
	}

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go readAndLogRTP(bearerToken, sessionID, remoteTrack)
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("bearerToken=%s session=%s peer-connection=%s", bearerToken, sessionID, state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			forgetSession(sessionID)
		}
	})

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		SDP:  offer,
		Type: webrtc.SDPTypeOffer,
	}); err != nil {
		cleanup()
		return "", err
	}

	return createAnswer(peerConnection, cleanup)
}

func createAnswer(peerConnection *webrtc.PeerConnection, cleanup func()) (string, error) {
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		cleanup()
		return "", err
	}

	if err = peerConnection.SetLocalDescription(answer); err != nil {
		cleanup()
		return "", err
	}

	<-gatherComplete
	localDescription := peerConnection.LocalDescription()
	if localDescription == nil {
		cleanup()
		return "", errors.New("missing local description")
	}

	return maybePrintOfferAnswer(appendAnswer(localDescription.SDP), false), nil
}
