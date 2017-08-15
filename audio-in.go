/* Audio input (UDP server) for the Internet of Chuffs.
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
//    "encoding/hex"
)

//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

// Struct to hold a URTP datagram
type UrtpDatagram struct {
    SequenceNumber  uint16
    Timestamp       uint32
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
const URTP_HEADER_SIZE int = 8
const URTP_SAMPLE_SIZE int = 2
const URTP_DATAGRAM_SIZE int = URTP_HEADER_SIZE + SAMPLES_PER_BLOCK * URTP_SAMPLE_SIZE

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Run the UDP server that receives the audio of Chuffs; this function should never return
func operateAudioIn(port string) {
    var numBytesIn int
    var localUdpAddr *net.UDPAddr
    var server *net.UDPConn
    var err error
    line := make([]byte, URTP_DATAGRAM_SIZE)

    // Set up the server
    localUdpAddr, err = net.ResolveUDPAddr("udp", ":" + port)
    if (err == nil) && (localUdpAddr != nil) {
        // Begin listening
        server, err = net.ListenUDP("udp", localUdpAddr)
        if err == nil {
            fmt.Printf("UDP server listening for Chuffs on port %s.\n", port)    
            // Read UDP packets forever
            for numBytesIn, _, err = server.ReadFromUDP(line); (err == nil) && (numBytesIn > 0); numBytesIn, _, err = server.ReadFromUDP(line) {
                log.Printf("UDP packet of size %d byte(s) received.\n", numBytesIn)
//                log.Printf("%s\n", hex.Dump(line[:numBytesIn]))
                if (numBytesIn >= URTP_HEADER_SIZE) {
                    // Populate a URTP datagram with the data
                    urtpDatagram := new(UrtpDatagram)
                    log.Printf("URTP header:\n")
                    urtpDatagram.SequenceNumber = uint16(line[2]) << 8 + uint16(line[3])
                    log.Printf("  sequence number %d\n", urtpDatagram.SequenceNumber)
                    urtpDatagram.Timestamp = (uint32(line[4]) << 24) + (uint32(line[5]) << 16) + (uint32(line[6]) << 8) + uint32(line[7])
                    log.Printf("  timestamp       %d\n", urtpDatagram.Timestamp)
                    if (numBytesIn > URTP_HEADER_SIZE) {
                        audio := make([]int16, (numBytesIn - URTP_HEADER_SIZE) / URTP_SAMPLE_SIZE)
                        // Copy in the bytes
                        x := URTP_HEADER_SIZE
                        for y := range audio {
                            audio[y] = (int16(line[x]) << 8) + int16(line[x + 1])
                            x += 2 
                        }
                        urtpDatagram.Audio = &audio
                    }
                    log.Printf("URTP samples %d\n", len(*urtpDatagram.Audio))
                    
                    // Send the data to the processing channel
                    ProcessDatagramsChannel <- urtpDatagram
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
