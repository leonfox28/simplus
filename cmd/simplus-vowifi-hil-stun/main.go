package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

const cookie uint32 = 0x2112A442

func main() {
	target := flag.String("target", "", "STUN IP:port")
	flag.Parse()
	address, err := net.ResolveUDPAddr("udp4", *target)
	if err != nil {
		fail(err)
	}
	connection, err := net.DialUDP("udp4", nil, address)
	if err != nil {
		fail(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(6 * time.Second))
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request[:2], 0x0001)
	binary.BigEndian.PutUint32(request[4:8], cookie)
	if _, err := rand.Read(request[8:]); err != nil {
		fail(err)
	}
	if _, err := connection.Write(request); err != nil {
		fail(err)
	}
	response := make([]byte, 2048)
	count, err := connection.Read(response)
	if err != nil {
		fail(err)
	}
	ip, err := mappedIP(response[:count], request[8:])
	if err != nil {
		fail(err)
	}
	fmt.Printf("ok=true mapped_ip=%s\n", ip)
}

func mappedIP(response, transaction []byte) (net.IP, error) {
	if len(response) < 20 || binary.BigEndian.Uint16(response[:2]) != 0x0101 || binary.BigEndian.Uint32(response[4:8]) != cookie {
		return nil, errors.New("invalid response")
	}
	for index := range transaction {
		if response[index+8] != transaction[index] {
			return nil, errors.New("transaction mismatch")
		}
	}
	end := 20 + int(binary.BigEndian.Uint16(response[2:4]))
	if end > len(response) {
		return nil, errors.New("truncated response")
	}
	for offset := 20; offset+4 <= end; {
		kind := binary.BigEndian.Uint16(response[offset : offset+2])
		length := int(binary.BigEndian.Uint16(response[offset+2 : offset+4]))
		start, finish := offset+4, offset+4+length
		if finish > end {
			return nil, errors.New("truncated attribute")
		}
		if (kind == 0x0020 || kind == 0x0001) && length >= 8 && response[start+1] == 1 {
			ip := append(net.IP(nil), response[start+4:start+8]...)
			if kind == 0x0020 {
				mask := make([]byte, 4)
				binary.BigEndian.PutUint32(mask, cookie)
				for index := range ip {
					ip[index] ^= mask[index]
				}
			}
			return ip, nil
		}
		offset = start + (length+3)&^3
	}
	return nil, errors.New("no mapped address")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ok=false error="+err.Error())
	os.Exit(1)
}
