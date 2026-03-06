package webrtc

import (
	"errors"
	"io"
	"log"

	internalh264 "stream-sniff/internal/h264"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	h264Packet := &codecs.H264Packet{}

	for {
		packet, _, err := remoteTrack.ReadRTP()
		switch {
		case errors.Is(err, io.EOF):
			return
		case err != nil:
			log.Printf("bearerToken=%s session=%s track-end kind=%s err=%v", bearerToken, sessionID, remoteTrack.Kind().String(), err)
			return
		}

		if remoteTrack.Codec().MimeType != webrtc.MimeTypeH264 {
			continue
		}

		unmarshaledPayload, unmarshalErr := h264Packet.Unmarshal(packet.Payload)
		if unmarshalErr != nil || len(unmarshaledPayload) == 0 {
			continue
		}

		for _, nalu := range internalh264.SplitAnnexBNALUs(unmarshaledPayload) {
			if len(nalu) == 0 {
				continue
			}

			naluType := nalu[0] & 0x1F
			switch naluType {
			case 7:
				sps, parseErr := internalh264.ParseSPSInfo(nalu)
				if parseErr != nil {
					log.Printf("bearerToken=%s session=%s sps-parse err=%v", bearerToken, sessionID, parseErr)
					continue
				}

				writeAnalyzeMessage(
					bearerToken,
					"sps profile_idc=%d level_idc=%d sps_id=%d width=%d height=%d",
					sps.ProfileIDC,
					sps.LevelIDC,
					sps.SPSID,
					sps.Width,
					sps.Height,
				)
			case 8:
				pps, parseErr := internalh264.ParsePPSInfo(nalu)
				if parseErr != nil {
					log.Printf("bearerToken=%s session=%s pps-parse err=%v", bearerToken, sessionID, parseErr)
					continue
				}

				writeAnalyzeMessage(
					bearerToken,
					"pps pps_id=%d sps_id=%d entropy=%s",
					pps.PPSID,
					pps.SPSID,
					pps.EntropyCoding,
				)
			}
		}
	}
}

func WHIP(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken, func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string) {
		peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			go readAndLogRTP(bearerToken, sessionID, remoteTrack)
		})
	})
}
