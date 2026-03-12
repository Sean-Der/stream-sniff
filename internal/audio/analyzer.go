package audio

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/pion/rtp"
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

type snapshot struct {
	rmsDBFS  float64
	peakDBFS float64
}

const (
	dbfsFloor             = -96.0
	clipThresh            = 0.99
	silenceDBFS           = -60.0
	audioLevelExtensionID = 1
)

type Analyzer struct {
	windowSize int

	snapshots []snapshot
	snapCount int
	snapIdx   int

	sumSquares float64
	peakAbs    float64
	samples    int

	clippingSamples int
	silentSnapshots int

	started        bool
	trackStartedAt time.Time
	lastEmissionAt time.Time
}

func NewAnalyzer(windowSeconds int) *Analyzer {
	if windowSeconds < 1 {
		windowSeconds = 5
	}
	return &Analyzer{
		windowSize: windowSeconds,
		snapshots:  make([]snapshot, windowSeconds),
	}
}

func toDBFS(linear float64) float64 {
	if linear <= 0 {
		return dbfsFloor
	}
	db := 20 * math.Log10(linear)
	if db < dbfsFloor {
		return dbfsFloor
	}
	return db
}

func formatDBFS(db float64) string {
	if db <= dbfsFloor {
		return fmt.Sprintf("%.1f dBFS (silence)", db)
	}
	return fmt.Sprintf("%.1f dBFS", db)
}

func colorForRMS(db float64) string {
	switch {
	case db > -3:
		return "rgba(239, 68, 68, 0.22)"
	case db > -6:
		return "rgba(249, 115, 22, 0.22)"
	case db > -24:
		return "rgba(34, 197, 94, 0.22)"
	case db > silenceDBFS:
		return "rgba(250, 204, 21, 0.22)"
	default:
		return "rgba(239, 68, 68, 0.22)"
	}
}

func colorForPeak(db float64) string {
	switch {
	case db > -1:
		return "rgba(239, 68, 68, 0.22)"
	case db > -3:
		return "rgba(249, 115, 22, 0.22)"
	default:
		return "rgba(34, 197, 94, 0.22)"
	}
}

func colorForClipping(count int) string {
	if count > 0 {
		return "rgba(239, 68, 68, 0.22)"
	}
	return "rgba(34, 197, 94, 0.22)"
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

	var linear float64
	if ext := packet.GetExtension(audioLevelExtensionID); len(ext) >= 1 {
		level := ext[0] & 0x7F
		dbov := -float64(level)
		linear = math.Pow(10, dbov/20.0)
	} else {
		energy := float64(len(packet.Payload)) / 200.0
		if energy > 1.0 {
			energy = 1.0
		}
		linear = energy * 0.1
	}

	a.sumSquares += linear * linear
	abs := math.Abs(linear)
	if abs > a.peakAbs {
		a.peakAbs = abs
	}
	if abs >= clipThresh {
		a.clippingSamples++
	}
	a.samples++

	if now.Sub(a.lastEmissionAt) < time.Second {
		return nil
	}

	var snap snapshot
	if a.samples > 0 {
		rms := math.Sqrt(a.sumSquares / float64(a.samples))
		snap.rmsDBFS = toDBFS(rms)
		snap.peakDBFS = toDBFS(a.peakAbs)
	} else {
		snap.rmsDBFS = dbfsFloor
		snap.peakDBFS = dbfsFloor
	}

	if snap.rmsDBFS <= silenceDBFS {
		a.silentSnapshots++
	}

	a.snapshots[a.snapIdx] = snap
	a.snapIdx = (a.snapIdx + 1) % a.windowSize
	if a.snapCount < a.windowSize {
		a.snapCount++
	}

	a.sumSquares = 0
	a.peakAbs = 0
	a.samples = 0
	a.lastEmissionAt = now

	return a.buildPayload(snap)
}

func (a *Analyzer) buildPayload(current snapshot) []byte {
	p := payload{
		Type:            "audio",
		Analyses:        []analysisItem{},
		Recommendations: map[string]string{},
	}

	p.Analyses = append(p.Analyses, analysisItem{
		ID:      "rms_level",
		Label:   "RMS Level",
		Message: formatDBFS(current.rmsDBFS),
		Color:   colorForRMS(current.rmsDBFS),
	})

	p.Analyses = append(p.Analyses, analysisItem{
		ID:      "peak_level",
		Label:   "Peak Level",
		Message: formatDBFS(current.peakDBFS),
		Color:   colorForPeak(current.peakDBFS),
	})

	if a.snapCount > 0 {
		minRMS := math.MaxFloat64
		maxRMS := -math.MaxFloat64
		sumRMS := 0.0
		maxPeak := -math.MaxFloat64

		for i := 0; i < a.snapCount; i++ {
			s := a.snapshots[i]
			if s.rmsDBFS < minRMS {
				minRMS = s.rmsDBFS
			}
			if s.rmsDBFS > maxRMS {
				maxRMS = s.rmsDBFS
			}
			sumRMS += s.rmsDBFS
			if s.peakDBFS > maxPeak {
				maxPeak = s.peakDBFS
			}
		}
		avgRMS := sumRMS / float64(a.snapCount)

		windowLabel := fmt.Sprintf("%ds", a.windowSize)

		p.Analyses = append(p.Analyses,
			analysisItem{
				ID:      "window_avg",
				Label:   "Avg RMS (" + windowLabel + " window)",
				Message: formatDBFS(avgRMS),
				Color:   colorForRMS(avgRMS),
			},
			analysisItem{
				ID:      "window_min",
				Label:   "Min RMS (" + windowLabel + " window)",
				Message: formatDBFS(minRMS),
			},
			analysisItem{
				ID:      "window_max",
				Label:   "Max RMS (" + windowLabel + " window)",
				Message: formatDBFS(maxRMS),
			},
			analysisItem{
				ID:      "window_peak",
				Label:   "Peak (" + windowLabel + " window)",
				Message: formatDBFS(maxPeak),
				Color:   colorForPeak(maxPeak),
			},
		)
	}

	clippingMsg := "None detected"
	if a.clippingSamples > 0 {
		clippingMsg = fmt.Sprintf("%d samples clipped", a.clippingSamples)
	}
	p.Analyses = append(p.Analyses, analysisItem{
		ID:      "clipping",
		Label:   "Clipping",
		Message: clippingMsg,
		Color:   colorForClipping(a.clippingSamples),
	})

	elapsed := time.Since(a.trackStartedAt).Seconds()
	if elapsed >= 2 {
		totalSnaps := int(elapsed)
		if totalSnaps > 0 {
			silencePct := float64(a.silentSnapshots) / float64(totalSnaps) * 100
			silenceMsg := fmt.Sprintf("%.0f%%", silencePct)
			silenceColor := "rgba(34, 197, 94, 0.22)"
			if silencePct > 50 {
				silenceColor = "rgba(239, 68, 68, 0.22)"
			} else if silencePct > 20 {
				silenceColor = "rgba(250, 204, 21, 0.22)"
			}
			p.Analyses = append(p.Analyses, analysisItem{
				ID:      "silence",
				Label:   "Silence",
				Message: silenceMsg,
				Color:   silenceColor,
			})
		}
	}

	if current.rmsDBFS > -3 {
		p.Recommendations["rec_hot"] = "Audio levels are very hot (above -3 dBFS). Reduce input gain to avoid distortion."
	} else if current.rmsDBFS > -6 {
		p.Recommendations["rec_warm"] = "Audio levels are approaching hot. Consider reducing input gain slightly."
	}

	if a.clippingSamples > 0 {
		p.Recommendations["rec_clipping"] = "Clipping detected. Reduce your input gain or add a limiter."
	}

	if elapsed >= 5 && current.rmsDBFS <= silenceDBFS {
		p.Recommendations["rec_silence"] = "Audio appears silent. Check that your microphone is unmuted and input gain is turned up."
	}

	if elapsed >= 5 && current.rmsDBFS > silenceDBFS && current.rmsDBFS <= -36 {
		p.Recommendations["rec_low"] = "Audio level is very low. Increase your input gain for better signal quality."
	}

	message, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return message
}
