package h264

import (
	"errors"
	"fmt"
	"io"
	"strconv"
)

type bitReader struct {
	data      []byte
	bitOffset int
}

func (b *bitReader) readBit() (uint, error) {
	if b.bitOffset >= len(b.data)*8 {
		return 0, io.ErrUnexpectedEOF
	}

	byteIndex := b.bitOffset / 8
	bitIndex := 7 - (b.bitOffset % 8)
	value := (b.data[byteIndex] >> bitIndex) & 0x01
	b.bitOffset++

	return uint(value), nil
}

func (b *bitReader) readBits(bitCount int) (uint, error) {
	var result uint

	for i := 0; i < bitCount; i++ {
		bit, err := b.readBit()
		if err != nil {
			return 0, err
		}

		result = (result << 1) | bit
	}

	return result, nil
}

func (b *bitReader) readUE() (uint, error) {
	leadingZeroBits := 0
	for {
		bit, err := b.readBit()
		if err != nil {
			return 0, err
		}

		if bit == 1 {
			break
		}

		leadingZeroBits++
		if leadingZeroBits > 31 {
			return 0, errors.New("exp-golomb value too large")
		}
	}

	if leadingZeroBits == 0 {
		return 0, nil
	}

	suffix, err := b.readBits(leadingZeroBits)
	if err != nil {
		return 0, err
	}

	return ((1 << leadingZeroBits) - 1) + suffix, nil
}

func (b *bitReader) readSE() (int, error) {
	unsignedValue, err := b.readUE()
	if err != nil {
		return 0, err
	}

	value := int((unsignedValue + 1) / 2)
	if unsignedValue%2 == 0 {
		return -value, nil
	}

	return value, nil
}

func rbspFromNALU(nalu []byte) []byte {
	if len(nalu) <= 1 {
		return nil
	}

	payload := nalu[1:]
	rbsp := make([]byte, 0, len(payload))
	zeroCount := 0

	for _, b := range payload {
		if zeroCount >= 2 && b == 0x03 {
			zeroCount = 0
			continue
		}

		rbsp = append(rbsp, b)

		if b == 0x00 {
			zeroCount++
		} else {
			zeroCount = 0
		}
	}

	return rbsp
}

func isHighProfile(profileIDC uint) bool {
	switch profileIDC {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 244:
		return true
	default:
		return false
	}
}

func skipScalingList(reader *bitReader, size int) error {
	lastScale := 8
	nextScale := 8

	for i := 0; i < size; i++ {
		if nextScale != 0 {
			deltaScale, err := reader.readSE()
			if err != nil {
				return err
			}

			nextScale = (lastScale + deltaScale + 256) % 256
		}

		if nextScale != 0 {
			lastScale = nextScale
		}
	}

	return nil
}

type SPSInfo struct {
	ProfileIDC int
	LevelIDC   int
	SPSID      int
	Width      int
	Height     int
}

func (s SPSInfo) ProfileName() string {
	switch s.ProfileIDC {
	case 66:
		return "Baseline"
	case 77:
		return "Main"
	case 88:
		return "Extended"
	case 100:
		return "High"
	case 110:
		return "High 10"
	case 122:
		return "High 4:2:2"
	case 144:
		return "High 4:4:4"
	default:
		return "Unknown (" + strconv.Itoa(s.ProfileIDC) + ")"
	}
}

func (s SPSInfo) LevelName() string {
	switch s.LevelIDC {
	case 9:
		return "1b"
	case 10:
		return "1.0"
	case 11:
		return "1.1"
	case 12:
		return "1.2"
	case 13:
		return "1.3"
	case 20:
		return "2.0"
	case 21:
		return "2.1"
	case 22:
		return "2.2"
	case 30:
		return "3.0"
	case 31:
		return "3.1"
	case 32:
		return "3.2"
	case 40:
		return "4.0"
	case 41:
		return "4.1"
	case 42:
		return "4.2"
	case 50:
		return "5.0"
	case 51:
		return "5.1"
	case 52:
		return "5.2"
	case 60:
		return "6.0"
	case 61:
		return "6.1"
	case 62:
		return "6.2"
	default:
		return "Unknown (" + strconv.Itoa(s.LevelIDC) + ")"
	}
}

func ParseSPSInfo(nalu []byte) (SPSInfo, error) {
	if len(nalu) == 0 {
		return SPSInfo{}, errors.New("empty sps nalu")
	}

	reader := &bitReader{data: rbspFromNALU(nalu)}

	profileIDC, err := reader.readBits(8)
	if err != nil {
		return SPSInfo{}, err
	}

	if _, err = reader.readBits(8); err != nil {
		return SPSInfo{}, err
	}

	levelIDC, err := reader.readBits(8)
	if err != nil {
		return SPSInfo{}, err
	}

	spsID, err := reader.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	chromaFormatIDC := uint(1)
	separateColourPlaneFlag := uint(0)

	if isHighProfile(profileIDC) {
		if chromaFormatIDC, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if chromaFormatIDC == 3 {
			if separateColourPlaneFlag, err = reader.readBit(); err != nil {
				return SPSInfo{}, err
			}
		}

		if _, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if _, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if _, err = reader.readBit(); err != nil {
			return SPSInfo{}, err
		}

		seqScalingMatrixPresentFlag, scalingMatrixErr := reader.readBit()
		if scalingMatrixErr != nil {
			return SPSInfo{}, scalingMatrixErr
		}

		if seqScalingMatrixPresentFlag == 1 {
			scalingListCount := 8
			if chromaFormatIDC == 3 {
				scalingListCount = 12
			}

			for i := 0; i < scalingListCount; i++ {
				seqScalingListPresentFlag, presentErr := reader.readBit()
				if presentErr != nil {
					return SPSInfo{}, presentErr
				}

				if seqScalingListPresentFlag == 0 {
					continue
				}

				scalingListSize := 16
				if i >= 6 {
					scalingListSize = 64
				}

				if err = skipScalingList(reader, scalingListSize); err != nil {
					return SPSInfo{}, err
				}
			}
		}
	}

	if _, err = reader.readUE(); err != nil {
		return SPSInfo{}, err
	}

	picOrderCntType, err := reader.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	switch picOrderCntType {
	case 0:
		if _, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}
	case 1:
		if _, err = reader.readBit(); err != nil {
			return SPSInfo{}, err
		}

		if _, err = reader.readSE(); err != nil {
			return SPSInfo{}, err
		}

		if _, err = reader.readSE(); err != nil {
			return SPSInfo{}, err
		}

		refFramesInPicOrderCntCycle, cycleErr := reader.readUE()
		if cycleErr != nil {
			return SPSInfo{}, cycleErr
		}

		for i := uint(0); i < refFramesInPicOrderCntCycle; i++ {
			if _, err = reader.readSE(); err != nil {
				return SPSInfo{}, err
			}
		}
	}

	if _, err = reader.readUE(); err != nil {
		return SPSInfo{}, err
	}

	if _, err = reader.readBit(); err != nil {
		return SPSInfo{}, err
	}

	picWidthInMbsMinus1, err := reader.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	picHeightInMapUnitsMinus1, err := reader.readUE()
	if err != nil {
		return SPSInfo{}, err
	}

	frameMbsOnlyFlag, err := reader.readBit()
	if err != nil {
		return SPSInfo{}, err
	}

	if frameMbsOnlyFlag == 0 {
		if _, err = reader.readBit(); err != nil {
			return SPSInfo{}, err
		}
	}

	if _, err = reader.readBit(); err != nil {
		return SPSInfo{}, err
	}

	frameCroppingFlag, err := reader.readBit()
	if err != nil {
		return SPSInfo{}, err
	}

	var frameCropLeftOffset uint
	var frameCropRightOffset uint
	var frameCropTopOffset uint
	var frameCropBottomOffset uint

	if frameCroppingFlag == 1 {
		if frameCropLeftOffset, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if frameCropRightOffset, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if frameCropTopOffset, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}

		if frameCropBottomOffset, err = reader.readUE(); err != nil {
			return SPSInfo{}, err
		}
	}

	frameMbsOnlyFlagInt := int(frameMbsOnlyFlag)
	width := int(picWidthInMbsMinus1+1) * 16
	height := int(picHeightInMapUnitsMinus1+1) * 16 * (2 - frameMbsOnlyFlagInt)

	chromaArrayType := int(chromaFormatIDC)
	if separateColourPlaneFlag == 1 {
		chromaArrayType = 0
	}

	cropUnitX := 1
	cropUnitY := 2 - frameMbsOnlyFlagInt

	switch chromaArrayType {
	case 1:
		cropUnitX = 2
		cropUnitY = 2 * (2 - frameMbsOnlyFlagInt)
	case 2:
		cropUnitX = 2
		cropUnitY = 2 - frameMbsOnlyFlagInt
	case 3:
		cropUnitX = 1
		cropUnitY = 2 - frameMbsOnlyFlagInt
	}

	width -= int(frameCropLeftOffset+frameCropRightOffset) * cropUnitX
	height -= int(frameCropTopOffset+frameCropBottomOffset) * cropUnitY

	if width <= 0 || height <= 0 {
		return SPSInfo{}, fmt.Errorf("invalid cropped resolution %dx%d", width, height)
	}

	return SPSInfo{
		ProfileIDC: int(profileIDC),
		LevelIDC:   int(levelIDC),
		SPSID:      int(spsID),
		Width:      width,
		Height:     height,
	}, nil
}

type PPSInfo struct {
	PPSID         int
	SPSID         int
	EntropyCoding string
}

func ParsePPSInfo(nalu []byte) (PPSInfo, error) {
	if len(nalu) == 0 {
		return PPSInfo{}, errors.New("empty pps nalu")
	}

	reader := &bitReader{data: rbspFromNALU(nalu)}

	ppsID, err := reader.readUE()
	if err != nil {
		return PPSInfo{}, err
	}

	spsID, err := reader.readUE()
	if err != nil {
		return PPSInfo{}, err
	}

	entropyCodingModeFlag, err := reader.readBit()
	if err != nil {
		return PPSInfo{}, err
	}

	entropyCoding := "CAVLC"
	if entropyCodingModeFlag == 1 {
		entropyCoding = "CABAC"
	}

	return PPSInfo{
		PPSID:         int(ppsID),
		SPSID:         int(spsID),
		EntropyCoding: entropyCoding,
	}, nil
}

func findAnnexBStartCode(payload []byte, offset int) (int, int) {
	for i := offset; i+3 <= len(payload); i++ {
		if payload[i] != 0x00 || payload[i+1] != 0x00 {
			continue
		}

		if payload[i+2] == 0x01 {
			return i, 3
		}

		if i+3 < len(payload) && payload[i+2] == 0x00 && payload[i+3] == 0x01 {
			return i, 4
		}
	}

	return -1, 0
}

func SplitAnnexBNALUs(payload []byte) [][]byte {
	nalus := make([][]byte, 0)
	searchOffset := 0

	for {
		start, startCodeSize := findAnnexBStartCode(payload, searchOffset)
		if start == -1 {
			break
		}

		naluStart := start + startCodeSize
		nextStart, _ := findAnnexBStartCode(payload, naluStart)

		naluEnd := len(payload)
		if nextStart != -1 {
			naluEnd = nextStart
		}

		if naluStart < naluEnd {
			nalus = append(nalus, payload[naluStart:naluEnd])
		}

		if nextStart == -1 {
			break
		}

		searchOffset = nextStart
	}

	return nalus
}
