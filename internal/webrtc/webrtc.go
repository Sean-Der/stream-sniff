package webrtc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/pion/dtls/v3/pkg/crypto/elliptic"
	"github.com/pion/ice/v3"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

var (
	apiWhip *webrtc.API

	videoRTCPFeedback = []webrtc.RTCPFeedback{
		{Type: "goog-remb", Parameter: ""},
		{Type: "ccm", Parameter: "fir"},
		{Type: "nack", Parameter: ""},
		{Type: "nack", Parameter: "pli"},
	}
)

func getPublicIP() string {
	req, err := http.Get("http://ip-api.com/json/")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if closeErr := req.Body.Close(); closeErr != nil {
			log.Fatal(closeErr)
		}
	}()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Fatal(err)
	}

	ip := struct {
		Query string
	}{}
	if err = json.Unmarshal(body, &ip); err != nil {
		log.Fatal(err)
	}

	if ip.Query == "" {
		log.Fatal("Query entry was not populated")
	}

	return ip.Query
}

func createSettingEngine(udpMuxCache map[int]*ice.MultiUDPMuxDefault, tcpMuxCache map[string]ice.TCPMux) (settingEngine webrtc.SettingEngine) {
	var (
		nat1To1IPs   []string
		networkTypes []webrtc.NetworkType
		udpMuxPort   int
		udpMuxOpts   []ice.UDPMuxFromPortOption
		err          error
	)

	if os.Getenv("NETWORK_TYPES") != "" {
		for _, networkTypeStr := range strings.Split(os.Getenv("NETWORK_TYPES"), "|") {
			networkType, convErr := webrtc.NewNetworkType(networkTypeStr)
			if convErr != nil {
				log.Fatal(convErr)
			}
			networkTypes = append(networkTypes, networkType)
		}
	} else {
		networkTypes = append(networkTypes, webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6)
	}

	if os.Getenv("INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP") != "" {
		nat1To1IPs = append(nat1To1IPs, getPublicIP())
	}

	if os.Getenv("NAT_1_TO_1_IP") != "" {
		nat1To1IPs = append(nat1To1IPs, strings.Split(os.Getenv("NAT_1_TO_1_IP"), "|")...)
	}

	natICECandidateType := webrtc.ICECandidateTypeHost
	if os.Getenv("NAT_ICE_CANDIDATE_TYPE") == "srflx" {
		natICECandidateType = webrtc.ICECandidateTypeSrflx
	}

	if len(nat1To1IPs) != 0 {
		settingEngine.SetNAT1To1IPs(nat1To1IPs, natICECandidateType)
	}

	if os.Getenv("INTERFACE_FILTER") != "" {
		interfaceFilter := func(i string) bool {
			return i == os.Getenv("INTERFACE_FILTER")
		}

		settingEngine.SetInterfaceFilter(interfaceFilter)
		udpMuxOpts = append(udpMuxOpts, ice.UDPMuxFromPortWithInterfaceFilter(interfaceFilter))
	}

	if os.Getenv("UDP_MUX_PORT_WHIP") != "" {
		if udpMuxPort, err = strconv.Atoi(os.Getenv("UDP_MUX_PORT_WHIP")); err != nil {
			log.Fatal(err)
		}
	} else if os.Getenv("UDP_MUX_PORT") != "" {
		if udpMuxPort, err = strconv.Atoi(os.Getenv("UDP_MUX_PORT")); err != nil {
			log.Fatal(err)
		}
	}

	if udpMuxPort != 0 {
		udpMux, ok := udpMuxCache[udpMuxPort]
		if !ok {
			if udpMux, err = ice.NewMultiUDPMuxFromPort(udpMuxPort, udpMuxOpts...); err != nil {
				log.Fatal(err)
			}
			udpMuxCache[udpMuxPort] = udpMux
		}

		settingEngine.SetICEUDPMux(udpMux)
	}

	if os.Getenv("TCP_MUX_ADDRESS") != "" {
		tcpMux, ok := tcpMuxCache[os.Getenv("TCP_MUX_ADDRESS")]
		if !ok {
			tcpAddr, err := net.ResolveTCPAddr("tcp", os.Getenv("TCP_MUX_ADDRESS"))
			if err != nil {
				log.Fatal(err)
			}

			tcpListener, err := net.ListenTCP("tcp", tcpAddr)
			if err != nil {
				log.Fatal(err)
			}

			tcpMux = webrtc.NewICETCPMux(nil, tcpListener, 8)
			tcpMuxCache[os.Getenv("TCP_MUX_ADDRESS")] = tcpMux
		}
		settingEngine.SetICETCPMux(tcpMux)

		if os.Getenv("TCP_MUX_FORCE") != "" {
			networkTypes = []webrtc.NetworkType{webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6}
		} else {
			networkTypes = append(networkTypes, webrtc.NetworkTypeTCP4, webrtc.NetworkTypeTCP6)
		}
	}

	settingEngine.SetDTLSEllipticCurves(elliptic.X25519, elliptic.P384, elliptic.P256)
	settingEngine.SetNetworkTypes(networkTypes)
	settingEngine.DisableSRTCPReplayProtection(true)
	settingEngine.DisableSRTPReplayProtection(true)
	settingEngine.SetIncludeLoopbackCandidate(os.Getenv("INCLUDE_LOOPBACK_CANDIDATE") != "")

	return settingEngine
}

func PopulateMediaEngine(m *webrtc.MediaEngine) error {
	for _, codec := range []webrtc.RTPCodecParameters{
		{
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}, //nolint:exhaustruct
			PayloadType:        111,
		},
	} {
		if err := m.RegisterCodec(codec, webrtc.RTPCodecTypeAudio); err != nil {
			return err
		}
	}

	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{URI: "urn:ietf:params:rtp-hdrext:ssrc-audio-level"}, webrtc.RTPCodecTypeAudio); err != nil {
		return err
	}

	for _, codecDetails := range []struct {
		payloadType uint8
		mimeType    string
		sdpFmtpLine string
	}{
		{102, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f"},
		{104, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42001f"},
		{106, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"},
		{108, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f"},
		{39, webrtc.MimeTypeH264, "level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=4d001f"},
	} {
		if err := m.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:     codecDetails.mimeType,
				ClockRate:    90000,
				SDPFmtpLine:  codecDetails.sdpFmtpLine,
				RTCPFeedback: videoRTCPFeedback,
			},
			PayloadType: webrtc.PayloadType(codecDetails.payloadType),
		}, webrtc.RTPCodecTypeVideo); err != nil {
			return err
		}

		if err := m.RegisterCodec(webrtc.RTPCodecParameters{
			RTPCodecCapability: webrtc.RTPCodecCapability{
				MimeType:    "video/rtx",
				ClockRate:   90000,
				SDPFmtpLine: fmt.Sprintf("apt=%d", codecDetails.payloadType),
			},
			PayloadType: webrtc.PayloadType(codecDetails.payloadType + 1),
		}, webrtc.RTPCodecTypeVideo); err != nil {
			return err
		}
	}

	return nil
}

func newPeerConnection(api *webrtc.API) (*webrtc.PeerConnection, error) {
	cfg := webrtc.Configuration{}

	if stunServers := os.Getenv("STUN_SERVERS"); stunServers != "" {
		for _, stunServer := range strings.Split(stunServers, "|") {
			cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{
				URLs: []string{"stun:" + stunServer},
			})
		}
	}

	return api.NewPeerConnection(cfg)
}

func negotiateOffer(
	offer, bearerToken string,
	configurePeerConnection func(peerConnection *webrtc.PeerConnection, bearerToken, sessionID string),
) (string, error) {
	maybePrintOfferAnswer(offer, true)

	sessionID := uuid.NewString()
	peerConnection, err := newPeerConnection(apiWhip)
	if err != nil {
		return "", err
	}

	cleanup := func() {
		if closeErr := peerConnection.Close(); closeErr != nil {
			log.Printf("bearerToken=%s session=%s close err=%v", bearerToken, sessionID, closeErr)
		}
	}

	if configurePeerConnection != nil {
		configurePeerConnection(peerConnection, bearerToken, sessionID)
	}

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("bearerToken=%s session=%s peer-connection=%s", bearerToken, sessionID, state.String())

		if state == webrtc.PeerConnectionStateFailed {
			if closeErr := peerConnection.Close(); closeErr != nil {
				log.Printf("bearerToken=%s session=%s close err=%v", bearerToken, sessionID, closeErr)
			}
		}
	})

	if err = peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		SDP:  offer,
		Type: webrtc.SDPTypeOffer,
	}); err != nil {
		cleanup()
		return "", err
	}

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

func appendAnswer(in string) string {
	if extraCandidate := os.Getenv("APPEND_CANDIDATE"); extraCandidate != "" {
		index := strings.Index(in, "a=end-of-candidates")
		in = in[:index] + extraCandidate + in[index:]
	}

	return in
}

func maybePrintOfferAnswer(sdp string, isOffer bool) string {
	if os.Getenv("DEBUG_PRINT_OFFER") != "" && isOffer {
		fmt.Println(sdp) //nolint:forbidigo
	}

	if os.Getenv("DEBUG_PRINT_ANSWER") != "" && !isOffer {
		fmt.Println(sdp) //nolint:forbidigo
	}

	return sdp
}

func Configure() {
	mediaEngine := &webrtc.MediaEngine{}
	if err := PopulateMediaEngine(mediaEngine); err != nil {
		panic(err)
	}

	interceptorRegistry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		log.Fatal(err)
	}

	udpMuxCache := map[int]*ice.MultiUDPMuxDefault{}
	tcpMuxCache := map[string]ice.TCPMux{}

	apiWhip = webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
		webrtc.WithSettingEngine(createSettingEngine(udpMuxCache, tcpMuxCache)),
	)
}
