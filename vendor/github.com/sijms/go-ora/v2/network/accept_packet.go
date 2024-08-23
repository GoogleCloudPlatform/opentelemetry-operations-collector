package network

import (
	"encoding/binary"
	"fmt"
	"github.com/sijms/go-ora/v2/configurations"
)

// type AcceptPacket Packet
type AcceptPacket struct {
	Packet
	buffer []byte
}

func (pck *AcceptPacket) bytes() []byte {
	output := pck.Packet.bytes()
	binary.BigEndian.PutUint16(output[8:], pck.sessionCtx.Version)
	binary.BigEndian.PutUint16(output[10:], pck.sessionCtx.Options)
	if pck.sessionCtx.Version < 315 {
		binary.BigEndian.PutUint16(output[12:], uint16(pck.sessionCtx.SessionDataUnit))
		binary.BigEndian.PutUint16(output[14:], uint16(pck.sessionCtx.TransportDataUnit))
	} else {
		binary.BigEndian.PutUint32(output[32:], pck.sessionCtx.SessionDataUnit)
		binary.BigEndian.PutUint32(output[36:], pck.sessionCtx.TransportDataUnit)
	}

	binary.BigEndian.PutUint16(output[16:], pck.sessionCtx.Histone)
	binary.BigEndian.PutUint16(output[18:], uint16(len(pck.buffer)))
	binary.BigEndian.PutUint16(output[20:], pck.dataOffset)
	output[22] = pck.sessionCtx.ACFL0
	output[23] = pck.sessionCtx.ACFL1
	// s
	output = append(output, pck.buffer...)
	return output
}

func newAcceptPacketFromData(packetData []byte, config *configurations.ConnectionConfig) *AcceptPacket {
	if len(packetData) < 32 {
		return nil
	}
	reconAddStart := binary.BigEndian.Uint16(packetData[28:])
	reconAddLen := binary.BigEndian.Uint16(packetData[30:])
	reconAdd := ""
	if reconAddStart != 0 && reconAddLen != 0 && uint16(len(packetData)) > (reconAddStart+reconAddLen) {
		reconAdd = string(packetData[reconAddStart:(reconAddStart + reconAddLen)])
	}
	pck := AcceptPacket{
		Packet: Packet{
			sessionCtx: &SessionContext{
				connConfig:          config,
				SID:                 nil,
				Version:             binary.BigEndian.Uint16(packetData[8:]),
				LoVersion:           0,
				Options:             0,
				NegotiatedOptions:   binary.BigEndian.Uint16(packetData[10:]),
				OurOne:              0,
				Histone:             binary.BigEndian.Uint16(packetData[16:]),
				ReconAddr:           reconAdd,
				ACFL0:               packetData[22],
				ACFL1:               packetData[23],
				SessionDataUnit:     uint32(binary.BigEndian.Uint16(packetData[12:])),
				TransportDataUnit:   uint32(binary.BigEndian.Uint16(packetData[14:])),
				UsingAsyncReceivers: false,
				IsNTConnected:       false,
				OnBreakReset:        false,
				GotReset:            false,
			},
			dataOffset: binary.BigEndian.Uint16(packetData[20:]),
			length:     uint32(binary.BigEndian.Uint16(packetData)),
			packetType: PacketType(packetData[4]),
			flag:       packetData[5],
		},
	}
	pck.buffer = packetData[int(pck.dataOffset):]
	if pck.sessionCtx.Version >= 315 {
		pck.sessionCtx.SessionDataUnit = binary.BigEndian.Uint32(packetData[32:])
		pck.sessionCtx.TransportDataUnit = binary.BigEndian.Uint32(packetData[36:])
	}
	if (pck.flag & 1) > 0 {
		fmt.Println("contain SID data")
		pck.length -= 16
		pck.sessionCtx.SID = packetData[int(pck.length):]
	}
	if pck.sessionCtx.TransportDataUnit < pck.sessionCtx.SessionDataUnit {
		pck.sessionCtx.SessionDataUnit = pck.sessionCtx.TransportDataUnit
	}
	if binary.BigEndian.Uint16(packetData[18:]) != uint16(len(pck.buffer)) {
		return nil
	}
	return &pck
}

//func (pck *AcceptPacket) SessionCTX() SessionContext {
//	return pck.sessionCtx
//}
