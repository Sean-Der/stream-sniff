package h264

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

type analysisItem struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Message string `json:"message"`
	Color   string `json:"color,omitempty"`
}

type payload struct {
	Type            string            `json:"type"`
	Analyses        []analysisItem    `json:"analyses"`
	Recommendations map[string]string `json:"recommendations"`
}

// Analyzer consumes H264 RTP packets and emits frontend JSON payloads.
type Analyzer struct {
	h264Packet codecs.H264Packet

	started        bool
	trackStartedAt time.Time
	lastEmissionAt time.Time

	totalBits        int
	totalFrames      int
	hasLastTimestamp bool
	lastTimestamp    uint32

	currentFrameSliceClass string
	iFrames                int
	pFrames                int
	bFrames                int

	totalQP   int
	qpSamples int

	lastKeyframeAt               time.Time
	totalKeyframeIntervalSeconds float64
	keyframeIntervals            int

	hasLatestSPS bool
	latestSPS    SPSInfo
	spsByID      map[int]SPSInfo
	ppsByID      map[int]PPSInfo

	bFramesDetected bool
}

func NewAnalyzer() *Analyzer {
	return &Analyzer{
		spsByID: map[int]SPSInfo{},
		ppsByID: map[int]PPSInfo{},
	}
}

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

func (a *Analyzer) WriteRTP(packet *rtp.Packet) []byte {
	if packet == nil {
		return nil
	}

	now := time.Now()
	if !a.started {
		a.started = true
		a.trackStartedAt = now
		a.lastEmissionAt = now
	}

	if !a.hasLastTimestamp || packet.Timestamp != a.lastTimestamp {
		if a.hasLastTimestamp {
			switch a.currentFrameSliceClass {
			case "I":
				a.iFrames++
			case "P":
				a.pFrames++
			case "B":
				a.bFrames++
			}
		}

		a.totalFrames++
		a.hasLastTimestamp = true
		a.lastTimestamp = packet.Timestamp
		a.currentFrameSliceClass = ""
	}

	a.totalBits += len(packet.Payload) * 8

	shouldEmit := false
	if payload, err := a.h264Packet.Unmarshal(packet.Payload); err == nil && len(payload) > 0 {
		for _, nalu := range SplitAnnexBNALUs(payload) {
			if len(nalu) == 0 {
				continue
			}

			naluType := nalu[0] & 0x1F
			switch naluType {
			case 7:
				sps, err := ParseSPSInfo(nalu)
				if err != nil {
					continue
				}

				a.spsByID[sps.SPSID] = sps
				a.latestSPS = sps
				a.hasLatestSPS = true
				shouldEmit = true
			case 8:
				pps, err := ParsePPSInfo(nalu)
				if err != nil {
					continue
				}

				a.ppsByID[pps.PPSID] = pps
			case 1, 5:
				sliceClass, ok := ParseSliceClass(nalu)
				if ok {
					switch {
					case a.currentFrameSliceClass == "":
						a.currentFrameSliceClass = sliceClass
					case sliceClass == "B":
						a.currentFrameSliceClass = "B"
					case sliceClass == "I" && a.currentFrameSliceClass == "P":
						a.currentFrameSliceClass = "I"
					}

					if !a.bFramesDetected && sliceClass == "B" {
						a.bFramesDetected = true
						shouldEmit = true
					}
				}

				if naluType == 5 {
					if !a.lastKeyframeAt.IsZero() {
						a.totalKeyframeIntervalSeconds += now.Sub(a.lastKeyframeAt).Seconds()
						a.keyframeIntervals++
					}

					a.lastKeyframeAt = now
				}

				if qp, ok := ParseSliceQP(nalu, a.spsByID, a.ppsByID); ok {
					a.totalQP += qp
					a.qpSamples++
				}
			}
		}
	}

	if !shouldEmit && now.Sub(a.lastEmissionAt) < time.Second {
		return nil
	}

	state := a.items(now)
	message, err := json.Marshal(state)
	if err != nil {
		return nil
	}

	a.lastEmissionAt = now

	return message
}

func (a *Analyzer) items(now time.Time) payload {
	state := payload{
		Type:            "video",
		Analyses:        []analysisItem{},
		Recommendations: map[string]string{},
	}

	elapsedSeconds := now.Sub(a.trackStartedAt).Seconds()
	if elapsedSeconds < 1 {
		if a.bFramesDetected {
			state.Analyses = append(state.Analyses,
				analysisItem{
					ID:      "b_frames_detected",
					Label:   "B-Frames detected",
					Message: "B-Frames detected",
					Color:   "rgba(239, 68, 68, 0.22)",
				},
			)
			state.Recommendations["rec_disable_b_frames"] = "B-Frames are enabled. Disable B-Frames when you need lower latency."
		}

		if a.hasLatestSPS {
			state.Analyses = append(state.Analyses,
				analysisItem{
					ID:      "resolution",
					Label:   "Resolution",
					Message: fmt.Sprintf("%dx%d", a.latestSPS.Width, a.latestSPS.Height),
				},
				analysisItem{
					ID:      "profile_level",
					Label:   "Profile Level",
					Message: fmt.Sprintf("H.264 %s, level %s.", a.latestSPS.ProfileName(), a.latestSPS.LevelName()),
				},
			)
		}

		return state
	}

	averageBitsPerSecond := float64(a.totalBits) / elapsedSeconds
	state.Analyses = append(state.Analyses, analysisItem{
		ID:      "average_bitrate",
		Label:   "Average Bitrate",
		Message: formatBitrate(averageBitsPerSecond),
	})

	if a.qpSamples > 0 {
		averageQP := float64(a.totalQP) / float64(a.qpSamples)
		state.Analyses = append(state.Analyses, analysisItem{
			ID:      "average_qp",
			Label:   "Average Quantization Parameter (QP)",
			Message: fmt.Sprintf("%.1f", averageQP),
			Color:   colorForAverageQP(averageQP),
		})

		switch {
		case averageQP > 35:
			state.Recommendations["rec_compression"] = "Strong Compression. Increase bitrate and lower resolution/framerate."
		case averageQP > 28:
			state.Recommendations["rec_compression"] = "noticeable Compression. Increase bitrate and lower resolution/framerate."
		}
	}

	averageFPS := float64(a.totalFrames) / elapsedSeconds
	if averageFPS > 0 {
		state.Analyses = append(state.Analyses, analysisItem{
			ID:      "frame_rate",
			Label:   "Frame Rate",
			Message: fmt.Sprintf("%.1f fps", averageFPS),
		})
	}

	if a.hasLatestSPS && averageFPS > 0 {
		bitsPerPixelPerFrame := averageBitsPerSecond / (float64(a.latestSPS.Width*a.latestSPS.Height) * averageFPS)
		state.Analyses = append(state.Analyses, analysisItem{
			ID:      "bits_per_pixel_per_frame",
			Label:   "Bits Per Pixel Per Frame",
			Message: fmt.Sprintf("%.3f", bitsPerPixelPerFrame),
			Color:   colorForBitsPerPixelPerFrame(bitsPerPixelPerFrame),
		})
	}

	if a.keyframeIntervals > 0 {
		averageKeyframeInterval := a.totalKeyframeIntervalSeconds / float64(a.keyframeIntervals)
		state.Analyses = append(state.Analyses, analysisItem{
			ID:      "average_time_between_keyframes",
			Label:   "Average Time Between Keyframes",
			Message: fmt.Sprintf("%.1fs", averageKeyframeInterval),
			Color:   colorForAverageKeyframeInterval(averageKeyframeInterval),
		})

		if averageKeyframeInterval >= 2 {
			state.Recommendations["rec_keyframe_interval"] = "Keyframes are far apart, set to a lower value so your stream starts faster."
		}
	}

	displayIFrames := a.iFrames
	displayPFrames := a.pFrames
	displayBFrames := a.bFrames
	switch a.currentFrameSliceClass {
	case "I":
		displayIFrames++
	case "P":
		displayPFrames++
	case "B":
		displayBFrames++
	}

	totalClassifiedFrames := displayIFrames + displayPFrames + displayBFrames
	if totalClassifiedFrames > 0 {
		state.Analyses = append(state.Analyses, analysisItem{
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

	if a.bFramesDetected {
		state.Analyses = append(state.Analyses,
			analysisItem{
				ID:      "b_frames_detected",
				Label:   "B-Frames detected",
				Message: "B-Frames detected",
				Color:   "rgba(239, 68, 68, 0.22)",
			},
		)
		state.Recommendations["rec_disable_b_frames"] = "B-Frames are enabled. Disable B-Frames when you need lower latency."
	}

	if a.hasLatestSPS {
		state.Analyses = append(state.Analyses,
			analysisItem{
				ID:      "resolution",
				Label:   "Resolution",
				Message: fmt.Sprintf("%dx%d", a.latestSPS.Width, a.latestSPS.Height),
			},
			analysisItem{
				ID:      "profile_level",
				Label:   "Profile Level",
				Message: fmt.Sprintf("H.264 %s, level %s.", a.latestSPS.ProfileName(), a.latestSPS.LevelName()),
			},
		)
	}

	return state
}
