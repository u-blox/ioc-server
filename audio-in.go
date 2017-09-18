/* Audio input (UDP or TCP server) for the Internet of Chuffs.
 *
 * Copyright (C) u-blox Melbourn Ltd
 * u-blox Melbourn Ltd, Melbourn, UK
 *
 * All rights reserved.
 *
 * This source file is the sole property of u-blox Melbourn Ltd.
 * Reproduction or utilization of this source in whole or part is
 * forbidden without the written consent of u-blox Melbourn Ltd.
 */

package main

import (
    "fmt"
    "net"
    "os"
    "log"
    "bytes"
//    "encoding/hex"
)

//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

// Struct to hold a URTP datagram
type UrtpDatagram struct {
    SequenceNumber  uint16
    Timestamp       uint64
    Audio           *[]int16
}

//--------------------------------------------------------------------
// Constants
//--------------------------------------------------------------------

// The duration of a block of incoming audio in ms
const BLOCK_DURATION_MS int = 20

// The sampling frequency of the incoming audio
const SAMPLING_FREQUENCY int = 16000

// The number of samples per block
const SAMPLES_PER_BLOCK int = SAMPLING_FREQUENCY * BLOCK_DURATION_MS / 1000

// UNICAM parameters
const UNICAM_SAMPLES_PER_BLOCK int = SAMPLING_FREQUENCY / 1000
const UNICAM_CODED_SAMPLE_SIZE_BITS int = 8
const UNICAM_CODED_SHIFT_SIZE_BITS int = 4

// The URTP datagram paramegers
const SYNC_BYTE byte = 0x5a
const URTP_TIMESTAMP_SIZE int = 8
const URTP_SEQUENCE_NUMBER_SIZE int = 2
const URTP_PAYLOAD_SIZE_SIZE int = 2
const URTP_HEADER_SIZE int = 14
const URTP_SAMPLE_SIZE int = 2
const URTP_DATAGRAM_MAX_SIZE int = URTP_HEADER_SIZE + SAMPLES_PER_BLOCK * URTP_SAMPLE_SIZE

// Offset to the number of bytes part of the URTP header
const URTP_NUM_BYTES_AUDIO_OFFSET int = 12

// The overhead to add to the URTP datagram size to give a good IP buffer size for
// one packet
const IP_HEADER_OVERHEAD int = 40

// The audio coding schemes
const (
    PCM_SIGNED_16_BIT_16000_HZ = 0
    UNICAM_COMPRESSED_16000_HZ = 1
    MAX_NUM_AUDIO_CODING_SCHEMES = iota
)

// URTP reassembly states (needed for TCP reception)
const (
    URTP_STATE_WAITING_SYNC = iota
    URTP_STATE_WAITING_AUDIO_CODING = iota
    URTP_STATE_WAITING_SEQUENCE_NUMBER = iota
    URTP_STATE_WAITING_TIMESTAMP = iota
    URTP_STATE_WAITING_PAYLOAD_SIZE = iota
    URTP_STATE_WAITING_PAYLOAD = iota
)

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// A buffer for TCP data
var tcpBuffer bytes.Buffer

// A buffer in which to assemble a URTP packet (required for TCP mode) 
var urtpDatagram bytes.Buffer

// Where we are in reassembling a URTP packet (required for TCP reception)
var urtpReassemblyState int = URTP_STATE_WAITING_SYNC
var urtpByteCount int
var urtpPayloadSize int

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Decode PCM_SIGNED_16_BIT_16000_HZ data from a datagram
func decodePcm(audioDataPcm []byte) *[]int16 {
    audio := make([]int16, len(audioDataPcm) / URTP_SAMPLE_SIZE)
    
    // Just copy in the bytes
    x := 0
    for y := range audio {
        audio[y] = (int16(audioDataPcm[x]) << 8) + int16(audioDataPcm[x + 1])
        x += 2 
    }
    
    return &audio    
}

// Decode UNICAM_COMPRESSED_16000_HZ data from a datagram
func decodeUnicam(audioDataUnicam []byte) *[]int16 {
    var numBlocks int
    var blockOffset int
    var blockCount int
    var shiftValues byte
    var shift byte
    var peakShift byte
    var sample int16
    var sourceIndex int
    
    // Work out how much audio data is present
    for x := 0; x < len(audioDataUnicam) * 8; x += UNICAM_SAMPLES_PER_BLOCK * UNICAM_CODED_SAMPLE_SIZE_BITS + UNICAM_CODED_SHIFT_SIZE_BITS {
        numBlocks++;
    }
    
    // Allocate space
    audio := make([]int16, numBlocks * UNICAM_SAMPLES_PER_BLOCK)
    
    log.Printf("UNICAM: %d byte(s) containing %d block(s), expanding to a total of %d samples(s) of uncompressed audio.\n", len(audioDataUnicam), numBlocks, len(audio))
    
    // Decode the blocks
    for blockCount < numBlocks {
        
        // Get the compressed values
        for x := 0; x < UNICAM_SAMPLES_PER_BLOCK; x++ {
            audio[blockOffset + x] = int16(audioDataUnicam[sourceIndex])
            sourceIndex++
        }
        
        // Get the shift value
        if (blockCount % 2 == 0) {
            // Even block
            shiftValues = audioDataUnicam[sourceIndex]            
            sourceIndex++
            shift = shiftValues >> 4
        } else {
            shift = shiftValues & 0x0F
        }
        
        if shift > peakShift {
            peakShift = shift
        }
        
        //log.Printf("UNICAM block %d, shift value %d.\n", blockCount, shift)
        // Shift the values to uncompress them
        for x := 0; x < UNICAM_SAMPLES_PER_BLOCK; x++ {
            // Check if the top bit is set and, if so, sign extend
            sample = audio[blockOffset + x]
            if sample & (1 << (uint(UNICAM_CODED_SAMPLE_SIZE_BITS) - 1)) != 0 {
                for y := uint(UNICAM_CODED_SAMPLE_SIZE_BITS); y < uint(URTP_SAMPLE_SIZE) * 8; y++ {
                    sample |= (1 << y)
                }
            }
            audio[blockOffset + x] = sample << shift
            
            //log.Printf("UNICAM block %d:%02d, compressed value %d (0x%x) becomes %d (0x%x).\n",
            //           blockCount, x, sample, sample, audio[blockOffset + x], audio[blockOffset + x])
        }
        
        blockOffset += UNICAM_SAMPLES_PER_BLOCK
        blockCount++
    }
    log.Printf("UNICAM highest shift value was %d.\n", peakShift)
    
    return &audio    
}

// Handle an incoming URTP datagram and send it off for processing
func handleUrtpDatagram(packet []byte) {
    log.Printf("Packet of size %d byte(s) received.\n", len(packet))
//    log.Printf("%s\n", hex.Dump(line[:numBytesIn]))
    if (len(packet) >= URTP_HEADER_SIZE) {
        // Populate a URTP datagram with the data
        urtpDatagram := new(UrtpDatagram)
        log.Printf("URTP header:\n")
        log.Printf("  sync byte:        0x%x.\n", packet[0])
        audioCodingScheme := packet[1]
        urtpDatagram.SequenceNumber = uint16(packet[2]) << 8 + uint16(packet[3])
        log.Printf("  sequence number:  %d.\n", urtpDatagram.SequenceNumber)
        urtpDatagram.Timestamp = (uint64(packet[4]) << 56) + (uint64(packet[5]) << 48) + (uint64(packet[6]) << 40) + (uint64(packet[7]) << 32) +
                                 (uint64(packet[8]) << 24) + (uint64(packet[9]) << 16) + (uint64(packet[10]) << 8) + uint64(packet[11])
        log.Printf("  timestamp:        %6.3f ms.\n", float64(urtpDatagram.Timestamp) / 1000)
        
        if (len(packet) > URTP_HEADER_SIZE) {
            switch (audioCodingScheme) {
                case PCM_SIGNED_16_BIT_16000_HZ:
                    log.Printf("  audio coding:     PCM_SIGNED_16_BIT_16000_HZ.\n")
                    urtpDatagram.Audio = decodePcm(packet[URTP_HEADER_SIZE:])
                case UNICAM_COMPRESSED_16000_HZ:
                    log.Printf("  audio coding:     UNICAM_COMPRESSED_16000_HZ.\n")
                    urtpDatagram.Audio = decodeUnicam(packet[URTP_HEADER_SIZE:])
                default:
                    log.Printf("  audio coding:     !unknown!\n")
            }
        }
        
        if urtpDatagram.Audio != nil {
            log.Printf("URTP sample(s) %d\n", len(*urtpDatagram.Audio))
        } else {
            log.Printf("Unable to decode audio samples from this datagram.\n")
        }
        
        // Send the data to the processing channel
        ProcessDatagramsChannel <- urtpDatagram
    }    
}

// Verify that a sequence of byte represents URTP beader
func verifyUrtpHeader(header []byte) bool {
    var isHeader bool
    
    if len(header) >= URTP_HEADER_SIZE {
        if header[0] == SYNC_BYTE {
            if header[1] < MAX_NUM_AUDIO_CODING_SCHEMES {
                bytesOfPayload := ((int(header[URTP_NUM_BYTES_AUDIO_OFFSET]) << 8) + (int(header[URTP_NUM_BYTES_AUDIO_OFFSET + 1])))
                if bytesOfPayload <= URTP_DATAGRAM_MAX_SIZE {
                    isHeader = true;
                } else {
                    log.Printf("NOT a URTP header %x (%d (0x%x, in the last two bytes) is larger than the maximum number of payload bytes (%d)).\n", header,
                               bytesOfPayload, bytesOfPayload, URTP_DATAGRAM_MAX_SIZE)
                }
            } else {
                log.Printf("NOT a URTP header %x (0x%x in the second byte is not a valid audio coding scheme).\n", header, header[1])
            }
        } else {
            log.Printf("NOT a URTP header %x (0x%x at the start is not a sync byte (%x)).\n", header, header[0], SYNC_BYTE)
        }
    } else {
        log.Printf("NOT a URTP header %x (must be at least %d bytes long).\n", header, URTP_HEADER_SIZE)
    }
    
    return isHeader
}

// Handle a stream of (e.g. TCP) bytes containing URTP datagrams
func handleUrtpStream(data []byte) {
    var err error
    var item byte
    var header bytes.Buffer
    
    // Write all the data to the TCP buffer
    tcpBuffer.Write(data)
    
    log.Printf("TCP reassembly: %d byte(s) received.\n", len(data))
    for item, err = tcpBuffer.ReadByte(); err == nil; item, err = tcpBuffer.ReadByte() {
        //log.Printf("TCP reassembly: state %d, byte %d (0x%x).\n", urtpReassemblyState, item, item)
        switch (urtpReassemblyState) {
            case URTP_STATE_WAITING_SYNC:
                // Look for the sync byte
                if item == SYNC_BYTE {
                    header.WriteByte(item)
                    urtpReassemblyState = URTP_STATE_WAITING_AUDIO_CODING
                } else {
                    // log.Printf("TCP reassembly: awaiting initial sync byte but 0x%x isn't one (0x%x).\n", item, SYNC_BYTE)
                    header.Reset()
                    urtpReassemblyState = URTP_STATE_WAITING_SYNC
                }
            case URTP_STATE_WAITING_AUDIO_CODING:
                // Look for the audio coding scheme and check it
                if item < MAX_NUM_AUDIO_CODING_SCHEMES {
                    header.WriteByte(item)
                    urtpReassemblyState = URTP_STATE_WAITING_SEQUENCE_NUMBER
                } else {
                    log.Printf("TCP reassembly: audio coding scheme in the second byte (0x%0x) is not a valid audio coding scheme.\n", item)
                    header.Reset()
                    urtpReassemblyState = URTP_STATE_WAITING_SYNC
                }
            case URTP_STATE_WAITING_SEQUENCE_NUMBER:
                // Read in the two-byte sequence number
                header.WriteByte(item)
                urtpByteCount++
                if urtpByteCount >= URTP_SEQUENCE_NUMBER_SIZE {
                    urtpByteCount = 0
                    urtpReassemblyState = URTP_STATE_WAITING_TIMESTAMP
                }
            case URTP_STATE_WAITING_TIMESTAMP:
                // Read in the eight-byte timestamp
                header.WriteByte(item)
                urtpByteCount++
                if urtpByteCount >= URTP_TIMESTAMP_SIZE {
                    urtpByteCount = 0
                    urtpReassemblyState = URTP_STATE_WAITING_PAYLOAD_SIZE
                }
            case URTP_STATE_WAITING_PAYLOAD_SIZE:
                // Read in the two-byte payload size
                header.WriteByte(item)
                urtpPayloadSize += int (uint(item) << uint((8 * (URTP_PAYLOAD_SIZE_SIZE - urtpByteCount - 1))))
                urtpByteCount++
                if urtpByteCount >= URTP_PAYLOAD_SIZE_SIZE {
                    // Got the payload size, check it and, if it is OK, write the header
                    urtpByteCount = 0
                    //log.Printf("TCP reassembly: URTP payload is %d byte(s).\n", urtpPayloadSize)
                    if urtpPayloadSize <= URTP_DATAGRAM_MAX_SIZE {
                        urtpReassemblyState = URTP_STATE_WAITING_PAYLOAD
                        urtpDatagram.Write(header.Bytes())
                        if urtpPayloadSize == 0 {
                            header.Reset()
                            urtpReassemblyState = URTP_STATE_WAITING_SYNC                
                        }
                    } else {
                        log.Printf("TCP reassembly: NOT a URTP header, payload length %d (0x%x, in the last two bytes) is larger than the maximum number of payload bytes (%d)).\n",
                                   urtpPayloadSize, urtpPayloadSize, URTP_DATAGRAM_MAX_SIZE)
                        urtpPayloadSize = 0
                        header.Reset()
                        urtpReassemblyState = URTP_STATE_WAITING_SYNC
                    }
                }
            case URTP_STATE_WAITING_PAYLOAD:
                // Write the one byte we have
                urtpDatagram.WriteByte(item)
                if urtpPayloadSize > 0 {
                    urtpPayloadSize--
                }
                // Read in as much of the rest of the payload as possible
                bytesToRead := tcpBuffer.Len()
                if bytesToRead > urtpPayloadSize {
                    bytesToRead = urtpPayloadSize
                }
                urtpDatagram.Write(tcpBuffer.Next(bytesToRead))
                urtpPayloadSize -= bytesToRead
                if urtpPayloadSize == 0 {
                    // Got the lot, handle the complete datagram now and reset the state machine
                    log.Printf("TCP reassembly: URTP packet (%d bytes) fully received.\n", urtpDatagram.Len())
                    handleUrtpDatagram(urtpDatagram.Next(urtpDatagram.Len()))
                    header.Reset()
                    urtpReassemblyState = URTP_STATE_WAITING_SYNC                
                } else {
                    //log.Printf("TCP reassembly: %d byte(s) of payload remaining to be read.\n", urtpPayloadSize)
                }
            default:
                urtpByteCount = 0
                urtpPayloadSize = 0
                header.Reset()
                urtpReassemblyState = URTP_STATE_WAITING_SYNC                
        }
    }
}

// Run a UDP server forever
func udpServer(port string) {
    var numBytesIn int
    var server *net.UDPConn
    line := make([]byte, URTP_DATAGRAM_MAX_SIZE)

    // Set up the server
    localUdpAddr, err := net.ResolveUDPAddr("udp", ":" + port)
    if err == nil {
        // Begin listening
        server, err = net.ListenUDP("udp", localUdpAddr)
        if err == nil {
            defer server.Close()
            fmt.Printf("UDP server listening for Chuffs on port %s.\n", port)
            err1 := server.SetReadBuffer(URTP_DATAGRAM_MAX_SIZE + IP_HEADER_OVERHEAD)
            if err1 != nil {
                log.Printf("Unable to set optimal read buffer size (%s).\n", err1.Error())
            }
            // Read UDP packets forever
            for numBytesIn, _, err = server.ReadFromUDP(line); (err == nil) && (numBytesIn > 0); numBytesIn, _, err = server.ReadFromUDP(line) {
                // For UDP, a single URTP datagram arrives in a single UDP packet
                if (numBytesIn >= URTP_HEADER_SIZE) && (verifyUrtpHeader(line[:URTP_HEADER_SIZE])) {
                    handleUrtpDatagram(line[:numBytesIn])
                }
            }
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error reading from port %v (%s).\n", localUdpAddr, err.Error())
            } else {
                fmt.Fprintf(os.Stderr, "UDP read on port %v returned when it should not.\n", localUdpAddr)    
            }
        } else {
            fmt.Fprintf(os.Stderr, "Couldn't start UDP server on port %s (%s).\n", port, err.Error())
        }            
    } else {
        fmt.Fprintf(os.Stderr, "'%s' is not a valid UDP address (%s).\n", port, err.Error())
    }    
}

// Run a TCP server forever
func tcpServer(port string) {
    var newServer net.Conn
    var currentServer net.Conn
    
    listener, err := net.Listen("tcp", ":" + port)
    if err == nil {
        defer listener.Close()
        // Listen for a connection
        for {
            fmt.Printf("TCP server waiting for a [further] Chuff connection on port %s.\n", port)    
            newServer, err = listener.Accept()
            if err == nil {
                if currentServer != nil {
                    currentServer.Close()
                }
                currentServer = newServer
                x, success := currentServer.(*net.TCPConn)
                if success {
                    err1 := x.SetReadBuffer(30000)
                    if err1 != nil {
                        log.Printf("Unable to set optimal read buffer size (%s).\n", err1.Error())
                    }
                    err1 = x.SetNoDelay(true)
                    if err1 != nil {
                        log.Printf("Unable to switch of Nagle algorithm (%s).\n", err1.Error())
                    }
                } else {
                    log.Printf("Can't cast *net.Conn to *net.TCPConn in order to set optimal read buffer size.\n")
                }
                // Process datagrams received on the channel in another go routine
                fmt.Printf("Connection made by %s.\n", currentServer.RemoteAddr().String())
                go func(server net.Conn) {
                    // Read packets until the connection is closed under us
                    line := make([]byte, URTP_DATAGRAM_MAX_SIZE)                
                    for numBytesIn, err := server.Read(line); (err == nil) && (numBytesIn > 0); numBytesIn, err = server.Read(line) {
                        handleUrtpStream(line[:numBytesIn])
                    }
                    fmt.Printf("[Connection to %s closed].\n", server.RemoteAddr().String())
                }(currentServer)
            } else {
                fmt.Fprintf(os.Stderr, "Error accepting connection (%s).\n", err.Error())        
            }
        }
    } else {
        fmt.Fprintf(os.Stderr, "Unable to listen for TCP connections on port %s (%s).\n", port, err.Error())        
    }
}

// Run the server that receives the audio of Chuffs; this function should never return
func operateAudioIn(port string, useTCP bool) {    
    if useTCP {
        tcpServer(port)
    } else {
        udpServer(port)
    }
}
