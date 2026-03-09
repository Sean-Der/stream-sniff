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

func colorForAverageQP(averageQP float64) string {
	switch {
	case averageQP <= 28:
		return "rgba(34, 197, 94, 0.22)"
	case averageQP <= 35:
		return "rgba(249, 115, 22, 0.22)"
	default:
		return "rgba(239, 68, 68, 0.22)"
	}
}

func colorForBitsPerPixelPerFrame(bitsPerPixelPerFrame float64) string {
	switch {
	case bitsPerPixelPerFrame < 0.05:
		return "rgba(239, 68, 68, 0.22)"
	case bitsPerPixelPerFrame < 0.10:
		return "rgba(249, 115, 22, 0.22)"
	case bitsPerPixelPerFrame < 0.20:
		return "rgba(250, 204, 21, 0.22)"
	default:
		return "rgba(34, 197, 94, 0.22)"
	}
}

func colorForAverageKeyframeInterval(seconds float64) string {
	switch {
	case seconds < 2:
		return "rgba(34, 197, 94, 0.22)"
	case seconds < 5:
		return "rgba(250, 204, 21, 0.22)"
	default:
		return "rgba(239, 68, 68, 0.22)"
	}
}

func recommendationItem(id, message string) analysisItem {
	return analysisItem{
		ID:      id,
		Message: message,
		Kind:    "recommendation",
	}
}

func readAndLogRTP(bearerToken, sessionID string, remoteTrack *webrtc.TrackRemote) {
	h264Packet := &codecs.H264Packet{}
	trackStartedAt := time.Now()
	lastBitrateEmissionAt := trackStartedAt
	totalBits := 0
	totalFrames := 0
	hasLastTimestamp := false
	lastTimestamp := uint32(0)
	currentFrameSliceClass := ""
	iFrames := 0
	pFrames := 0
	bFrames := 0
	totalQP := 0
	qpSamples := 0
	currentWidth := 0
	currentHeight := 0
	lastKeyframeAt := time.Time{}
	totalKeyframeIntervalSeconds := 0.0
	keyframeIntervals := 0
	bFramesDetected := false
	spsByID := map[int]internalh264.SPSInfo{}
	ppsByID := map[int]internalh264.PPSInfo{}

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

		if !hasLastTimestamp || packet.Timestamp != lastTimestamp {
			if hasLastTimestamp {
				switch currentFrameSliceClass {
				case "I":
					iFrames++
				case "P":
					pFrames++
				case "B":
					bFrames++
				}
			}

			totalFrames++
			hasLastTimestamp = true
			lastTimestamp = packet.Timestamp
			currentFrameSliceClass = ""
		}

		totalBits += len(packet.Payload) * 8
		if time.Since(lastBitrateEmissionAt) >= time.Second {
			elapsedSeconds := time.Since(trackStartedAt).Seconds()
			if elapsedSeconds > 0 {
				averageBitsPerSecond := float64(totalBits) / elapsedSeconds
				analyses := []analysisItem{
					{
						ID:      "average_bitrate",
						Label:   "Average Bitrate",
						Message: formatBitrate(averageBitsPerSecond),
					},
				}
				recommendations := []analysisItem{}

				if qpSamples > 0 {
					averageQP := float64(totalQP) / float64(qpSamples)
					analyses = append(analyses, analysisItem{
						ID:      "average_qp",
						Label:   "Average Quantization Parameter (QP)",
						Message: fmt.Sprintf("%.1f", averageQP),
						Color:   colorForAverageQP(averageQP),
					})

					switch {
					case averageQP > 35:
						recommendations = append(recommendations, recommendationItem(
							"rec_compression",
							"Compression looks strong. Increase bitrate, lower resolution or increase H264 profile to preserve detail.",
						))
					case averageQP > 28:
						recommendations = append(recommendations, recommendationItem(
							"rec_compression",
							"Compression is noticeable. Increase bitrate, lower resolution or increase H264 profile to preserve detail.",
						))
					default:
						recommendations = append(recommendations, recommendationItem(
							"rec_compression",
							"",
						))
					}
				}

				averageFPS := float64(totalFrames) / elapsedSeconds
				if currentWidth > 0 && currentHeight > 0 && averageFPS > 0 {
					bitsPerPixelPerFrame := averageBitsPerSecond / (float64(currentWidth*currentHeight) * averageFPS)
					analyses = append(analyses, analysisItem{
						ID:      "bits_per_pixel_per_frame",
						Label:   "Bits Per Pixel Per Frame",
						Message: fmt.Sprintf("%.3f", bitsPerPixelPerFrame),
						Color:   colorForBitsPerPixelPerFrame(bitsPerPixelPerFrame),
					})
				}

				if keyframeIntervals > 0 {
					averageKeyframeInterval := totalKeyframeIntervalSeconds / float64(keyframeIntervals)
					analyses = append(analyses, analysisItem{
						ID:      "average_time_between_keyframes",
						Label:   "Average Time Between Keyframes",
						Message: fmt.Sprintf("%.1fs", averageKeyframeInterval),
						Color:   colorForAverageKeyframeInterval(averageKeyframeInterval),
					})

					switch {
					case averageKeyframeInterval >= 5:
						recommendations = append(recommendations, recommendationItem(
							"rec_keyframe_interval_shorter",
							"Keyframes are far apart, set to a lower value so your stream starts faster.",
						))
					case averageKeyframeInterval >= 2:
						recommendations = append(recommendations, recommendationItem(
							"rec_keyframe_interval_target",
							"Keyframes are far apart, set to a lower value so your stream starts faster.",
						))
					}
				}

				displayIFrames := iFrames
				displayPFrames := pFrames
				displayBFrames := bFrames
				switch currentFrameSliceClass {
				case "I":
					displayIFrames++
				case "P":
					displayPFrames++
				case "B":
					displayBFrames++
				}
				totalClassifiedFrames := displayIFrames + displayPFrames + displayBFrames
				if totalClassifiedFrames > 0 {
					analyses = append(analyses, analysisItem{
						ID:    "frame_type_distribution",
						Label: "Frame Type Distribution",
						Message: fmt.Sprintf(
							"I: %.1f%%, P: %.1f%%, B: %.1f%%",
							(float64(displayIFrames)/float64(totalClassifiedFrames))*100,
							(float64(displayPFrames)/float64(totalClassifiedFrames))*100,
							(float64(displayBFrames)/float64(totalClassifiedFrames))*100,
						),
					})
				}

				if bFramesDetected {
					analyses = append(analyses, analysisItem{
						ID:      "b_frames_detected",
						Label:   "B-Frames detected",
						Message: "B-Frames detected",
						Color:   "rgba(239, 68, 68, 0.22)",
					})
					recommendations = append(recommendations, recommendationItem(
						"rec_disable_b_frames",
						"B-Frames are enabled. Disable B-Frames when you need lower latency.",
					))
				}

				analyses = append(analyses, recommendations...)
				writeAnalyzeMessage(
					bearerToken,
					analyses,
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
				spsByID[sps.SPSID] = sps
				currentWidth = sps.Width
				currentHeight = sps.Height

				writeAnalyzeMessage(
					bearerToken,
					[]analysisItem{
						{
							ID:      "resolution",
							Label:   "Resolution",
							Message: fmt.Sprintf("%dx%d", sps.Width, sps.Height),
						},
						{
							ID:      "profile_level",
							Label:   "Profile Level",
							Message: fmt.Sprintf("H.264 %s, level %s.", sps.ProfileName(), sps.LevelName()),
						},
					},
				)
			case 8:
				pps, parseErr := internalh264.ParsePPSInfo(nalu)
				if parseErr != nil {
					log.Printf("bearerToken=%s session=%s pps-parse err=%v", bearerToken, sessionID, parseErr)
					continue
				}
				ppsByID[pps.PPSID] = pps
			case 1, 5:
				sliceClass, hasSliceClass := internalh264.ParseSliceClass(nalu)
				if hasSliceClass {
					switch {
					case currentFrameSliceClass == "":
						currentFrameSliceClass = sliceClass
					case sliceClass == "B":
						currentFrameSliceClass = "B"
					case sliceClass == "I" && currentFrameSliceClass == "P":
						currentFrameSliceClass = "I"
					}

					if !bFramesDetected && sliceClass == "B" {
						bFramesDetected = true
						writeAnalyzeMessage(
							bearerToken,
							[]analysisItem{
								{
									ID:      "b_frames_detected",
									Label:   "B-Frames detected",
									Message: "B-Frames detected",
									Color:   "rgba(239, 68, 68, 0.22)",
								},
								recommendationItem(
									"rec_disable_b_frames",
									"B-Frames are enabled. Disable B-Frames when you need lower latency.",
								),
							},
						)
					}
				}

				if naluType == 5 {
					now := time.Now()
					if !lastKeyframeAt.IsZero() {
						totalKeyframeIntervalSeconds += now.Sub(lastKeyframeAt).Seconds()
						keyframeIntervals++
					}
					lastKeyframeAt = now
				}

				qp, ok := internalh264.ParseSliceQP(nalu, spsByID, ppsByID)
				if !ok {
					continue
				}

				totalQP += qp
				qpSamples++
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
