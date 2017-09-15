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

// The URTP datagram dimensions
const URTP_HEADER_SIZE int = 14
const URTP_SAMPLE_SIZE int = 2
const URTP_DATAGRAM_SIZE int = URTP_HEADER_SIZE + SAMPLES_PER_BLOCK * URTP_SAMPLE_SIZE

// Offset to the number of bytes part of the URTP header
const URTP_NUM_BYTES_AUDIO_OFFSET int = 12

// The overhead to add to the URTP datagram size to give a good IP buffer size for
// one packet
const IP_HEADER_OVERHEAD int = 40

// The audio coding schemes
const (
    PCM_SIGNED_16_BIT_16000_HZ = 0
    UNICAM_COMPRESSED_16000_HZ = 1
)

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// A buffer for TCP data
var tcpBuffer bytes.Buffer

// A buffer in which to assemble a URTP packet (required for TCP mode) 
var urtpDatagram bytes.Buffer

// The current number of bytes expected to make a complete URTP datagram
// (only needed in TCP mode)
var urtpDatagramBytesRemaining int

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Handle an incoming URTP datagram and send it off for processing
func handleUrtpDatagram(packet []byte) {
    log.Printf("Packet of size %d byte(s) received.\n", len(packet))
//    log.Printf("%s\n", hex.Dump(line[:numBytesIn]))
    if (len(packet) >= URTP_HEADER_SIZE) {
        // Populate a URTP datagram with the data
        urtpDatagram := new(UrtpDatagram)
        log.Printf("URTP header:\n")
        log.Printf("  protocol version: %d.\n", packet[0])
        audioCodingScheme := packet[1]
        urtpDatagram.SequenceNumber = uint16(packet[2]) << 8 + uint16(packet[3])
        log.Printf("  sequence number:  %d\n", urtpDatagram.SequenceNumber)
        urtpDatagram.Timestamp = (uint64(packet[4]) << 56) + (uint64(packet[5]) << 48) + (uint64(packet[6]) << 40) + (uint64(packet[7]) << 32) +
                                 (uint64(packet[8]) << 24) + (uint64(packet[9]) << 16) + (uint64(packet[10]) << 8) + uint64(packet[11])
        log.Printf("  timestamp:        %6.3f ms\n", float64(urtpDatagram.Timestamp) / 1000)
        if (len(packet) > URTP_HEADER_SIZE) {
            switch (audioCodingScheme) {
                case PCM_SIGNED_16_BIT_16000_HZ:
                    log.Printf("  audio coding:     PCM_SIGNED_16_BIT_16000_HZ.\n")
                    audio := make([]int16, (len(packet) - URTP_HEADER_SIZE) / URTP_SAMPLE_SIZE)
                    // Copy in the bytes
                    x := URTP_HEADER_SIZE
                    for y := range audio {
                        audio[y] = (int16(packet[x]) << 8) + int16(packet[x + 1])
                        x += 2 
                    }
                    urtpDatagram.Audio = &audio
                case UNICAM_COMPRESSED_16000_HZ:
                    log.Printf("  audio coding:     UNICAM_COMPRESSED_16000_HZ.\n")
                default:
                    log.Printf("  audio coding:     !unknown!\n")
            }
        }
        log.Printf("URTP samples %d\n", len(*urtpDatagram.Audio))
        
        // Send the data to the processing channel
        ProcessDatagramsChannel <- urtpDatagram
    }    
}

// Handle a stream of (e.g. TCP) bytes containing URTP datagrams
func handleUrtpStream(data []byte) {    
    // Write all the data to the TCP buffer
    tcpBuffer.Write(data)
    
    log.Printf("TCP reassembly: %d byte(s) received.\n", len(data))
    // Keep reading while there are more headers
    for tcpBuffer.Len() >= URTP_HEADER_SIZE {
        if urtpDatagram.Len() == 0 {
            // If the urtpDatagram we're making is empty and we have received enough
            // data, read the header bytes to determine the number of samples
            if tcpBuffer.Len() >= URTP_HEADER_SIZE {
                header := tcpBuffer.Next(URTP_HEADER_SIZE)
                urtpDatagramBytesRemaining = ((int(header[URTP_NUM_BYTES_AUDIO_OFFSET]) << 8) + (int(header[URTP_NUM_BYTES_AUDIO_OFFSET + 1])))
                log.Printf("TCP reassembly: URTP header %x (%d sample(s) expected).\n", header, urtpDatagramBytesRemaining / URTP_SAMPLE_SIZE)
                urtpDatagram.Write(header)
                if urtpDatagramBytesRemaining > URTP_DATAGRAM_SIZE - URTP_HEADER_SIZE {
                    log.Printf("WARNING: number of URTP samples (%d) is bigger then the maximum expected (%d).\n",
                               urtpDatagramBytesRemaining, URTP_DATAGRAM_SIZE - URTP_HEADER_SIZE)
                }
            }
        }
        
        if urtpDatagram.Len() > 0 {
            // If we've assembled something of a URTP datagram but not the desired
            // number of bytes worth of samples, read more in
            if urtpDatagramBytesRemaining > 0 {
                body := tcpBuffer.Next(urtpDatagramBytesRemaining)
                log.Printf("TCP reassembly: %d sample(s) received so far.\n", len(body) / 2)
                urtpDatagram.Write(body)
                urtpDatagramBytesRemaining -= len(body)
            }
            
            // If we've read all we need, pass the assembled URTP datagram to the handler
            if urtpDatagramBytesRemaining == 0 {
                log.Printf("TCP reassembly: URTP packet (%d bytes) fully received.\n", urtpDatagram.Len())
                handleUrtpDatagram(urtpDatagram.Next(urtpDatagram.Len()))
            }
        }
    }
}

// Run a UDP server forever
func udpServer(port string) {
    var numBytesIn int
    var server *net.UDPConn
    line := make([]byte, URTP_DATAGRAM_SIZE)

    // Set up the server
    localUdpAddr, err := net.ResolveUDPAddr("udp", ":" + port)
    if err == nil {
        // Begin listening
        server, err = net.ListenUDP("udp", localUdpAddr)
        if err == nil {
            defer server.Close()
            fmt.Printf("UDP server listening for Chuffs on port %s.\n", port)
            err1 := server.SetReadBuffer(URTP_DATAGRAM_SIZE + IP_HEADER_OVERHEAD)
            if err1 != nil {
                log.Printf("Unable to set optimal read buffer size (%s).\n", err1.Error())
            }
            // Read UDP packets forever
            for numBytesIn, _, err = server.ReadFromUDP(line); (err == nil) && (numBytesIn > 0); numBytesIn, _, err = server.ReadFromUDP(line) {
                // For UDP, a single URTP datagram arrives in a single UDP packet
                handleUrtpDatagram(line[:numBytesIn])
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
                    line := make([]byte, URTP_DATAGRAM_SIZE)                
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
