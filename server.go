package main

import (
    "fmt"
    "net"
    "os"
    "flag"
    "log"
    "time"
)


//--------------------------------------------------------------------
// Types
//--------------------------------------------------------------------

//--------------------------------------------------------------------
// Variables
//--------------------------------------------------------------------

// Averaging interval for the throughput calculation
var averagingInterval time.Duration = time.Second * 10

// Command-line flags
var pPort = flag.String ("p", "", "the port number to listen on.");
var Usage = func() {
    fmt.Fprintf(os.Stderr, "\n%s: run the Internet of Chuffs server.  Usage:\n", os.Args[0])
        flag.PrintDefaults()
    }

//--------------------------------------------------------------------
// Functions
//--------------------------------------------------------------------

// Entry point
func main() {
    var numPackets int
    var readErr error
    var numBytesIn int
    var prevPacketTime time.Time
    var prevIntervalTime time.Time
    var bytesDuringInterval int
    var timeNow time.Time
    var sequenceNumber int
    var prevSequenceNumber int = 0
    var timestamp int
    var alarm string
    var rate float64 = 0
    line := make([]byte, 1024)

    // Deal with the command-line parameters
    flag.Parse()
    
    if *pPort != "" {
        // Say what we're doing
        fmt.Printf("Waiting to receiving UDP packets on port %s.\n", *pPort)
        
        // Set up the server
        pLocalUdpAddr, err := net.ResolveUDPAddr("udp", ":" + *pPort)
        if (err == nil) && (pLocalUdpAddr != nil) {
            pServer, err := net.ListenUDP("udp", pLocalUdpAddr)
            if err == nil {
                for numBytesIn, _, readErr = pServer.ReadFromUDP(line); (readErr == nil) && (numBytesIn > 0); numBytesIn, _, readErr = pServer.ReadFromUDP(line) {
                    numPackets++
                    sequenceNumber = int(line[2]) << 8 + int(line[3])
                    timestamp = (int(line[4]) << 24) + (int(line[5]) << 16) + (int(line[6]) << 8) + int(line[7])
                    alarm = ""
                    if (prevSequenceNumber != 0) && (sequenceNumber != prevSequenceNumber + 1) {
                        alarm = "*"
                    }
                    fmt.Printf("\rProtocol %d, seq %d%s, time %3.3f, throughput %3.3f kbits/s.                ",
                               line[0], sequenceNumber, alarm, float64(timestamp) / 1000, rate)
                    timeNow = time.Now();
                    if (timeNow.Sub(prevPacketTime) < averagingInterval) {
                        if (timeNow.Sub(prevIntervalTime) < averagingInterval) {
                            bytesDuringInterval += numBytesIn;
                        } else {
                            rate = float64(bytesDuringInterval) * 8 / timeNow.Sub(prevIntervalTime).Seconds() / 1000
                            bytesDuringInterval = numBytesIn
                            prevIntervalTime = timeNow
                        }
                    } else {
                        bytesDuringInterval = 0
                    }
                    prevSequenceNumber = sequenceNumber;
                    prevPacketTime = timeNow
                }    
                if readErr != nil {
                    log.Printf("Error reading from port %v (%s).\n", pLocalUdpAddr, readErr.Error())
                } else {
                    log.Printf("UDP read on port %v returned when it should not.\n", pLocalUdpAddr)    
                }
            } else {
                log.Printf("Couldn't start UDP server on port %s (%s).\n", *pPort, err.Error())
            }            
        } else {
            log.Printf("'%s' is not a valid UDP address (%s).\n", *pPort, err.Error())
        }            
    } else {
        fmt.Printf("Must specify a port number.\n")
        flag.PrintDefaults()
        os.Exit(-1)
    }
}
