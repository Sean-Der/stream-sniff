package webrtc

import (
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	internalh264 "stream-sniff/internal/h264"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
)

func formatBitrate(bitsPerSecond float64) string {
	if bitsPerSecond >= 1_000_000 {
		return fmt.Sprintf("%.2f Mbps", bitsPerSecond/1_000_000)
	}

	return fmt.Sprintf("%.1f kbps", bitsPerSecond/1_000)
}

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	h264Packet := &codecs.H264Packet{}
	trackStartedAt := time.Now()
	lastBitrateEmissionAt := trackStartedAt
	totalBits := 0

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

		totalBits += len(packet.Payload) * 8
		if time.Since(lastBitrateEmissionAt) >= time.Second {
			elapsedSeconds := time.Since(trackStartedAt).Seconds()
			if elapsedSeconds > 0 {
				averageBitsPerSecond := float64(totalBits) / elapsedSeconds
				writeAnalyzeMessage(
					bearerToken,
					[]analysisItem{
						{
							ID:      "average_bitrate",
							Label:   "information",
							Message: fmt.Sprintf("Average video bitrate is %s.", formatBitrate(averageBitsPerSecond)),
						},
					},
				)
			}
			lastBitrateEmissionAt = time.Now()
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
					[]analysisItem{
						{
							ID:      "resolution",
							Label:   "information",
							Message: fmt.Sprintf("Your video resolution is %dx%d.", sps.Width, sps.Height),
						},
						{
							ID:      "profile_level",
							Label:   "information",
							Message: fmt.Sprintf("Video format: H.264 %s, level %s.", sps.ProfileName(), sps.LevelName()),
						},
					},
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
