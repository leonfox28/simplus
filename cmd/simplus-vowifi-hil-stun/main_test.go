package main

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestMappedIPParsesXORMappedAddress(t *testing.T) {
	transaction := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	response := make([]byte, 32)
	binary.BigEndian.PutUint16(response[:2], 0x0101)
	binary.BigEndian.PutUint16(response[2:4], 12)
	binary.BigEndian.PutUint32(response[4:8], cookie)
	copy(response[8:20], transaction)
	binary.BigEndian.PutUint16(response[20:22], 0x0020)
	binary.BigEndian.PutUint16(response[22:24], 8)
	response[25] = 1
	address := net.ParseIP("203.0.113.7").To4()
	mask := make([]byte, 4)
	binary.BigEndian.PutUint32(mask, cookie)
	for index := range address {
		response[28+index] = address[index] ^ mask[index]
	}
	parsed, err := mappedIP(response, transaction)
	if err != nil || !parsed.Equal(address) {
		t.Fatalf("address=%v error=%v", parsed, err)
	}
}

func TestMappedIPRejectsWrongTransaction(t *testing.T) {
	response := make([]byte, 20)
	binary.BigEndian.PutUint16(response[:2], 0x0101)
	binary.BigEndian.PutUint32(response[4:8], cookie)
	response[8] = 1
	if _, err := mappedIP(response, make([]byte, 12)); err == nil {
		t.Fatal("accepted a mismatched transaction")
	}
}
