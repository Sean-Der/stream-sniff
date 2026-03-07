package webrtc

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/pion/webrtc/v4"
)

var (
	anaylzeDataChannelsLock sync.Mutex
	anaylzeDataChannels     = map[string][]*webrtc.DataChannel{}
)

type analysisItem struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Message string `json:"message"`
	Color   string `json:"color,omitempty"`
}

func writeAnalyzeMessage(bearerToken string, analyses []analysisItem) {
	anaylzeDataChannelsLock.Lock()
	defer anaylzeDataChannelsLock.Unlock()

	message, err := json.Marshal(analyses)
	if err != nil {
		log.Printf("bearerToken=%s encode-analysis err=%v", bearerToken, err)
		return
	}

	for _, dataChannel := range anaylzeDataChannels[bearerToken] {
		if err := dataChannel.SendText(string(message)); err != nil {
			log.Printf("bearerToken=%s data-channel label=%s send-analysis err=%v", bearerToken, dataChannel.Label(), err)
		}
	}
}

func Analyze(offer, bearerToken string) (string, error) {
	return negotiateOffer(offer, bearerToken, func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string) {
		peerConnection.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
			anaylzeDataChannelsLock.Lock()
			anaylzeDataChannels[bearerToken] = append(anaylzeDataChannels[bearerToken], dataChannel)
			anaylzeDataChannelsLock.Unlock()

			dataChannel.OnClose(func() {
				anaylzeDataChannelsLock.Lock()
				defer anaylzeDataChannelsLock.Unlock()

				dataChannels := anaylzeDataChannels[bearerToken]
				for i, currentDataChannel := range dataChannels {
					if currentDataChannel != dataChannel {
						continue
					}

					dataChannels = append(dataChannels[:i], dataChannels[i+1:]...)
					if len(dataChannels) == 0 {
						delete(anaylzeDataChannels, bearerToken)
					} else {
						anaylzeDataChannels[bearerToken] = dataChannels
					}

					return
				}
			})

			log.Printf("bearerToken=%s session=%s data-channel label=%s", bearerToken, sessionID, dataChannel.Label())
		})
	})
}
