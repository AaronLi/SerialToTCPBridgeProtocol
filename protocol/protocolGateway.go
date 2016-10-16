package protocol

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"strconv"
	"time"
)

// Implementation of the Protocol Gateway.
type gateway struct {
	protocolTransport          // Connection between Protocol Gateway & Client.
	uStream           net.Conn // Upstream connection to tcp Server.
}

// Initialize downstream RX and listen for a protocol Client.
func (g *gateway) Listen(ds serialInterface) {
	g.com = ds
	g.rxBuff = make(chan byte, 512)
	g.acknowledgeEvent = make(chan bool)
	g.state = Disconnected

	g.session.Add(2)
	go g.rxSerial(g.dropGateway)
	go g.packetReader()
	g.session.Wait()
}

// Parse RX buffer for legitimate packets.
func (g *gateway) packetReader() {
	defer g.session.Done()
	timeouts := 0
PACKET_RX_LOOP:
	for {
		if timeouts >= 5 {
			if g.state == Connected {
				log.Println("Downstream RX timeout. Disconnecting client")
				g.txBuff <- Packet{command: disconnect}
				g.dropLink()
				return
			}
			timeouts = 0
		}

		p := Packet{}
		var ok bool

		// Length byte
		p.length, ok = <-g.rxBuff
		if !ok {
			return
		}

		// Command byte
		select {
		case p.command, ok = <-g.rxBuff:
			if !ok {
				return
			}
		case <-time.After(time.Millisecond * 100):
			timeouts++
			continue PACKET_RX_LOOP // discard
		}

		// Payload
		for i := 0; i < int(p.length)-5; i++ {
			select {
			case payloadByte, ok := <-g.rxBuff:
				if !ok {
					return
				}
				p.payload = append(p.payload, payloadByte)
			case <-time.After(time.Millisecond * 100):
				timeouts++
				continue PACKET_RX_LOOP
			}
		}

		// CRC32
		rxCrc := make([]byte, 0, 4)
		for i := 0; i < 4; i++ {
			select {
			case crcByte, ok := <-g.rxBuff:
				if !ok {
					return
				}
				rxCrc = append(rxCrc, crcByte)
			case <-time.After(time.Millisecond * 100):
				timeouts++
				continue PACKET_RX_LOOP
			}
		}
		p.crc = binary.LittleEndian.Uint32(rxCrc)

		// Integrity Checking
		if p.calcCrc() != p.crc {
			log.Println("Downstream packet RX CRCFAIL")
			timeouts++
			continue PACKET_RX_LOOP
		}
		timeouts = 0
		g.handleRxPacket(&p)
	}
}

// Packet RX done. Handle it.
func (g *gateway) handleRxPacket(packet *Packet) {
	var rxSeqFlag bool = (packet.command & 0x80) > 0
	switch packet.command & 0x7F {
	case publish:
		// Payload from serial client
		if g.state == Connected {
			g.txBuff <- Packet{command: acknowledge | (packet.command & 0x80)}
			if rxSeqFlag == g.expectedRxSeqFlag {
				g.expectedRxSeqFlag = !g.expectedRxSeqFlag
				_, err := g.uStream.Write(packet.payload)
				if err != nil {
					log.Printf("Error sending upstream: %v Disconnecting client\n", err)
					g.txBuff <- Packet{command: disconnect}
					g.dropLink()
				}
			}
		}
	case acknowledge:
		if g.state == Connected {
			g.acknowledgeEvent <- rxSeqFlag
		}
	case connect:
		if g.state != Disconnected {
			return
		}
		if len(packet.payload) != 6 {
			return
		}

		port := binary.LittleEndian.Uint16(packet.payload[4:])

		var dst bytes.Buffer
		for i := 0; i < 3; i++ {
			dst.WriteString(strconv.Itoa(int(packet.payload[i])))
			dst.WriteByte('.')
		}
		dst.WriteString(strconv.Itoa(int(packet.payload[3])))
		dst.WriteByte(':')
		dst.WriteString(strconv.Itoa(int(port)))
		dstStr := dst.String()

		g.txBuff = make(chan Packet, 2)
		g.expectedRxSeqFlag = false

		// Start downstream TX
		g.session.Add(1)
		go g.txSerial(g.dropGateway)

		// Open connection to upstream server on behalf of client
		// log.Printf("Gateway: Connect request from client. Dialing to: %v\n", dstStr)
		var err error
		if g.uStream, err = net.Dial("tcp", dstStr); err != nil { // TODO: add timeout
			log.Printf("Gateway: Failed to connect to: %v\n", dstStr)
			g.txBuff <- Packet{command: disconnect} // TODO: payload to contain error or timeout
			g.dropLink()
			return
		}

		// Start link session
		g.session.Add(1)
		go g.packetSender()
		// log.Printf("Gateway: Connected to %v\n", dstStr)
		g.state = Connected
		g.txBuff <- Packet{command: connack}
	case disconnect:
		if g.state == Connected {
			log.Println("Client wants to disconnect. Ending link session")
			g.dropLink()
		}
	}
}

// Publish data downstream received from upstream tcp server.
// We need to get an Ack before sending the next publish packet.
// Resend same publish packet after timeout, and kill link after 5 retries.
func (g *gateway) packetSender() {
	defer g.session.Done()
	sequenceTxFlag := false
	retries := 0
	tx := make([]byte, 512)
	for {
		nRx, err := g.uStream.Read(tx)
		if err != nil {
			if g.state == Connected {
				log.Printf("Error receiving upstream: %v. Disconnecting client\n", err)
				g.txBuff <- Packet{command: disconnect}
				g.dropLink()
			}
			return
		}
		p := Packet{command: publish, payload: tx[:nRx]}
		if sequenceTxFlag {
			p.command |= 0x80
		}
	PUB_LOOP:
		for {
			g.txBuff <- p
			select {
			case ack, ok := <-g.acknowledgeEvent:
				if ok && ack == sequenceTxFlag {
					retries = 0
					sequenceTxFlag = !sequenceTxFlag
					break PUB_LOOP // success
				}
			case <-time.After(time.Millisecond * 500):
				retries++
				if retries >= 5 {
					log.Println("Too many downstream send retries. Disconnecting client")
					g.txBuff <- Packet{command: disconnect}
					g.dropLink()
					return
				}
			}
		}
	}
}

// End link session between upstream server and downstream client.
func (g *gateway) dropLink() {
	if g.uStream != nil {
		g.uStream.Close()
	}
	if g.txBuff != nil {
		close(g.txBuff)
		g.txBuff = nil
	}
	g.state = Disconnected
}

// Stop activity and release downstream interface.
func (g *gateway) dropGateway() {
	g.dropLink()
	g.com.Close()
	close(g.rxBuff)
	g.state = TransportNotReady
}